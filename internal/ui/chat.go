package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/agent"
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

// startChat opens the chat on a subject; the conversation carries over until the user starts a new one.
func (currentModel *model) startChat(about *store.Suggestion, subject chatSubject) tea.Cmd {
	state := &currentModel.chat
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
			if len(state.pendingActions) > 0 {
				state.settlePlan(planSkipped)
			}
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
		case key.Matches(keyPress, keys.newChat):
			state.messages, state.pendingActions, state.scrollOffset = nil, nil, 0
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
		Now:             time.Now(),
		AgentStatuses:   map[string]string{},
		SessionTurns:    map[string][]agent.Turn{},
	}
	for _, open := range currentModel.openAgents {
		input.AgentStatuses[open.sessionId] = open.status
	}
	for _, sessionId := range brain.ChatSessionIds(input) {
		session := currentModel.repository.FindSession(sessionId)
		if adapter, known := agent.ByName(session.Tool); known {
			if turns, err := adapter.Turns(sessionId); err == nil && len(turns) > 0 {
				input.SessionTurns[sessionId] = turns
			}
		}
	}
	if state.about != nil {
		label, summary := currentModel.suggestionSummary(*state.about)
		input.About = label + ": " + summary + ". " + state.about.Reason
		if len(state.about.Evidence) > 0 {
			input.About += "\nwhat den based it on, quoted from the session:\n- " + strings.Join(state.about.Evidence, "\n- ")
		}
	}
	if state.subject.folderId != "" {
		input.SubjectNotes = currentModel.folderNotesText(state.subject.folderId)
	}
	input.Transcripts = currentModel.chatTranscripts(state.subject.folderId, state.subject.sessionId)
	return input
}

// chatTranscripts reads the latest turns of the sessions a chat is about, so den can write context from them.
func (currentModel model) chatTranscripts(folderId string, sessionId string) []brain.Transcript {
	var transcripts []brain.Transcript
	for _, transcriptSessionId := range brain.ChatTranscriptSessionIds(&currentModel.repository, folderId, sessionId) {
		session := currentModel.repository.FindSession(transcriptSessionId)
		adapter, known := agent.ByName(session.Tool)
		if !known {
			continue
		}
		if turns, err := adapter.Turns(session.SessionId); err == nil && len(turns) > 0 {
			transcripts = append(transcripts, brain.Transcript{SessionId: session.SessionId, Turns: turns, LastAgentMessage: currentModel.digests[session.SessionId].LastAgentMessage})
		}
	}
	return transcripts
}

// applyChatAnswer shows the model's reply, with the changes it proposes listed under it, waiting for enter.
func (currentModel *model) applyChatAnswer(answer brain.ChatAnswer, input brain.ChatInput) {
	state := &currentModel.chat
	state.thinking = false
	actions := brain.ResolveChatActions(answer, input, currentModel.workingDirectory)
	text := replyText(answer.Reply, actions)
	if len(answer.Actions) > len(actions) {
		text += fmt.Sprintf("\n(%d change I thought of didn't match your folders, sessions or files, so I left it out.)", len(answer.Actions)-len(actions))
	}
	state.addMessage(brain.ChatMessage{FromDen: true, Text: text, Actions: actions})
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "chat", Action: "reply", Detail: fmt.Sprintf("%d actions", len(actions)), Outcome: "proposed"}))
}

// applyChatActions makes the changes the user asked for in the chat and confirmed with enter.
func (currentModel *model) applyChatActions(actions []brain.ChatAction) {
	currentModel.doChatActions(actions)
	currentModel.chat.settlePlan(planDone)
}

// settlePlan records what became of the plan waiting for the user's enter, so its card says so.
func (state *chatState) settlePlan(outcome string) {
	for index := len(state.messages) - 1; index >= 0; index-- {
		if message := &state.messages[index]; len(message.Actions) > 0 && message.Outcome == "" {
			message.Outcome = outcome
			break
		}
	}
	state.pendingActions = nil
}

// doChatActions makes changes the user confirmed, in the chat or the dock, and says what each did.
// Folders are made first, so a move into a new folder finds it.
func (currentModel *model) doChatActions(actions []brain.ChatAction) []string {
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
				repository.MoveByHand(session.SessionId, folderId)
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
			created := repository.AddFolder(action.Name, action.FolderId)
			if action.Text != "" {
				currentModel.reportError(repository.WriteContext(created.Id, action.Text))
			}
		case "archive":
			if session != nil {
				repository.SetArchived(session.SessionId, true)
			}
		case "startSession":
			folderId := action.FolderId
			if action.Name != "" {
				folderId = currentModel.folderNamed(action.Name)
			}
			currentModel.startNewSession(folderId, false, action.Text)
		case "addRule":
			currentModel.addRuleNow(action)
		case "moveNotes":
			currentModel.moveNotes(action.FromFolderId, action.FolderId)
		case "editNotes":
			currentModel.reportError(repository.WriteContext(action.FolderId, action.Text))
		}
		done = append(done, action.Description)
		currentModel.reportError(repository.LogDecision(store.Decision{Job: "chat", SessionId: action.SessionId, FolderId: action.FolderId, Action: action.Kind, Detail: action.Description, Outcome: "applied"}))
	}
	currentModel.updatePaneFolders()
	currentModel.refreshContextTokens()
	currentModel.saveRepository()
	return done
}

