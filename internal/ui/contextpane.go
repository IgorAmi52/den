package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/tmux"
)

type contextPaneMode int

const (
	contextPaneReading contextPaneMode = iota
	contextPaneAsking
	contextPaneEditing
)

// contextPaneState is the right side of the wide view taken over for a folder's context, or the repo's:
// → from the tree enters it, ↓ asks den to change it, ↑ edits it by hand, ← goes back to the tree.
type contextPaneState struct {
	open     bool
	folderId string
	mode     contextPaneMode
	prompt   textinput.Model
	editor   textarea.Model
	// asked is what the user told den; reply is den's answer, with the change waiting for enter.
	asked    string
	reply    *brain.ChatMessage
	thinking bool
	offset   int
	// zoomed is set when entering zoomed the narrow panel, so leaving zooms back.
	zoomed bool
}

func newContextPaneState() contextPaneState {
	prompt := textinput.New()
	prompt.Prompt = "› "
	prompt.Placeholder = "ask den to change it…"
	prompt.CharLimit = 1000
	editor := textarea.New()
	editor.ShowLineNumbers = false
	editor.Prompt = ""
	editor.MaxHeight = 0
	return contextPaneState{prompt: prompt, editor: editor}
}

// hasContextPane tells whether a row's context can be opened on the right: a folder's, or the repo's.
func hasContextPane(row listRow) bool {
	return row.kind == folderRow || row.kind == repositoryRow || (row.kind == contextFileRow && row.laptopTool == "")
}

// openContextPane takes over the right side for the folder's context, zooming the narrow panel first.
func (currentModel *model) openContextPane(folderId string) {
	pane := &currentModel.contextPane
	pane.open, pane.folderId, pane.mode, pane.offset = true, folderId, contextPaneReading, 0
	pane.asked, pane.reply, pane.thinking, pane.zoomed = "", nil, false, false
	if !currentModel.isWide() && len(currentModel.openAgents) > 0 && !tmux.IsZoomed(currentModel.windowId) {
		if currentModel.reportError(tmux.ToggleZoom(currentModel.panelPaneId)) {
			return
		}
		pane.zoomed = true
	}
}

func (currentModel *model) closeContextPane() {
	pane := &currentModel.contextPane
	pane.prompt.Blur()
	pane.editor.Blur()
	pane.open = false
	if pane.zoomed && tmux.IsZoomed(currentModel.windowId) {
		currentModel.reportError(tmux.ToggleZoom(currentModel.panelPaneId))
	}
	pane.zoomed = false
}

func (currentModel model) updateContextPane(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	switch pane.mode {
	case contextPaneEditing:
		return currentModel.updateContextEditor(keyPress)
	case contextPaneAsking:
		return currentModel.updateContextPrompt(keyPress)
	}
	switch keyPress.String() {
	case "left", "esc", "q":
		currentModel.closeContextPane()
	case "down":
		pane.mode = contextPaneAsking
		return currentModel, pane.prompt.Focus()
	case "up":
		pane.mode = contextPaneEditing
		pane.editor.SetWidth(currentModel.width - currentModel.treeWidth() - 3)
		pane.editor.SetHeight(max(currentModel.treeHeight()-4, 3))
		pane.editor.SetValue(currentModel.repository.ReadContext(pane.folderId))
		return currentModel, pane.editor.Focus()
	case "alt+o":
		return currentModel, currentModel.editContext(pane.folderId)
	case "pgdown", "J":
		pane.offset += 5
	case "pgup", "K":
		pane.offset = max(pane.offset-5, 0)
	}
	return currentModel, nil
}

