package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
)

type instructionStarterState struct {
	instructions []store.Instruction
	selected     []bool
	cursor       int
}

func (currentModel *model) openInstructionsPane() {
	currentModel.openContextPane("")
	currentModel.contextPane.instructions = true
	instructions, _, failure := currentModel.instructionEntries()
	if failure == nil && len(instructions) == 0 {
		currentModel.chooseInstructionStarters()
	}
}

func (currentModel *model) chooseInstructionStarters() {
	instructions, failure := currentModel.repository.AvailableStarterInstructions()
	if currentModel.reportError(failure) {
		return
	}
	if len(instructions) == 0 {
		currentModel.setStatus("All starter instructions are already present")
		return
	}
	currentModel.contextPane.starters = &instructionStarterState{instructions: instructions, selected: make([]bool, len(instructions))}
	currentModel.contextPane.offset = 0
}

func (currentModel model) updateInstructionStarters(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	starters := currentModel.contextPane.starters
	switch keyPress.String() {
	case "esc", "left":
		currentModel.contextPane.starters = nil
	case "up", "k":
		starters.cursor = max(0, starters.cursor-1)
		currentModel.contextPane.offset = 0
	case "down", "j":
		starters.cursor = min(len(starters.instructions)-1, starters.cursor+1)
		currentModel.contextPane.offset = 0
	case "pgdown", "shift+down":
		currentModel.contextPane.offset += 5
	case "pgup", "shift+up":
		currentModel.contextPane.offset = max(0, currentModel.contextPane.offset-5)
	case "space":
		starters.selected[starters.cursor] = !starters.selected[starters.cursor]
	case "enter":
		text, failure := currentModel.repository.ReadInstructions()
		if currentModel.reportError(failure) {
			return currentModel, nil
		}
		instructions, failure := store.ParseInstructions(text)
		if currentModel.reportError(failure) {
			return currentModel, nil
		}
		available, failure := currentModel.repository.AvailableStarterInstructions()
		if currentModel.reportError(failure) {
			return currentModel, nil
		}
		for position, instruction := range starters.instructions {
			if starters.selected[position] {
				for _, candidate := range available {
					if candidate.Identifier == instruction.Identifier {
						instructions = append(instructions, instruction)
					}
				}
			}
		}
		if currentModel.reportError(currentModel.repository.SaveInstructions(store.FormatInstructions(instructions), store.ContextVersion(text))) {
			return currentModel, nil
		}
		currentModel.contextPane.starters = nil
		currentModel.contextPaneSaved()
	}
	return currentModel, nil
}

func (currentModel model) instructionStarterLines(width int, height int) []string {
	theme := currentModel.theme
	starters := currentModel.contextPane.starters
	innerWidth := width - 2
	lines := []string{theme.accentBold.Render("Choose starter instructions"), "", theme.dim.Render("Enable the behaviors you want Den to follow."), ""}
	for position, instruction := range starters.instructions {
		marker := "[ ] "
		if starters.selected[position] {
			marker = "[x] "
		}
		line := marker + instruction.Title
		if position == starters.cursor {
			line = theme.accentBold.Render("▸ " + line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	instruction := starters.instructions[starters.cursor]
	details := []string{"", theme.bold.Render("Instruction")}
	details = append(details, wrapText(instruction.Text, innerWidth)...)
	details = append(details, "", theme.bold.Render("Illustrative examples"))
	for _, example := range instruction.Examples {
		details = append(details, wrapText("• "+example, innerWidth)...)
	}
	hints := currentModel.hintLines([]key.Binding{hint("↑↓", "move"), hint("space", "choose"), hint("↵", "enable selected"), hint("esc", "skip"), hint("⇧↑↓", "details")}, innerWidth, 3)
	detailHeight := max(1, height-len(lines)-len(hints))
	offset := min(currentModel.contextPane.offset, max(0, len(details)-detailHeight))
	lines = append(lines, details[offset:min(len(details), offset+detailHeight)]...)
	lines = fitLines(strings.Join(lines, "\n"), innerWidth, max(1, height-len(hints)))
	return indentedLines(append(lines, hints...), width, height)
}
