package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

type instructionEditorState struct {
	instruction    store.Instruction
	sourceRevision string
	fields         [3]textarea.Model
	focusedField   int
	status         string
	changed        bool
	saved          bool
}

func instructionEditVersion(instruction store.Instruction) string {
	content, _ := json.Marshal(instruction)
	return store.ContextVersion(string(content))
}

func (currentModel model) instructionEntries() ([]store.Instruction, map[string]store.InstructionDraft, error) {
	instructions, failure := currentModel.repository.Instructions()
	if failure != nil {
		return nil, nil, failure
	}
	drafts, failure := currentModel.repository.InstructionDrafts()
	if failure != nil {
		return nil, nil, failure
	}
	var identifiers []string
	for identifier := range drafts {
		identifiers = append(identifiers, identifier)
	}
	slices.Sort(identifiers)
	for _, identifier := range identifiers {
		position := slices.IndexFunc(instructions, func(instruction store.Instruction) bool { return instruction.Identifier == identifier })
		if position < 0 {
			instructions = append(instructions, drafts[identifier].Instruction)
		} else {
			instructions[position] = drafts[identifier].Instruction
		}
	}
	return instructions, drafts, nil
}

func (currentModel *model) openInstructionEditor(instruction store.Instruction, draft *store.InstructionDraft) tea.Cmd {
	form := &instructionEditorState{instruction: instruction, status: "Changes save automatically", saved: true}
	if instruction.Identifier == "" {
		form.instruction.Identifier = store.ContextVersion(fmt.Sprint(time.Now().UnixNano()))[:12]
		form.instruction.Scope = "repo"
	} else {
		form.sourceRevision = instructionEditVersion(instruction)
	}
	if draft != nil {
		form.sourceRevision = draft.SourceRevision
		form.status = "Draft restored"
	}
	values := []string{instruction.Title, instruction.Text, strings.Join(instruction.Examples, "\n")}
	placeholders := []string{"Short name", "What should Den notice and maintain?", "One example per line; Den can suggest these"}
	for position := range form.fields {
		field := textarea.New()
		field.ShowLineNumbers, field.Prompt, field.MaxHeight = false, "", 0
		field.Placeholder = placeholders[position]
		field.SetValue(values[position])
		form.fields[position] = field
	}
	currentModel.contextPane.instructionEditor = form
	return form.fields[0].Focus()
}

func (currentModel model) updateInstructionList(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pane := &currentModel.contextPane
	instructions, drafts, failure := currentModel.instructionEntries()
	if failure != nil && keyPress.String() != "m" && keyPress.String() != "alt+o" && keyPress.String() != "esc" && keyPress.String() != "left" {
		currentModel.reportError(failure)
		return currentModel, nil
	}
	pane.instructionCursor = min(pane.instructionCursor, max(0, len(instructions)-1))
	switch keyPress.String() {
	case "esc", "left", "q":
		currentModel.closeContextPane()
	case "up", "k":
		pane.instructionCursor = max(0, pane.instructionCursor-1)
		pane.offset = 0
	case "down", "j":
		pane.instructionCursor = min(max(0, len(instructions)-1), pane.instructionCursor+1)
		pane.offset = 0
	case "enter", "e":
		if len(instructions) > 0 {
			instruction := instructions[pane.instructionCursor]
			if draft, found := drafts[instruction.Identifier]; found {
				return currentModel, currentModel.openInstructionEditor(instruction, &draft)
			}
			return currentModel, currentModel.openInstructionEditor(instruction, nil)
		}
		return currentModel, currentModel.openInstructionEditor(store.Instruction{}, nil)
	case "n":
		return currentModel, currentModel.openInstructionEditor(store.Instruction{}, nil)
	case "space", "d":
		if len(instructions) == 0 {
			return currentModel, nil
		}
		instruction := instructions[pane.instructionCursor]
		if keyPress.String() == "d" {
			currentModel.confirming = &confirmation{question: "Delete this instruction?", detail: instruction.Title,
				accept: func(currentModel *model) tea.Cmd {
					currentModel.changeListedInstruction(instruction.Identifier, true)
					return nil
				}}
		} else if _, found := drafts[instruction.Identifier]; found {
			currentModel.setStatus("Finish or discard the draft before changing its status")
		} else {
			currentModel.changeListedInstruction(instruction.Identifier, false)
		}
	case "x":
		if len(instructions) > 0 {
			instruction := instructions[pane.instructionCursor]
			if _, found := drafts[instruction.Identifier]; found {
				currentModel.confirming = &confirmation{question: "Discard this draft?", detail: "The last active version will be kept.",
					accept: func(currentModel *model) tea.Cmd {
						currentModel.reportError(currentModel.repository.RemoveInstructionDraft(instruction.Identifier))
						return nil
					}}
			}
		}
	case "c":
		pane.mode = contextPaneAsking
		return currentModel, pane.prompt.Focus()
	case "s":
		currentModel.chooseInstructionStarters()
	case "m":
		return currentModel.beginContextEditing()
	case "alt+o":
		return currentModel, currentModel.editInstructions()
	case "pgdown", "shift+down":
		pane.offset += 5
	case "pgup", "shift+up":
		pane.offset = max(0, pane.offset-5)
	}
	return currentModel, nil
}

