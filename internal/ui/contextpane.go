package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
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
	generation        int
	instructions      bool
	starters          *instructionStarterState
	instructionEditor *instructionEditorState
	instructionCursor int
	editorVersion     string
	editorSavedValue  string
	editorSaveStatus  string
	conversation      []brain.ChatMessage
	open              bool
	folderId          string
	mode              contextPaneMode
	prompt            textarea.Model
	editor            textarea.Model
	// asked is what the user told den; reply is den's answer, with the change waiting for enter.
	asked    string
	reply    *brain.ChatMessage
	thinking bool
	offset   int
	// zoomed is set when entering zoomed the narrow panel, so leaving zooms back.
	zoomed bool
}

func newContextPaneState() contextPaneState {
	prompt := newMessageInput("ask den to change it…", 1000)
	editor := textarea.New()
	editor.ShowLineNumbers = false
	editor.Prompt = ""
	editor.MaxHeight = 0
	return contextPaneState{prompt: prompt, editor: editor}
}

// hasContextPane tells whether a row's context can be opened on the right: a folder's, or the repo's.
func hasContextPane(row listRow) bool {
	return row.kind == instructionsRow || row.kind == folderRow || row.kind == repositoryRow || (row.kind == contextFileRow && row.laptopTool == "")
}

// openContextPane takes over the right side for the folder's context, zooming the narrow panel first.
func (currentModel *model) openContextPane(folderId string) {
	pane := &currentModel.contextPane
	pane.generation++
	pane.instructions, pane.conversation = false, nil
	pane.starters = nil
	pane.instructionEditor, pane.instructionCursor = nil, 0
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
	if pane.instructionEditor != nil {
		pane.instructionEditor.fields[pane.instructionEditor.focusedField].Blur()
		pane.instructionEditor = nil
	}
	pane.open = false
	pane.generation++
	if pane.zoomed && tmux.IsZoomed(currentModel.windowId) {
		currentModel.reportError(tmux.ToggleZoom(currentModel.panelPaneId))
	}
	pane.zoomed = false
}

func (currentModel model) updateContextPane(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	if pane.starters != nil {
		return currentModel.updateInstructionStarters(keyPress)
	}
	if pane.instructionEditor != nil {
		return currentModel.updateInstructionEditor(keyPress)
	}
	switch pane.mode {
	case contextPaneEditing:
		return currentModel.updateContextEditor(keyPress)
	case contextPaneAsking:
		return currentModel.updateContextPrompt(keyPress)
	}
	if pane.instructions {
		return currentModel.updateInstructionList(keyPress)
	}
	switch keyPress.String() {
	case "left", "esc", "q":
		currentModel.closeContextPane()
	case "down":
		pane.mode = contextPaneAsking
		return currentModel, pane.prompt.Focus()
	case "up":
		return currentModel.beginContextEditing()
	case "alt+o":
		if pane.instructions {
			return currentModel, currentModel.editInstructions()
		}
		return currentModel, currentModel.editContext(pane.folderId)
	case "pgdown", "shift+down", "J":
		pane.offset += 5
	case "pgup", "shift+up", "K":
		pane.offset = max(pane.offset-5, 0)
	}
	return currentModel, nil
}

func (currentModel model) beginContextEditing() (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	pane.mode = contextPaneEditing
	pane.editor.SetWidth(currentModel.width - currentModel.treeWidth() - 3)
	pane.editor.SetHeight(max(currentModel.treeHeight()-4, 3))
	text := currentModel.contextPaneText()
	pane.editorVersion = store.ContextVersion(text)
	if pane.instructions && text == "" {
		text = store.InstructionsTemplate
	}
	pane.editor.SetValue(text)
	pane.editorSavedValue = pane.editor.Value()
	pane.editorSaveStatus = "Changes save automatically"
	return currentModel, pane.editor.Focus()
}

