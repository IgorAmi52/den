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
	sessionTitle := func(sessionId string) string {
		if session := currentModel.repository.FindSession(sessionId); session != nil {
			return session.Title
		}
		return sessionId
	}
	switch suggestion.Kind {
	case store.SuggestMove:
		if len(suggestion.SessionIds) == 1 {
			lines = append(lines, theme.bold.Render(sessionTitle(suggestion.SessionIds[0])))
		} else {
			for _, sessionId := range suggestion.SessionIds {
				lines = append(lines, theme.text.Render("· "+truncate(sessionTitle(sessionId), width-2)))
			}
		}
		from := "Inbox"
		if session := currentModel.repository.FindSession(suggestion.SessionIds[0]); session != nil {
			from = currentModel.repository.FolderName(session.FolderId)
		}
		lines = append(lines, theme.dim.Render(from)+theme.dim.Render("  →  ")+theme.accentBold.Render(currentModel.repository.FolderName(suggestion.FolderId)))
	case store.SuggestSubfolder:
		lines = append(lines, theme.text.Render("New ")+theme.accentBold.Render(subfolderLabel(suggestion))+theme.text.Render(" in "+currentModel.repository.FolderName(suggestion.ParentFolderId)))
		lines = append(lines, theme.dim.Render("moves "+countedSessions(len(suggestion.SessionIds))+":"))
		for _, sessionId := range suggestion.SessionIds {
			lines = append(lines, theme.text.Render("  · "+truncate(sessionTitle(sessionId), width-4)))
		}
	case store.SuggestCleanup:
		lines = append(lines, theme.bold.Render(countedSessions(len(suggestion.SessionIds))+" look finished"))
		for _, sessionId := range suggestion.SessionIds {
			session := currentModel.repository.FindSession(sessionId)
			if session == nil {
				continue
			}
			right := []segment{{text: relativeAge(session.LastActivityAt), style: theme.dim}}
			if currentModel.mergedBranches[session.Branch] {
				right = append(right, segment{text: " merged", style: theme.running})
			}
			lines = append(lines, currentModel.layoutRow([]segment{{text: "· " + session.Title, style: theme.text}}, right, width, false))
		}
	case store.SuggestContext:
		if suggestion.Source != "" && suggestion.Starter == "" {
			lines = append(lines, theme.dim.Render("learned in "+suggestion.Source))
		}
		lines = append(lines, currentModel.diffLines(currentModel.currentContextText(suggestion), suggestion.ProposedText, width)...)
	case store.SuggestLink:
		text := store.ReadText(suggestion.LinkedPath)
		lines = append(lines, currentModel.layoutRow([]segment{{text: filepath.Base(suggestion.LinkedPath), style: theme.bold}},
			[]segment{{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(text)), style: theme.dim}}, width, false))
		lines = append(lines, currentModel.fileLink(suggestion.LinkedPath, width), "")
		lines = append(lines, currentModel.draftPreview(text, width)...)
	case store.SuggestLaptopRule:
		lines = append(lines, currentModel.diffLines("", "- "+suggestion.Rule, width)...)
		lines = append(lines, "", theme.dim.Render("goes to "+laptopFileNames(suggestion.FilePaths)+", the files with no such rule yet"))
	case store.SuggestQuestion:
		if suggestion.Rule != "" {
			lines = append(lines, "", theme.dim.Render("A yes adds: ")+theme.text.Render(suggestion.Rule))
		}
	}
	if suggestion.Reason != "" {
		lines = append(lines, "")
		lines = append(lines, wrapAll([]string{theme.dim.Render(suggestion.Reason)}, width)...)
	}
	if suggestion.Kind == store.SuggestCleanup {
		lines = append(lines, theme.faint.Render(fmt.Sprintf("Archived sessions stay resumable for %d days.", archiveKeepDays())))
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