// moveNotes moves one folder's context into another's. A linked file moves as the link when the receiving folder
// has no context; otherwise its text is added there. den's own file is removed after; the user's file never is.
func (currentModel *model) moveNotes(fromFolderId string, toFolderId string) {
	repository := &currentModel.repository
	from, to := repository.FindFolder(fromFolderId), repository.FindFolder(toFolderId)
	moved := strings.TrimSpace(repository.ReadContext(fromFolderId))
	receiving := strings.TrimSpace(repository.ReadContext(toFolderId))
	if from != nil && from.LinkedPath != "" && to != nil && receiving == "" {
		to.LinkedPath, from.LinkedPath = from.LinkedPath, ""
		return
	}
	if moved == "" {
		return
	}
	if receiving != "" {
		moved = receiving + "\n\n" + moved
	}
	if currentModel.reportError(repository.WriteContext(toFolderId, moved)) {
		return
	}
	if from != nil && from.LinkedPath != "" {
		from.LinkedPath = ""
		return
	}
	if contextPath, err := repository.ContextPath(fromFolderId); err == nil {
		os.Remove(contextPath)
	}
}

// replyText is den's reply as shown. With changes listed under it, only its first sentence stays,
// since the list says the rest.
func replyText(reply string, actions []brain.ChatAction) string {
	reply = strings.TrimSpace(reply)
	if len(actions) == 0 {
		return reply
	}
	for index, character := range reply {
		if (character == '.' || character == '!' || character == '?') && index+1 < len(reply) && reply[index+1] == ' ' {
			return reply[:index+1]
		}
	}
	return reply
}

func boolOrder(value bool) int {
	if value {
		return 1
	}
	return 0
}

// addRuleNow writes a rule the user asked for: to a folder's notes, the repo's context, or the laptop files that lack it.
func (currentModel *model) addRuleNow(action brain.ChatAction) {
	if action.Target == "laptop" {
		for filePath := range currentModel.laptopTexts() {
			_, err := store.AppendRule(filePath, action.Rule)
			currentModel.reportError(err)
		}
		return
	}
	current := currentModel.repository.ReadContext(action.FolderId)
	if store.HasRule(current, action.Rule) {
		return
	}
	if strings.TrimSpace(current) == "" {
		current = "# " + currentModel.contextHeading(action.FolderId) + "\n"
	}
	currentModel.reportError(currentModel.repository.WriteContext(action.FolderId, store.AddToSection(current, "Rules", action.Rule)))
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
			theme.dim.Render(session.Tool + " agent · in " + currentModel.repository.FolderName(session.FolderId))}
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
		send = relabeled(send, "confirm the plan")
	}
	bottom = append(bottom, margin+strings.Join(currentModel.hintLines([]key.Binding{send, currentModel.keys.newChat, currentModel.keys.scrollChat, currentModel.keys.back}, contentWidth, 1), ""))
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
			theme.text.Render("Ask den about your sessions and folders, or tell it what to change: write a folder's context, make a folder, move a session."),
			theme.dim.Render("den reads " + currentModel.chatReadsText() + ". It never touches your repo or runs anything, and every change waits in a plan card for your enter."),
			theme.faint.Render("Each chat lasts until you close den or press ⌥L, and reads the row you opened it on; den remembers its last 12 messages."),
		}, width)
		lines = append(lines, "", theme.faint.Render("try:"))
		examples := []string{"make an Exploration skill folder: research only, no code changes", "which sessions worked on the eval loader?", "link docs/PLAN.md to Eval"}
		switch {
		case currentModel.chat.about != nil:
			examples = []string{"call the folder Eval pagination instead", "why do you think it's worth keeping?"}
		case currentModel.chat.subject.sessionId != "":
			examples = []string{"make a folder for this with its findings as context", "add what this session decided to its folder's notes"}
		case currentModel.chat.subject.folderId != "":
			examples = []string{"make the context from the latest session", "link docs/PLAN.md"}
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
		if len(message.Actions) > 0 {
			confirmKeys := []key.Binding{relabeled(currentModel.keys.send, "confirm"), key.NewBinding(key.WithKeys(), key.WithHelp("type", "to change it")), relabeled(currentModel.keys.back, "back")}
			for _, cardLine := range currentModel.planCard(message.Actions, message.Outcome, confirmKeys, width-chatLabelWidth) {
				lines = append(lines, strings.Repeat(" ", chatLabelWidth)+cardLine)
			}
		}
	}
	if currentModel.chat.thinking {
		lines = append(lines, "", theme.denLabel.Render(padRight("den", chatLabelWidth))+theme.faint.Render("thinking…"))
	}
	return lines
}

// chatReadsText says what den reads to answer, for the chat it's about.
func (currentModel model) chatReadsText() string {
	reads := "every folder and session by name, and the repo's notes"
	switch {
	case currentModel.chat.subject.sessionId != "":
		return "this session's latest turns, its folder's context, " + reads
	case currentModel.chat.subject.folderId != "":
		return "this folder's context, the latest turns of its newest sessions, " + reads
	}
	return "your latest session's turns, " + reads
}
