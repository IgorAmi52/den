package ui

import (
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
	// scrollOffset is how many lines the conversation is scrolled up from the newest message.
	scrollOffset int
}

type chatReplyMessage struct {
	reply brain.ChatMessage
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
	state.scrollOffset = 0
}

// openChat starts a chat, about a suggestion when one is given; a new subject starts a new conversation.
func (currentModel *model) openChat(about *store.Suggestion) tea.Cmd {
	state := &currentModel.chat
	isSameSubject := (about == nil && state.about == nil) || (about != nil && state.about != nil && about.Id == state.about.Id)
	if !isSameSubject {
		state.messages = nil
	}
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
				return currentModel, nil
			}
			state.input.SetValue("")
			state.addMessage(brain.ChatMessage{Text: text})
			conversation := append([]brain.ChatMessage{}, state.messages...)
			about := state.about
			return currentModel, func() tea.Msg {
				return chatReplyMessage{reply: brain.Reply(conversation, about)}
			}
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

func (currentModel model) chatView() string {
	theme := currentModel.theme
	width := currentModel.width
	state := currentModel.chat
	contentWidth := min(width-4, 100)
	margin := strings.Repeat(" ", max((width-contentWidth)/2, 2))
	top := []string{currentModel.screenTopBar("ask den", width), theme.border.Render(strings.Repeat("─", width))}
	if state.about != nil {
		label, summary := currentModel.suggestionSummary(*state.about)
		pinned := []string{theme.suggestionBold.Render("✦ about this suggestion"), theme.text.Render(label + " · " + summary)}
		if state.about.Reason != "" {
			pinned = append(pinned, theme.dim.Render(truncate(state.about.Reason, contentWidth-2)))
		}
		top = append(top, "")
		for _, line := range strings.Split(theme.pinned.Render(strings.Join(pinned, "\n")), "\n") {
			top = append(top, margin+line)
		}
	}
	bottom := []string{theme.border.Render(strings.Repeat("─", width)), margin + state.input.View()}
	bottom = append(bottom, margin+strings.Join(currentModel.hintLines([]key.Binding{currentModel.keys.send, currentModel.keys.scrollChat, currentModel.keys.back}, contentWidth, 1), ""))
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
		examples := []string{"which sessions worked on the eval loader?", "what does Eval's context say about pagination?"}
		if currentModel.chat.about != nil {
			examples = []string{"put it in Agent API instead", "why do you think it belongs there?"}
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
