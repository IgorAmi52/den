package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/store"
)

// suggestionBody describes a suggestion in full, for the wide preview.
func (currentModel model) suggestionBody(suggestion store.Suggestion, width int) []string {
	theme := currentModel.theme
	var lines []string
	if suggestion.Reason != "" {
		lines = append(lines, wrapAll([]string{theme.text.Render(suggestion.Reason)}, width)...)
	}
	if suggestion.Source != "" {
		lines = append(lines, theme.dim.Render("from \""+truncate(suggestion.Source, width-8)+"\""))
	}
	lines = append(lines, "")
	switch suggestion.Kind {
	case store.SuggestContext:
		lines = append(lines, currentModel.diffLines(currentModel.currentContextText(suggestion), suggestion.ProposedText, width)...)
	case store.SuggestLink:
		text := store.ReadText(suggestion.LinkedPath)
		lines = append(lines, currentModel.layoutRow([]segment{{text: filepath.Base(suggestion.LinkedPath), style: theme.bold}},
			[]segment{{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(text)), style: theme.dim}}, width, false))
		lines = append(lines, currentModel.fileLink(suggestion.LinkedPath, width), "")
		lines = append(lines, currentModel.renderedMarkdown(text, width)...)
	case store.SuggestFolder:
		where := "at the top level"
		if suggestion.ParentFolderId != "" {
			where = "in " + currentModel.repository.FolderName(suggestion.ParentFolderId)
		}
		lines = append(lines, theme.folder.Render("▸ "+suggestion.FolderName)+theme.dim.Render(" · new, "+where))
		if suggestion.LinkedPath != "" {
			lines = append(lines, theme.dim.Render("context: ")+currentModel.fileLink(suggestion.LinkedPath, width-9))
			lines = append(lines, currentModel.renderedMarkdown(store.ReadText(suggestion.LinkedPath), width)...)
		} else {
			lines = append(lines, currentModel.renderedMarkdown(suggestion.ProposedText, width)...)
		}
		if suggestion.Skill {
			lines = append(lines, "", theme.dim.Render("Every session you start in it begins with these instructions."))
		} else {
			lines = append(lines, "", theme.dim.Render("The session moves into it, and every new session there begins with this context."))
		}
	case store.SuggestRule:
		lines = append(lines, currentModel.diffLines("", "- "+suggestion.Rule, width)...)
		lines = append(lines, "", theme.dim.Render("Goes to "+laptopFileNames(suggestion.FilePaths)+", which agents load in every repo. Tell den to keep it in a folder or the repo instead."))
	}
	return lines
}

// diffLines shows a context change as a unified diff, green and red on faint backgrounds.
func (currentModel model) diffLines(before string, after string, width int) []string {
	theme := currentModel.theme
	var lines []string
	for _, line := range diffHunks(lineDiff(before, after), 2) {
		style, marker := theme.diffContext, "  "
		switch line.kind {
		case addedLine:
			style, marker = theme.diffAdded, "+ "
		case removedLine:
			style, marker = theme.diffRemoved, "- "
		case skippedLines:
			lines = append(lines, theme.faint.Render("  …"))
			continue
		}
		for pieceIndex, piece := range wrapText(line.text, width-2) {
			if pieceIndex > 0 {
				marker = "  "
			}
			if line.kind == unchangedLine {
				lines = append(lines, style.Render(marker+piece))
			} else {
				lines = append(lines, style.Render(padRight(marker+piece, width)))
			}
		}
	}
	return lines
}

// screenTopBar is the first line of a full-window screen: where you are, the agents, and the way back.
func (currentModel model) screenTopBar(name string, width int) string {
	theme := currentModel.theme
	left := []segment{{text: " den", style: theme.accentBold}, {text: " · " + currentModel.repositoryName() + " · " + name, style: theme.dim}}
	var right []segment
	for _, open := range currentModel.openAgents {
		right = append(right, segment{text: fmt.Sprint(open.number), style: theme.accentBold}, currentModel.statusGlyph(open.status), segment{text: "  ", style: theme.text})
	}
	right = append(right, segment{text: "esc", style: theme.key}, segment{text: " back ", style: theme.action})
	return currentModel.layoutRow(left, right, width, false)
}

func centered(block string, width int) []string {
	return strings.Split(lipgloss.PlaceHorizontal(width, lipgloss.Center, block), "\n")
}