func (currentModel *model) changeListedInstruction(identifier string, remove bool) {
	text, failure := currentModel.repository.ReadInstructions()
	if currentModel.reportError(failure) {
		return
	}
	instructions, failure := store.ParseInstructions(text)
	if currentModel.reportError(failure) {
		return
	}
	if remove {
		instructions = slices.DeleteFunc(instructions, func(instruction store.Instruction) bool { return instruction.Identifier == identifier })
	} else {
		for position := range instructions {
			if instructions[position].Identifier == identifier {
				instructions[position].Paused = !instructions[position].Paused
			}
		}
	}
	if currentModel.reportError(currentModel.repository.SaveInstructions(store.FormatInstructions(instructions), store.ContextVersion(text))) {
		return
	}
	if remove {
		currentModel.reportError(currentModel.repository.RemoveInstructionDraft(identifier))
	}
	currentModel.contextPaneSaved()
}

func (currentModel model) updateInstructionEditor(message tea.Msg) (tea.Model, tea.Cmd) {
	form := currentModel.contextPane.instructionEditor
	if keyPress, found := message.(tea.KeyPressMsg); found {
		switch keyPress.String() {
		case "tab", "shift+tab":
			form.fields[form.focusedField].Blur()
			step := 1
			if keyPress.String() == "shift+tab" {
				step = 2
			}
			form.focusedField = (form.focusedField + step) % len(form.fields)
			return currentModel, form.fields[form.focusedField].Focus()
		case "esc":
			if !form.saved && !currentModel.saveInstructionEditor() {
				return currentModel, nil
			}
			if currentModel.helperCaller != nil && form.changed && form.sourceRevision != "" && len(form.instruction.ApprovedExamples()) == 0 && form.status == "Saved automatically" {
				return currentModel, currentModel.suggestInstructionExamples()
			}
			form.fields[form.focusedField].Blur()
			currentModel.contextPane.instructionEditor = nil
			return currentModel, nil
		case "ctrl+g":
			return currentModel, currentModel.suggestInstructionExamples()
		}
	}
	before := form.fields[form.focusedField].Value()
	var command tea.Cmd
	form.fields[form.focusedField], command = form.fields[form.focusedField].Update(message)
	if before != form.fields[form.focusedField].Value() {
		form.changed, form.saved = true, false
		currentModel.saveInstructionEditor()
	}
	return currentModel, command
}