func (currentModel model) updateContextPrompt(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	isEmpty := pane.prompt.Value() == ""
	switch {
	case keyPress.String() == "pgdown", keyPress.String() == "shift+down":
		pane.offset += 5
		return currentModel, nil
	case keyPress.String() == "pgup", keyPress.String() == "shift+up":
		pane.offset = max(pane.offset-5, 0)
		return currentModel, nil
	case keyPress.String() == "esc", keyPress.String() == "up" && isEmpty:
		pane.prompt.Blur()
		pane.mode = contextPaneReading
		return currentModel, nil
	case keyPress.String() == "enter" && !isEmpty && !pane.thinking:
		pane.asked, pane.reply, pane.thinking = strings.TrimSpace(pane.prompt.Value()), nil, true
		pane.prompt.SetValue("")
		pane.conversation = append(pane.conversation, brain.ChatMessage{Text: pane.asked})
		pane.generation++
		currentModel.queueJob(job{kind: contextJob, contextGeneration: pane.generation})
		return currentModel, currentModel.startNextJob()
	case keyPress.String() == "enter" && isEmpty && pane.reply != nil && len(pane.reply.Actions) > 0:
		if len(currentModel.doChatActions(pane.reply.Actions)) == 0 {
			return currentModel, nil
		}
		pane.asked, pane.reply = "", nil
		if len(pane.conversation) > 0 {
			pane.conversation[len(pane.conversation)-1].Outcome = planDone
		}
		currentModel.contextPaneSaved()
		return currentModel, nil
	case keyPress.String() == "backspace" && isEmpty && (pane.reply != nil || pane.thinking):
		pane.asked, pane.reply, pane.thinking = "", nil, false
		pane.generation++
		if len(pane.conversation) > 0 {
			pane.conversation[len(pane.conversation)-1].Outcome = planSkipped
		}
		return currentModel, nil
	}
	var command tea.Cmd
	pane.prompt, command = pane.prompt.Update(keyPress)
	return currentModel, command
}

func (currentModel model) updateContextEditor(message tea.Msg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	if keyPress, isKeyPress := message.(tea.KeyPressMsg); isKeyPress {
		switch keyPress.String() {
		case "esc", "alt+o":
			if !currentModel.saveContextEditor(true) {
				if keyPress.String() == "esc" {
					currentModel.confirming = &confirmation{
						question: "Leave without saving these edits?",
						detail:   "Your unsaved editor text will be discarded.",
						accept: func(currentModel *model) tea.Cmd {
							currentModel.contextPane.editor.Blur()
							currentModel.contextPane.mode = contextPaneReading
							return nil
						},
					}
				}
				return currentModel, nil
			}
			pane.editor.Blur()
			pane.mode = contextPaneReading
			currentModel.refreshContextTokens()
			if keyPress.String() == "alt+o" {
				if pane.instructions {
					return currentModel, currentModel.editInstructions()
				}
				return currentModel, currentModel.editContext(pane.folderId)
			}
			return currentModel, nil
		}
	}
	var command tea.Cmd
	pane.editor, command = pane.editor.Update(message)
	if pane.editor.Value() != pane.editorSavedValue {
		currentModel.saveContextEditor(false)
	}
	return currentModel, command
}

