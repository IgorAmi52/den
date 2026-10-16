package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// segment is a piece of a row with its own style. Rows are built from segments
// so the selection bar can give every piece its background.
type segment struct {
	text  string
	style lipgloss.Style
}

// layoutRow fits the left segments and the right-aligned ones into width. When space runs out,
// the left text is cut with … by display width. A selected row gets the full-width selection bar.
func (currentModel model) layoutRow(left []segment, right []segment, width int, selected bool) string {
	rightWidth := 0
	for _, part := range right {
		rightWidth += ansi.StringWidth(part.text)
	}
	available := width - rightWidth
	if rightWidth > 0 {
		available--
	}
	var fitted []segment
	usedWidth := 0
	for _, part := range left {
		partWidth := ansi.StringWidth(part.text)
		if usedWidth+partWidth > available {
			part.text = ansi.Truncate(part.text, max(available-usedWidth, 0), "…")
			fitted = append(fitted, part)
			usedWidth += ansi.StringWidth(part.text)
			break
		}
		fitted = append(fitted, part)
		usedWidth += partWidth
	}
	fitted = append(fitted, segment{text: strings.Repeat(" ", max(width-usedWidth-rightWidth, 0)), style: currentModel.theme.text})
	fitted = append(fitted, right...)
	var line strings.Builder
	background := currentModel.theme.selectedRow.GetBackground()
	for _, part := range fitted {
		style := part.style
		if selected {
			style = style.Background(background)
		}
		line.WriteString(style.Render(part.text))
	}
	return line.String()
}

// fitLines pads or cuts text to exactly height lines, each cut to width.
func fitLines(text string, width int, height int) []string {
	lines := strings.Split(text, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for index, line := range lines {
		lines[index] = padRight(ansi.Truncate(line, width, "…"), width)
	}
	return lines
}

// truncateStyled cuts styled text to width by display width, keeping its styling.
func truncateStyled(text string, width int) string {
	return ansi.Truncate(text, width, "…")
}

func padRight(text string, width int) string {
	return text + strings.Repeat(" ", max(width-ansi.StringWidth(text), 0))
}

// truncate cuts plain text to a display width, ending with … when it was cut.
func truncate(text string, width int) string {
	return ansi.Truncate(text, max(width, 0), "…")
}

// wrapText breaks plain text into lines of at most width cells, keeping words whole when they fit.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	return strings.Split(ansi.Wrap(text, width, ""), "\n")
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
