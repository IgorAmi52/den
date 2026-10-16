package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

const chatLabelWidth = 6

// chatState is a conversation with den itself: to ask about the sessions, or to say what to change in a suggestion.
// den answers and proposes; it never acts on the repo.
type chatState struct {
	messages []brain.ChatMessage
	input    textinput.Model
	// about is the suggestion the chat was opened on, pinned above the conversation.
	about *store.Suggestion
	// subject is the folder or session the chat was opened on, so what the user asks is about it without naming it.
	subject chatSubject
	// scrollOffset is how many lines the conversation is scrolled up from the newest message.
	scrollOffset int
	// pendingAction is a change den offered for what the user asked; enter on an empty line makes it.
	pendingAction *brain.ChatAction
}

func newChatState() chatState {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "ask den, or tell it what to change"
	input.CharLimit = 2000
	return chatState{input: input}
}

func (state *chatState) resize(width int) {
	state.input.SetWidth(max(width-6, 10))
}

func (state *chatState) addMessage(message brain.ChatMessage) {
	state.messages = append(state.messages, message)
	state.pendingAction = message.Action
	state.scrollOffset = 0
}

// chatSubject is a folder, or a session and its folder; empty means the whole repo.
type chatSubject struct {
	folderId  string
	sessionId string
}

// openChatAbout starts a chat about the selected folder or session. From the repo, Inbox or Archive rows,
// the chat is about the whole repo.
func (currentModel *model) openChatAbout(row listRow, exists bool) tea.Cmd {
	var subject chatSubject
	switch {
	case exists && row.kind == folderRow:
		subject = chatSubject{folderId: row.folder.Id}
	case exists && row.kind == sessionRow:
		subject = chatSubject{folderId: row.session.FolderId, sessionId: row.session.SessionId}
	}
	return currentModel.startChat(nil, subject)
}

// openChat starts a chat about a suggestion.
func (currentModel *model) openChat(about *store.Suggestion) tea.Cmd {
	return currentModel.startChat(about, chatSubject{})
}

// startChat opens the chat; a new subject starts a new conversation.
func (currentModel *model) startChat(about *store.Suggestion, subject chatSubject) tea.Cmd {
	state := &currentModel.chat
	isSameSuggestion := (about == nil && state.about == nil) || (about != nil && state.about != nil && about.Id == state.about.Id)
	if !isSameSuggestion || subject != state.subject {
		state.messages, state.pendingAction = nil, nil
	}
	state.subject = subject
	state.about = nil
	if about != nil {
		pinned := *about
		state.about = &pinned
	}
	state.resize(currentModel.width)
	currentModel.openScreen(chatScreen)
	return state.input.Focus()
}

func (currentModel model) updateChat(message tea.Msg) (tea.Model, tea.Cmd) {
	state := &currentModel.chat
	if keyPress, isKeyPress := message.(tea.KeyPressMsg); isKeyPress {
		keys := currentModel.keys
		switch {
		case key.Matches(keyPress, keys.back):
			state.input.Blur()
			currentModel.closeScreen()
			return currentModel, nil
		case key.Matches(keyPress, keys.send):
			text := strings.TrimSpace(state.input.Value())
			if text == "" {
				if state.pendingAction != nil {
					currentModel.applyChatAction(*state.pendingAction)
				}
				return currentModel, nil
			}
			state.input.SetValue("")
			state.addMessage(brain.ChatMessage{Text: text})
			state.addMessage(brain.Reply(state.messages, state.about, brain.ChatContext{
				Repository:       &currentModel.repository,
				Digests:          currentModel.digests,
				WorkingDirectory: currentModel.workingDirectory,
				FolderId:         state.subject.folderId,
			}))
			return currentModel, nil
		case keyPress.String() == "pgup":
			state.scrollOffset += 5
			return currentModel, nil
		case keyPress.String() == "pgdown":
			state.scrollOffset = max(state.scrollOffset-5, 0)
			return currentModel, nil
		}
	}
	var command tea.Cmd
	state.input, command = state.input.Update(message)
	return currentModel, command
}

// applyChatAction makes a change the user asked for in the chat and confirmed with enter.
func (currentModel *model) applyChatAction(action brain.ChatAction) {
	folder := currentModel.repository.FindFolder(action.FolderId)
	if folder == nil {
		return
	}
	text := ""
	if action.LinkedPath == "" {
		text = fmt.Sprintf("Unlinked %s. New sessions in %s start without it.", filepath.Base(folder.LinkedPath), folder.Name)
		currentModel.repository.Unlink(folder.Id)
	} else {
		folder.LinkedPath = action.LinkedPath
		currentModel.repository.ForgetFolderStarters(folder.Id)
		text = fmt.Sprintf("Linked %s. New sessions in %s start with it.", filepath.Base(action.LinkedPath), folder.Name)
	}
	currentModel.refreshContextTokens()
	currentModel.saveRepository()
	currentModel.chat.addMessage(brain.ChatMessage{FromDen: true, Text: text})
}