func (currentModel model) updateContextPrompt(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	isEmpty := pane.prompt.Value() == ""
	switch {
	case keyPress.String() == "pgdown":
		pane.offset += 5
		return currentModel, nil
	case keyPress.String() == "pgup":
		pane.offset = max(pane.offset-5, 0)
		return currentModel, nil
	case keyPress.String() == "esc", keyPress.String() == "up" && isEmpty:
		pane.prompt.Blur()
		pane.mode = contextPaneReading
		return currentModel, nil
	case keyPress.String() == "enter" && !isEmpty && !pane.thinking:
		pane.asked, pane.reply, pane.thinking = strings.TrimSpace(pane.prompt.Value()), nil, true
		pane.prompt.SetValue("")
		currentModel.queueJob(job{kind: contextJob})
		return currentModel, currentModel.startNextJob()
	case keyPress.String() == "enter" && isEmpty && pane.reply != nil && len(pane.reply.Actions) > 0:
		currentModel.doChatActions(pane.reply.Actions)
		pane.asked, pane.reply = "", nil
		currentModel.setStatus("saved · new sessions in " + currentModel.contextHeading(pane.folderId) + " start with it")
		return currentModel, nil
	case keyPress.String() == "backspace" && isEmpty && (pane.reply != nil || pane.thinking):
		pane.asked, pane.reply, pane.thinking = "", nil, false
		return currentModel, nil
	}
	var command tea.Cmd
	pane.prompt, command = pane.prompt.Update(keyPress)
	return currentModel, command
}

func (currentModel model) updateContextEditor(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	switch keyPress.String() {
	case "esc":
		pane.editor.Blur()
		pane.mode = contextPaneReading
		currentModel.setStatus("not saved")
		return currentModel, nil
	case "alt+s":
		if currentModel.reportError(currentModel.repository.WriteContext(pane.folderId, pane.editor.Value())) {
			return currentModel, nil
		}
		pane.editor.Blur()
		pane.mode = contextPaneReading
		currentModel.refreshContextTokens()
		currentModel.setStatus("saved · new sessions in " + currentModel.contextHeading(pane.folderId) + " start with it")
		return currentModel, nil
	}
	var command tea.Cmd
	pane.editor, command = pane.editor.Update(keyPress)
	return currentModel, command
}

// contextPaneInput is what den's model gets when asked to change a folder's context from its pane.
func (currentModel model) contextPaneInput() brain.ChatInput {
	pane := currentModel.contextPane
	return brain.ChatInput{
		Conversation:    []brain.ChatMessage{{Text: pane.asked}},
		Repository:      &currentModel.repository,
		Digests:         currentModel.digests,
		SubjectFolder:   pane.folderId,
		SubjectNotes:    currentModel.folderNotesText(pane.folderId),
		RepositoryNotes: currentModel.repository.ReadContext(""),
		EditingContext:  true,
		Transcripts:     currentModel.chatTranscripts(pane.folderId, ""),
		Now:             time.Now(),
	}
}

// applyContextPaneAnswer shows den's proposed change in the pane, waiting for enter.
func (currentModel *model) applyContextPaneAnswer(answer brain.ChatAnswer, input brain.ChatInput) {
	pane := &currentModel.contextPane
	pane.thinking = false
	actions := brain.ResolveChatActions(answer, input, currentModel.workingDirectory)
	pane.reply = &brain.ChatMessage{FromDen: true, Text: replyText(answer.Reply, actions), Actions: actions}
}

