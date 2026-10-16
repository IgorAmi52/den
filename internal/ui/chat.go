package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
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
	// pendingActions are changes den offered for what the user asked; enter on an empty line makes them.
	pendingActions []brain.ChatAction
	// thinking is set while den's model works on a reply.
	thinking bool
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
	state.pendingActions = message.Actions
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
		state.messages, state.pendingActions = nil, nil
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
				if len(state.pendingActions) > 0 {
					currentModel.applyChatActions(state.pendingActions)
				}
				return currentModel, nil
			}
			if state.thinking {
				return currentModel, nil
			}
			state.input.SetValue("")
			state.addMessage(brain.ChatMessage{Text: text})
			if currentModel.helperCaller != nil {
				state.thinking = true
				currentModel.queueJob(job{kind: chatJob})
				return currentModel, currentModel.startNextJob()
			}
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

// chatInput is what den's model gets for a chat reply: the conversation, what it's about, and the repo.
func (currentModel model) chatInput() brain.ChatInput {
	state := currentModel.chat
	input := brain.ChatInput{
		Conversation:    state.messages,
		Repository:      &currentModel.repository,
		Digests:         currentModel.digests,
		SubjectFolder:   state.subject.folderId,
		SubjectSession:  state.subject.sessionId,
		RepositoryNotes: currentModel.repository.ReadContext(""),
	}
	if state.about != nil {
		label, summary := currentModel.suggestionSummary(*state.about)
		input.About = label + ": " + summary + ". " + state.about.Reason
	}
	if state.subject.folderId != "" {
		input.SubjectNotes = currentModel.folderNotesText(state.subject.folderId)
	}
	return input
}

// applyChatAnswer shows the model's reply, with the changes it proposes listed under it, waiting for enter.
func (currentModel *model) applyChatAnswer(answer brain.ChatAnswer, input brain.ChatInput) {
	state := &currentModel.chat
	state.thinking = false
	actions := brain.ResolveChatActions(answer, input, currentModel.workingDirectory)
	text := strings.TrimSpace(answer.Reply)
	if len(actions) > 0 {
		var listed []string
		for _, action := range actions {
			listed = append(listed, "· "+action.Description)
		}
		text += "\n\n" + strings.Join(listed, "\n") + "\n↵ does it"
	}
	if len(answer.Actions) > len(actions) {
		text += fmt.Sprintf("\n(%d change I thought of didn't match your folders, sessions or files, so I left it out.)", len(answer.Actions)-len(actions))
	}
	state.addMessage(brain.ChatMessage{FromDen: true, Text: text, Actions: actions})
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "chat", Action: "reply", Detail: fmt.Sprintf("%d actions", len(actions)), Outcome: "proposed"}))
}

// applyChatActions makes the changes the user asked for in the chat and confirmed with enter.
func (currentModel *model) applyChatActions(actions []brain.ChatAction) {
	repository := &currentModel.repository
	var done []string
	slices.SortStableFunc(actions, func(first brain.ChatAction, second brain.ChatAction) int {
		return boolOrder(second.Kind == "createFolder") - boolOrder(first.Kind == "createFolder")
	})
	for _, action := range actions {
		folder, session := repository.FindFolder(action.FolderId), repository.FindSession(action.SessionId)
		switch action.Kind {
		case "link":
			if folder != nil {
				folder.LinkedPath = action.LinkedPath
				repository.ForgetFolderStarters(folder.Id)
			}
		case "unlink":
			if folder != nil {
				repository.Unlink(folder.Id)
			}
		case "move":
			if session != nil {
				folderId := action.FolderId
				if action.Name != "" {
					folderId = currentModel.folderNamed(action.Name)
				}
				fromFolderId := session.FolderId
				repository.MoveByHand(session.SessionId, folderId)
				repository.RememberMovedOut(session.SessionId, fromFolderId)
			}
		case "renameSession":
			if session != nil {
				session.Title, session.TitlePinned = action.Name, true
				delete(currentModel.retitles, session.SessionId)
				if open := currentModel.openAgentFor(session.SessionId); open != nil {
					currentModel.reportError(tmux.SetPaneOption(open.paneId, paneTitleOption, action.Name))
				}
			}
		case "renameFolder":
			if folder != nil {
				folder.Name = action.Name
				currentModel.updatePaneFolderNames(folder.Id, action.Name)
			}
		case "createFolder":
			repository.AddFolder(action.Name, action.FolderId)
		case "archive":
			if session != nil {
				repository.SetArchived(session.SessionId, true)
			}
		case "addRule":
			currentModel.addRuleNow(action)
		}
		done = append(done, action.Description)
		currentModel.reportError(repository.LogDecision(store.Decision{Job: "chat", SessionId: action.SessionId, FolderId: action.FolderId, Action: action.Kind, Detail: action.Description, Outcome: "applied"}))
	}
	currentModel.updatePaneFolders()
	currentModel.refreshContextTokens()
	currentModel.saveRepository()
	currentModel.chat.addMessage(brain.ChatMessage{FromDen: true, Text: "Done: " + strings.Join(done, "; ") + "."})
}

func boolOrder(value bool) int {
	if value {
		return 1
	}
	return 0
}

// addRuleNow writes a rule the user asked for: to a folder's notes, the repo's context, or the laptop files that lack it.
func (currentModel *model) addRuleNow(action brain.ChatAction) {
	switch action.Target {
	case "laptop":
		for _, laptopFile := range currentModel.laptopFiles {
			if laptopFile.Exists || laptopFile.Installed {
				_, err := store.AppendRule(laptopFile.Path, action.Rule)
				currentModel.reportError(err)
			}
		}
	default:
		current := currentModel.repository.ReadContext(action.FolderId)
		if store.HasRule(current, action.Rule) {
			return
		}
		if strings.TrimSpace(current) == "" {
			current = "# " + currentModel.contextHeading(action.FolderId) + "\n"
		}
		currentModel.reportError(currentModel.repository.WriteContext(action.FolderId, store.AddToSection(current, "Rules", action.Rule)))
	}
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
	if len(state.pendingActions) > 0 && state.input.Value() == "" {
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
	if currentModel.chat.thinking {
		lines = append(lines, "", theme.denLabel.Render(padRight("den", chatLabelWidth))+theme.faint.Render("thinking…"))
	}
	return lines
}
