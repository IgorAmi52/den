package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/brain"
)

const (
	planDone    = "done"
	planSkipped = "skipped"
	// narrowPlanWidth is below which a plan card gives up some padding, so it still fits the narrow panel.
	narrowPlanWidth = 40
)

// planCard draws the changes den will make once the user confirms, as a card of its own, so a plan never reads
// like part of den's reply. Each change is a line saying what it does, with the text it would write in full under it.
// outcome is empty while the plan waits, then planDone or planSkipped; confirmKeys are the keys a waiting plan shows.
func (currentModel model) planCard(actions []brain.ChatAction, outcome string, confirmKeys []key.Binding, width int) []string {
	theme := currentModel.theme
	padding := 2
	if width < narrowPlanWidth {
		padding = 1
	}
	innerWidth := max(width-2-2*padding, 10)
	header := []segment{{text: " PLAN ", style: theme.badge}, {text: fmt.Sprintf("  %d %s", len(actions), plural(len(actions), "change")), style: theme.dim}}
	borderColor := theme.suggestion.GetForeground()
	switch outcome {
	case planDone:
		header = []segment{{text: "✓ DONE", style: theme.running.Bold(true)}}
		borderColor = theme.border.GetForeground()
	case planSkipped:
		header = []segment{{text: "SKIPPED", style: theme.faint}, {text: " · nothing changed", style: theme.faint}}
		borderColor = theme.border.GetForeground()
	}
	lines := []string{currentModel.layoutRow(header, nil, innerWidth, false), ""}
	for _, action := range actions {
		lines = append(lines, hangingIndent(capitalized(action.Description), theme.bold, innerWidth, "▸ ", "  ")...)
		shownText := action.Text
		if action.Change != "" {
			shownText = action.Change
		}
		for _, textLine := range splitLines(strings.TrimSpace(shownText)) {
			style, continuation := theme.text, "    "
			if strings.HasPrefix(textLine, "#") {
				style = theme.bold
			}
			if strings.HasPrefix(strings.TrimSpace(textLine), "- ") {
				continuation = "      "
			}
			lines = append(lines, hangingIndent(textLine, style, innerWidth, "    ", continuation)...)
		}
	}
	if outcome == "" && len(confirmKeys) > 0 {
		lines = append(lines, "")
		lines = append(lines, currentModel.hintLines(confirmKeys, innerWidth, 2)...)
	}
	for index, line := range lines {
		lines[index] = padRight(line, innerWidth)
	}
	card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(borderColor).Padding(0, padding)
	return strings.Split(card.Render(strings.Join(lines, "\n")), "\n")
}

func capitalized(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