func (currentModel *model) saveContextEditor(formatInstructions bool) bool {
	pane := &currentModel.contextPane
	text := pane.editor.Value()
	if text == pane.editorSavedValue && !formatInstructions {
		return true
	}
	path, failure := currentModel.repository.FolderContextPath(pane.folderId)
	if pane.instructions {
		path, failure = currentModel.repository.InstructionsPath()
		if formatInstructions {
			if instructions, parseFailure := store.ParseInstructions(text); parseFailure == nil {
				text = store.FormatInstructions(instructions)
			}
		}
	}
	if failure == nil {
		var content []byte
		content, failure = os.ReadFile(path)
		if os.IsNotExist(failure) {
			failure = nil
		}
		if failure == nil && store.ContextVersion(string(content)) != pane.editorVersion {
			failure = fmt.Errorf("file changed outside this editor; reopen it before editing further")
		}
	}
	if failure == nil {
		failure = store.WriteText(path, text)
	}
	if failure != nil {
		pane.editorSaveStatus = "Not saved: " + failure.Error()
		currentModel.reportError(failure)
		return false
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	pane.editorVersion = store.ContextVersion(text)
	pane.editorSavedValue = pane.editor.Value()
	pane.editorSaveStatus = "Saved automatically"
	if pane.instructions {
		if _, failure := currentModel.repository.Instructions(); failure != nil {
			pane.editorSaveStatus = "Draft saved: " + failure.Error()
			currentModel.setStatus(pane.editorSaveStatus)
			return true
		}
	}
	currentModel.contextPaneSaved()
	return true
}

// contextPaneInput is what den's model gets when asked to change a folder's context from its pane.
func (currentModel model) contextPaneInput() brain.ChatInput {
	pane := currentModel.contextPane
	if pane.instructions {
		return brain.ChatInput{Conversation: pane.conversation, Repository: &currentModel.repository, EditingInstructions: true, Now: time.Now()}
	}
	return brain.ChatInput{
		Conversation:    pane.conversation,
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
	text := replyText(answer.Reply, actions)
	if brain.InvalidInstructionDraft(answer, actions) {
		text = "That instruction draft did not pass validation. Nothing was saved. Ask me to revise its format or scope."
	}
	pane.reply = &brain.ChatMessage{FromDen: true, Text: text, Actions: actions}
	pane.conversation = append(pane.conversation, *pane.reply)
}

// contextPaneLines draw the pane: whose context and which file, the text or den's proposed change to it,
// and at the bottom the prompt to den or the hints, or the editor filling the pane.
func (currentModel model) contextPaneLines(width int, height int) []string {
	if currentModel.contextPane.starters != nil {
		return currentModel.instructionStarterLines(width, height)
	}
	if currentModel.contextPane.instructionEditor != nil {
		return currentModel.instructionEditorLines(width, height)
	}
	if currentModel.contextPane.instructions && currentModel.contextPane.mode == contextPaneReading {
		return currentModel.instructionListLines(width, height)
	}
	theme := currentModel.theme
	pane := &currentModel.contextPane
	innerWidth := width - 2
	text := currentModel.contextPaneText()
	filePath, _ := currentModel.repository.FolderContextPath(pane.folderId)
	owner := "den's file"
	if currentModel.repository.IsLinked(pane.folderId) {
		owner = "your file " + filepath.Base(filePath)
	}
	heading := currentModel.contextHeading(pane.folderId)
	if pane.instructions {
		heading, owner = "Instructions", "what Den notices"
	}
	header := currentModel.layoutRow([]segment{{text: "◆ " + heading, style: theme.accentBold}, {text: " · context · " + owner, style: theme.dim}},
		[]segment{{text: fmt.Sprintf("~%d tokens", (len(text)+3)/4), style: theme.dim}}, innerWidth, false)
	if pane.instructions {
		header = currentModel.layoutRow([]segment{{text: "Instructions", style: theme.accentBold}, {text: " · what Den notices", style: theme.dim}}, nil, innerWidth, false)
	}
	var bottom []string
	if pane.mode == contextPaneEditing {
		editorHeight := max(height-5, 3)
		pane.editor.SetWidth(innerWidth)
		pane.editor.SetHeight(editorHeight)
		lines := []string{header, theme.border.Render(strings.Repeat("─", innerWidth))}
		lines = append(lines, strings.Split(pane.editor.View(), "\n")...)
		lines = append(lines, theme.dim.Render(truncate(pane.editorSaveStatus, innerWidth)))
		hints := []key.Binding{hint("esc", "back"), hint("⌥O", "open in $EDITOR")}
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
		input.SetWidth(innerWidth)
		bottom = append(bottom, strings.Split(input.View(), "\n")...)
		hints := []key.Binding{hint("↵", "send"), hint("↑", "back"), hint("⇧↑↓", "scroll")}
		if pane.reply != nil && len(pane.reply.Actions) > 0 {
			hints = []key.Binding{hint("↵", "confirm"), hint("⌫", "discard"), hint("type", "to change it")}
		}
		bottom = append(bottom, currentModel.hintLines(hints, innerWidth, 1)...)
	default:
		hints := []key.Binding{hint("↓", "ask den"), hint("↑", "edit by hand"), hint("←", "back"), hint("⇧↑↓", "scroll")}
		if pane.instructions {
			hints = append(hints, hint("s", "starters"))
		}
		bottom = append(bottom, currentModel.hintLines(hints, innerWidth, 2)...)
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
	if pane.instructions {
		instructions, failure := currentModel.repository.Instructions()
		if failure != nil {
			return wrapAll([]string{"Instructions need editing: " + failure.Error(), "", text}, width)
		}
		var previews []string
		for _, instruction := range instructions {
			previews = append(previews, brain.InstructionPreview(instruction, &currentModel.repository))
		}
		text = strings.Join(previews, "\n---\n")
	}
	if strings.TrimSpace(text) == "" {
		if pane.instructions {
			return wrapAll([]string{"", theme.dim.Render("Teach Den what to notice. ↓ drafts an instruction and illustrative examples; ↑ writes one by hand. For example: Catch issues mentioned in my sessions and keep their status current.")}, width)
		}
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

func (currentModel model) contextPaneText() string {
	if currentModel.contextPane.instructions {
		text, failure := currentModel.repository.ReadInstructions()
		if failure != nil {
			return failure.Error()
		}
		return text
	}
	return currentModel.repository.ReadContext(currentModel.contextPane.folderId)
}

func (currentModel *model) contextPaneSaved() {
	if currentModel.contextPane.instructions {
		currentModel.setStatus("saved · Den follows these instructions for new exchanges")
		return
	}
	currentModel.setStatus("saved · new sessions in " + currentModel.contextHeading(currentModel.contextPane.folderId) + " start with it")
}

func (currentModel *model) editInstructions() tea.Cmd {
	path, failure := currentModel.repository.InstructionsPath()
	if currentModel.reportError(failure) {
		return nil
	}
	return currentModel.editFile("Den instructions", path, store.InstructionsTemplate, func(currentModel *model, text string) {
		if currentModel.reportError(currentModel.repository.SaveInstructions(text, store.ContextVersion(text))) {
			return
		}
		currentModel.setStatus("Den instructions saved")
	})
}