func (currentModel *model) saveInstructionEditor() bool {
	form := currentModel.contextPane.instructionEditor
	instruction := form.instruction
	instruction.Title, instruction.Text = strings.TrimSpace(form.fields[0].Value()), strings.TrimSpace(form.fields[1].Value())
	instruction.Examples = nil
	for _, line := range strings.Split(form.fields[2].Value(), "\n") {
		if example := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- ")); example != "" {
			instruction.Examples = append(instruction.Examples, example)
		}
	}
	if !slices.Equal(instruction.Examples, form.instruction.Examples) {
		instruction.ExamplesRevision = instruction.BehaviorVersion()
	}
	if currentModel.reportError(currentModel.repository.SaveInstructionDraft(store.InstructionDraft{Instruction: instruction, SourceRevision: form.sourceRevision})) {
		form.status = "Could not save draft"
		return false
	}
	form.saved, form.instruction = true, instruction
	form.status = "Draft saved · add a title and instruction to activate"
	if instruction.Title == "" || instruction.Text == "" {
		return true
	}
	text, failure := currentModel.repository.ReadInstructions()
	if failure != nil {
		form.status = "Draft saved · " + failure.Error()
		return true
	}
	instructions, failure := store.ParseInstructions(text)
	if failure != nil {
		form.status = "Draft saved · repair the Markdown file before applying"
		return true
	}
	position := slices.IndexFunc(instructions, func(candidate store.Instruction) bool { return candidate.Identifier == instruction.Identifier })
	if (position < 0 && form.sourceRevision != "") || (position >= 0 && instructionEditVersion(instructions[position]) != form.sourceRevision) {
		form.status = "Draft saved · instruction changed elsewhere; resolve in Markdown"
		return true
	}
	if position < 0 {
		instructions = append(instructions, instruction)
	} else {
		instructions[position] = instruction
	}
	formatted := store.FormatInstructions(instructions)
	parsed, failure := store.ParseInstructions(formatted)
	if failure != nil || len(parsed) != len(instructions) || !slices.EqualFunc(parsed, instructions, func(first, second store.Instruction) bool { return first.Revision() == second.Revision() }) {
		form.status = "Draft saved · use plain text here; Markdown headings separate instructions"
		return true
	}
	if failure := currentModel.repository.SaveInstructions(formatted, store.ContextVersion(text)); failure != nil {
		form.status = "Draft saved · " + failure.Error()
		return true
	}
	form.instruction = parsed[slices.IndexFunc(parsed, func(candidate store.Instruction) bool { return candidate.Identifier == instruction.Identifier })]
	form.sourceRevision = instructionEditVersion(form.instruction)
	if currentModel.reportError(currentModel.repository.RemoveInstructionDraft(instruction.Identifier)) {
		form.status = "Saved · could not clear the draft"
		return true
	}
	form.status = "Saved automatically"
	currentModel.contextPane.instructionCursor = slices.IndexFunc(parsed, func(candidate store.Instruction) bool {
		return candidate.Identifier == instruction.Identifier
	})
	return true
}

func (currentModel *model) suggestInstructionExamples() tea.Cmd {
	pane := &currentModel.contextPane
	form := pane.instructionEditor
	if !currentModel.saveInstructionEditor() || form.status != "Saved automatically" {
		currentModel.setStatus("Finish the instruction before asking for examples")
		return nil
	}
	if currentModel.helperCaller == nil {
		currentModel.setError("A connected model is needed to suggest examples. Your instruction is saved.")
		return nil
	}
	instruction := form.instruction
	form.fields[form.focusedField].Blur()
	pane.instructionEditor, pane.mode, pane.conversation = nil, contextPaneAsking, nil
	pane.asked = fmt.Sprintf("Suggest two or three trigger examples for %q. Keep its title, instruction and scope unchanged, and show the examples for approval.", instruction.Title)
	pane.conversation = []brain.ChatMessage{{Text: pane.asked + "\nExisting instruction identifier: " + instruction.Identifier}}
	pane.reply, pane.thinking = nil, true
	pane.generation++
	currentModel.queueJob(job{kind: contextJob, contextGeneration: pane.generation})
	return tea.Batch(pane.prompt.Focus(), currentModel.startNextJob())
}