// contextPaneLines draw the pane: whose context and which file, the text or den's proposed change to it,
// and at the bottom the prompt to den or the hints, or the editor filling the pane.
func (currentModel model) contextPaneLines(width int, height int) []string {
	theme := currentModel.theme
	pane := &currentModel.contextPane
	innerWidth := width - 2
	text := currentModel.repository.ReadContext(pane.folderId)
	filePath, _ := currentModel.repository.FolderContextPath(pane.folderId)
	owner := "den's file"
	if currentModel.repository.IsLinked(pane.folderId) {
		owner = "your file " + filepath.Base(filePath)
	}
	header := currentModel.layoutRow([]segment{{text: "◆ " + currentModel.contextHeading(pane.folderId), style: theme.accentBold}, {text: " · context · " + owner, style: theme.dim}},
		[]segment{{text: fmt.Sprintf("~%d tokens", (len(text)+3)/4), style: theme.dim}}, innerWidth, false)
	var bottom []string
	if pane.mode == contextPaneEditing {
		editorHeight := max(height-4, 3)
		pane.editor.SetWidth(innerWidth)
		pane.editor.SetHeight(editorHeight)
		lines := []string{header, theme.border.Render(strings.Repeat("─", innerWidth))}
		lines = append(lines, strings.Split(pane.editor.View(), "\n")...)
		hints := []key.Binding{hint("⌥S", "save"), hint("esc", "cancel"), hint("⌥O", "open in $EDITOR")}
		lines = append(lines, currentModel.hintLines(hints, innerWidth, 1)...)
		return indentedLines(lines, width, height)
	}
	body := currentModel.contextPaneBody(text, innerWidth)
	bottom = append(bottom, theme.border.Render(strings.Repeat("─", innerWidth)))
	switch {
	case pane.mode == contextPaneAsking:
		if pane.asked != "" {
			bottom = append(bottom, wrapAll([]string{theme.youLabel.Render("you ") + theme.text.Render(pane.asked)}, innerWidth)...)
		}
		switch {
		case pane.thinking:
			bottom = append(bottom, theme.denLabel.Render("den ")+theme.faint.Render("thinking…"))
		case pane.reply != nil:
			bottom = append(bottom, wrapAll([]string{theme.denLabel.Render("den ") + theme.text.Render(pane.reply.Text)}, innerWidth)...)
		}
		input := pane.prompt
		input.SetWidth(innerWidth - 3)
		bottom = append(bottom, input.View())
		hints := []key.Binding{hint("↵", "send"), hint("↑", "back"), hint("pgup/pgdn", "scroll")}
		if pane.reply != nil && len(pane.reply.Actions) > 0 {
			hints = []key.Binding{hint("↵", "confirm"), hint("⌫", "discard"), hint("type", "to change it")}
		}
		bottom = append(bottom, currentModel.hintLines(hints, innerWidth, 1)...)
	default:
		bottom = append(bottom, currentModel.hintLines([]key.Binding{hint("↓", "ask den"), hint("↑", "edit by hand"), hint("←", "back"), hint("pgup/pgdn", "scroll")}, innerWidth, 1)...)
	}
	bodyHeight := max(height-1-len(bottom), 1)
	offset := min(pane.offset, max(len(body)-bodyHeight, 0))
	shown := body[offset:min(len(body), offset+bodyHeight)]
	for len(shown) < bodyHeight {
		shown = append(shown, "")
	}
	lines := append([]string{header}, shown...)
	return indentedLines(append(lines, bottom...), width, height)
}

// contextPaneBody is the context as it is, or, while den proposes a change, the change: a diff for a rewrite,
// a plan card for anything else.
func (currentModel model) contextPaneBody(text string, width int) []string {
	theme := currentModel.theme
	pane := currentModel.contextPane
	if pane.reply != nil && len(pane.reply.Actions) > 0 {
		action := pane.reply.Actions[0]
		if len(pane.reply.Actions) == 1 && action.Kind == "editNotes" && action.FolderId == pane.folderId {
			return append([]string{theme.suggestionBold.Render("✦ den's change"), ""}, currentModel.diffLines(text, action.Text, width)...)
		}
		return currentModel.planCard(pane.reply.Actions, "", nil, width)
	}
	if strings.TrimSpace(text) == "" {
		return wrapAll([]string{"", theme.dim.Render("No context yet. ↓ asks den to write it, for example \"make it from the latest session\"; ↑ writes it by hand.")}, width)
	}
	return append([]string{""}, currentModel.renderedMarkdown(text, width)...)
}

func hint(keyLabel string, description string) key.Binding {
	return key.NewBinding(key.WithKeys(), key.WithHelp(keyLabel, description))
}

// indentedLines puts every line one column in and fits them to the column's size.
func indentedLines(lines []string, width int, height int) []string {
	for index := range lines {
		lines[index] = " " + lines[index]
	}
	return fitLines(strings.Join(lines, "\n"), width, height)
}
