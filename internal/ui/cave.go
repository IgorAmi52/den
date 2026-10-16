package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const caveInteriorWidth = 24

var (
	rockStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	mascots   = map[string][]string{
		"claude": {"▐▛█▜▌", "▝▘ ▝▘"},
		"codex":  {"╭>_╮", "╰┬┬╯"},
	}
	mascotStyles = map[string]lipgloss.Style{
		"claude": lipgloss.NewStyle().Foreground(lipgloss.Color("173")),
		"codex":  lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
	}
)

func caveView(agentNames []string) string {
	var mascotColumns []string
	for _, agentName := range agentNames {
		if mascot, known := mascots[agentName]; known {
			mascotColumns = append(mascotColumns, mascotStyles[agentName].Render(strings.Join(mascot, "\n")))
		}
	}
	interior := hintStyle.Render("\npress n")
	if len(mascotColumns) > 0 {
		interior = lipgloss.JoinHorizontal(lipgloss.Bottom, joinWithGaps(mascotColumns)...)
	}
	interiorLines := strings.Split(lipgloss.PlaceHorizontal(caveInteriorWidth, lipgloss.Center, interior), "\n")
	wall := rockStyle.Render("██")
	lines := []string{
		rockStyle.Render("   ▄▄▄████████████████▄▄▄"),
		rockStyle.Render(" ▄█▀                    ▀█▄"),
	}
	for _, interiorLine := range interiorLines {
		lines = append(lines, wall+interiorLine+wall)
	}
	lines = append(lines, rockStyle.Render(strings.Repeat("▀", caveInteriorWidth+4)))
	return strings.Join(lines, "\n")
}

func joinWithGaps(columns []string) []string {
	var spaced []string
	for columnIndex, column := range columns {
		if columnIndex > 0 {
			spaced = append(spaced, " ")
		}
		spaced = append(spaced, column)
	}
	return spaced
}