func (currentModel model) instructionListLines(width int, height int) []string {
	theme := currentModel.theme
	innerWidth := width - 2
	instructions, drafts, failure := currentModel.instructionEntries()
	lines := []string{theme.accentBold.Render("Instructions"), ""}
	if failure != nil {
		lines = append(lines, wrapText("Repair instructions with m: "+failure.Error(), innerWidth)...)
	} else if len(instructions) == 0 {
		lines = append(lines, wrapText("Add an instruction with n, choose starters with s, or ask Den with c.", innerWidth)...)
	} else {
		cursor := min(currentModel.contextPane.instructionCursor, len(instructions)-1)
		visible := max(3, height/3)
		start := max(0, cursor-visible+1)
		for position := start; position < min(len(instructions), start+visible); position++ {
			instruction := instructions[position]
			status := "active"
			if instruction.Paused {
				status = "paused"
			}
			if _, found := drafts[instruction.Identifier]; found {
				status = "draft"
			}
			title := instruction.Title
			if title == "" {
				title = "Untitled instruction"
			}
			line := truncate(title+" · "+status, innerWidth-2)
			if position == cursor {
				lines = append(lines, theme.accentBold.Render("▸ "+line))
			} else {
				lines = append(lines, "  "+line)
			}
		}
		details := currentModel.renderedMarkdown(brain.InstructionPreview(instructions[cursor], &currentModel.repository), innerWidth)
		available := max(1, height-len(lines)-5)
		offset := min(currentModel.contextPane.offset, max(0, len(details)-available))
		lines = append(lines, "")
		lines = append(lines, details[offset:min(len(details), offset+available)]...)
	}
	bindings := []key.Binding{hint("↑↓", "choose"), hint("↵", "edit"), hint("n", "new"), hint("space", "pause/resume"), hint("d", "delete"), hint("c", "ask Den"), hint("s", "starters"), hint("m", "Markdown"), hint("esc", "back")}
	if len(drafts) > 0 {
		bindings = append(bindings, hint("x", "discard draft"))
	}
	hints := currentModel.hintLines(bindings, innerWidth, 4)
	lines = fitLines(strings.Join(lines, "\n"), innerWidth, max(1, height-len(hints)))
	return indentedLines(append(lines, hints...), width, height)
}

func (currentModel model) instructionEditorLines(width int, height int) []string {
	theme := currentModel.theme
	form := currentModel.contextPane.instructionEditor
	innerWidth := width - 2
	lines := []string{theme.accentBold.Render("Edit instruction"), theme.dim.Render("Scope: " + form.instruction.Scope), ""}
	labels := []string{"Title", "Instruction", "Examples · one per line"}
	remaining := max(6, height-12)
	heights := []int{1, remaining / 2, remaining - remaining/2}
	for position := range form.fields {
		label := labels[position]
		if position == form.focusedField {
			label = theme.accentBold.Render(label)
		} else {
			label = theme.bold.Render(label)
		}
		lines = append(lines, label)
		form.fields[position].SetWidth(innerWidth)
		form.fields[position].SetHeight(heights[position])
		lines = append(lines, strings.Split(form.fields[position].View(), "\n")...)
		lines = append(lines, "")
	}
	hints := currentModel.hintLines([]key.Binding{hint("tab", "next field"), hint("esc", "back"), hint("ctrl+g", "suggest examples")}, innerWidth, 2)
	bottom := append([]string{theme.dim.Render(truncate(form.status, innerWidth))}, hints...)
	lines = fitLines(strings.Join(lines, "\n"), innerWidth, max(1, height-len(bottom)))
	return indentedLines(append(lines, bottom...), width, height)
}