func (currentModel model) chatView() string {
	theme := currentModel.theme
	width := currentModel.width
	state := currentModel.chat
	contentWidth := min(width-4, 100)
	margin := strings.Repeat(" ", max((width-contentWidth)/2, 2))
	top := []string{currentModel.screenTopBar("ask den", width), theme.border.Render(strings.Repeat("─", width))}
	var pinned []string
	if state.about != nil {
		label, summary := currentModel.suggestionSummary(*state.about)
		pinned = []string{theme.suggestionBold.Render("✦ about this suggestion"), theme.text.Render(label + " · " + summary)}
		if state.about.Reason != "" {
			pinned = append(pinned, theme.dim.Render(truncate(state.about.Reason, contentWidth-2)))
		}
	} else if session := currentModel.repository.FindSession(state.subject.sessionId); session != nil {
		pinned = []string{theme.bold.Render("about this session"), theme.text.Render(truncate(session.Title, contentWidth-2)),
			theme.dim.Render(session.Tool + " · in " + currentModel.repository.FolderName(session.FolderId))}
	} else if folder := currentModel.repository.FindFolder(state.subject.folderId); folder != nil {
		pinned = []string{theme.bold.Render("about ◆ " + folder.Name), theme.dim.Render(fmt.Sprintf("%s · what you ask is about this folder", countedSessions(currentModel.repository.SessionCount(folder.Id))))}
	}
	if len(pinned) > 0 {
		top = append(top, "")
		for _, line := range strings.Split(theme.pinned.Render(strings.Join(pinned, "\n")), "\n") {
			top = append(top, margin+line)
		}
	}
	bottom := []string{theme.border.Render(strings.Repeat("─", width)), margin + state.input.View()}
	send := currentModel.keys.send
	if state.pendingAction != nil && state.input.Value() == "" {
		send = relabeled(send, "yes")
	}
	bottom = append(bottom, margin+strings.Join(currentModel.hintLines([]key.Binding{send, currentModel.keys.scrollChat, currentModel.keys.back}, contentWidth, 1), ""))
	conversationHeight := max(currentModel.height-len(top)-len(bottom)-1, 1)
	conversation := currentModel.conversationLines(contentWidth)
	end := max(len(conversation)-state.scrollOffset, 0)
	start := max(end-conversationHeight, 0)
	lines := append(top, "")
	for _, line := range conversation[start:end] {
		lines = append(lines, margin+line)
	}
	for len(lines) < currentModel.height-len(bottom) {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, bottom...), "\n")
}

// conversationLines lays out the messages with "you" and "den" in a label column, or explains the chat when it is empty.
func (currentModel model) conversationLines(width int) []string {
	theme := currentModel.theme
	if len(currentModel.chat.messages) == 0 {
		lines := wrapAll([]string{
			theme.text.Render("Ask den about your sessions and folders, or tell it what to change in what it suggested."),
			theme.dim.Render("den answers and proposes. It never touches your repo, never runs anything, and every change it proposes waits for your yes."),
		}, width)
		lines = append(lines, "", theme.faint.Render("try:"))
		examples := []string{"link docs/PLAN.md to Eval", "which sessions worked on the eval loader?", "what does Eval's context say about pagination?"}
		switch {
		case currentModel.chat.about != nil:
			examples = []string{"put it in Agent API instead", "why do you think it belongs there?"}
		case currentModel.chat.subject.sessionId != "":
			examples = []string{"link docs/PLAN.md to its folder", "what did this session decide?"}
		case currentModel.chat.subject.folderId != "":
			examples = []string{"link docs/PLAN.md", "what did sessions here decide?"}
		}
		for _, example := range examples {
			lines = append(lines, theme.dim.Render("  "+example))
		}
		return lines
	}
	var lines []string
	for index, message := range currentModel.chat.messages {
		if index > 0 {
			lines = append(lines, "")
		}
		label := theme.youLabel.Render(padRight("you", chatLabelWidth))
		if message.FromDen {
			label = theme.denLabel.Render(padRight("den", chatLabelWidth))
		}
		for lineIndex, line := range wrapText(message.Text, width-chatLabelWidth) {
			if lineIndex > 0 {
				label = strings.Repeat(" ", chatLabelWidth)
			}
			lines = append(lines, label+theme.text.Render(line))
		}
	}
	return lines
}
