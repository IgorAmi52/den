package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

const chatLabelWidth = 6

// maximumMessageLines is how tall a message typed to den grows before it scrolls.
const maximumMessageLines = 5

// chatState is a conversation with den itself: to ask about the sessions, or to say what to change in a suggestion.
// den answers and proposes; it never acts on the repo.
type chatState struct {
	messages []brain.ChatMessage
	input    textarea.Model
	// about is the suggestion the chat was opened on, pinned above the conversation.
	about *store.Suggestion
	// scrollOffset is how many lines the conversation is scrolled up from the newest message.
	scrollOffset int
	// pendingActions are changes den offered for what the user asked; enter on an empty line makes them.
	pendingActions []brain.ChatAction
	// thinking is set while den's model works on a reply.
	thinking bool
}

func newChatState() chatState {
	return chatState{input: newMessageInput("where did I leave off? or tell den what to change", 2000)}
}

// newMessageInput is where the user types to den: a long message wraps onto more lines, growing up to
// maximumMessageLines, and enter sends it instead of starting a new line.
func newMessageInput(placeholder string, characterLimit int) textarea.Model {
	input := textarea.New()
	input.Placeholder = placeholder
	input.CharLimit = characterLimit
	input.ShowLineNumbers = false
	input.DynamicHeight = true
	input.MaxHeight = maximumMessageLines
	input.KeyMap.InsertNewline.SetEnabled(false)
	input.SetPromptFunc(2, func(prompt textarea.PromptInfo) string {
		if prompt.LineNumber == 0 {
			return "› "
		}
		return ""
	})
	styles := input.Styles()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)
	return input
}

func (state *chatState) resize(width int) {
	state.input.SetWidth(chatContentWidth(width))
}

// chatContentWidth is the centered column the conversation and the input share.
func chatContentWidth(width int) int {
	return min(width-4, 100)
}

func (state *chatState) addMessage(message brain.ChatMessage) {
	state.messages = append(state.messages, message)
	state.pendingActions = message.Actions
	state.scrollOffset = 0
}

// openChat starts a chat about the whole repo, or about a suggestion; the conversation carries over until the user starts a new one.
func (currentModel *model) openChat(about *store.Suggestion) tea.Cmd {
	state := &currentModel.chat
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
				currentModel.logSkippedPlan(state.pendingActions, text)
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
	input.Transcripts = currentModel.chatTranscripts("", "")
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
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "chat", Action: "reply", Detail: fmt.Sprintf("%d actions", len(actions)), Outcome: "proposed", CallId: input.CallId}))
}

// applyChatActions makes the changes the user asked for in the chat and confirmed with enter.
func (currentModel *model) applyChatActions(actions []brain.ChatAction) {
	currentModel.doChatActions(actions)
	currentModel.chat.settlePlan(planDone)
}

// logSkippedPlan records a plan the user wrote past instead of confirming, with what they wrote instead.
func (currentModel *model) logSkippedPlan(actions []brain.ChatAction, instead string) {
	var descriptions []string
	for _, action := range actions {
		descriptions = append(descriptions, action.Description)
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "chat", Action: "plan", CallId: actions[0].CallId,
		Detail: strings.Join(descriptions, "; ") + " · user wrote instead: " + instead, Outcome: "skipped"}))
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
			// Back to the tree, where the agent picker shows and the new agent's row appears.
			currentModel.chat.input.Blur()
			for len(currentModel.screens) > 0 {
				currentModel.closeScreen()
			}
		case "addRule":
			currentModel.addRuleNow(action)
		case "moveNotes":
			currentModel.moveNotes(action.FromFolderId, action.FolderId)
		case "editNotes":
			currentModel.reportError(repository.WriteContext(action.FolderId, action.Text))
		}
		done = append(done, action.Description)
		currentModel.reportError(repository.LogDecision(store.Decision{Job: "chat", SessionId: action.SessionId, FolderId: action.FolderId, Action: action.Kind, Detail: action.Description, Outcome: "applied", CallId: action.CallId}))
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
	contentWidth := chatContentWidth(width)
	margin := strings.Repeat(" ", max((width-contentWidth)/2, 2))
	top := []string{currentModel.screenTopBar("ask den", width), theme.border.Render(strings.Repeat("─", width))}
	var pinned []string
	if state.about != nil {
		label, summary := currentModel.suggestionSummary(*state.about)
		pinned = []string{theme.suggestionBold.Render("✦ about this suggestion"), theme.text.Render(label + " · " + summary)}
		if state.about.Reason != "" {
			pinned = append(pinned, theme.dim.Render(truncate(state.about.Reason, contentWidth-2)))
		}
	}
	if len(pinned) > 0 {
		top = append(top, "")
		for _, line := range strings.Split(theme.pinned.Render(strings.Join(pinned, "\n")), "\n") {
			top = append(top, margin+line)
		}
	}
	bottom := []string{theme.border.Render(strings.Repeat("─", width))}
	for _, line := range strings.Split(state.input.View(), "\n") {
		bottom = append(bottom, margin+line)
	}
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
			theme.text.Render("Ask den about your sessions."),
			"",
			theme.dim.Render("  recall    ask where any session left off"),
			theme.dim.Render("  resume    start a new session that picks the work back up"),
			theme.dim.Render("  organize  make folders, write their context, move sessions"),
			"",
			theme.faint.Render("den only changes things after you approve a plan card. It never edits your repo or runs commands."),
		}, width)
		lines = append(lines, "", theme.faint.Render("try:"))
		examples := []string{"where did I leave off yesterday?", "start a session that picks up where that stopped", "make an Exploration skill folder: research only, no code changes"}
		switch {
		case currentModel.chat.about != nil:
			examples = []string{"call the folder Eval pagination instead", "why do you think it's worth keeping?"}
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
