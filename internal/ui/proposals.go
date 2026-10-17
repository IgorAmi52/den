package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

// proposalFor finds what den proposes for a row: a new folder from a session, or a change to a folder's context,
// or on the repo row, to the repo's.
func (currentModel model) proposalFor(row listRow) *store.Suggestion {
	repository := &currentModel.repository
	switch row.kind {
	case sessionRow:
		if suggestion := repository.PendingFor(row.session.SessionId); suggestion != nil && suggestion.Kind == store.SuggestFolder {
			return suggestion
		}
	case folderRow:
		return repository.PendingForFolder(row.folder.Id)
	case contextFileRow:
		if row.laptopTool == "" {
			return repository.PendingForFolder("")
		}
	case repositoryRow:
		return repository.PendingForFolder("")
	}
	return nil
}

// ghostText is the short form of a session's proposal, in faint text on its row: the folder den would make for it.
func (currentModel model) ghostText(sessionId string) string {
	if suggestion := currentModel.repository.PendingFor(sessionId); suggestion != nil && suggestion.Kind == store.SuggestFolder {
		return "→ " + suggestion.FolderName
	}
	return ""
}

// suggestionTag names a suggestion's kind in one word, the first thing its card shows.
func suggestionTag(suggestion store.Suggestion) string {
	switch {
	case suggestion.Kind == store.SuggestLink:
		return "LINK"
	case suggestion.Kind == store.SuggestFolder && suggestion.Skill:
		return "SKILL"
	case suggestion.Kind == store.SuggestFolder:
		return "FOLDER"
	}
	return "NOTES"
}

// proposalTitle says in plain words what saying yes does.
func (currentModel model) proposalTitle(suggestion store.Suggestion) string {
	switch suggestion.Kind {
	case store.SuggestLink:
		return "use " + filepath.Base(suggestion.LinkedPath) + " as " + currentModel.contextHeading(suggestion.FolderId) + "'s context"
	case store.SuggestFolder:
		if suggestion.Skill {
			return "make a skill folder " + suggestion.FolderName
		}
		return "make a folder " + suggestion.FolderName
	}
	added := addedLines(currentModel.currentContextText(suggestion), suggestion.ProposedText)
	if strings.TrimSpace(currentModel.currentContextText(suggestion)) == "" {
		return "start " + currentModel.contextHeading(suggestion.FolderId) + "'s notes"
	}
	return fmt.Sprintf("add %d %s to %s's notes", len(added), plural(len(added), "line"), currentModel.contextHeading(suggestion.FolderId))
}

// suggestionSummary is a suggestion's kind and one line about it, for the chat.
func (currentModel model) suggestionSummary(suggestion store.Suggestion) (label string, summary string) {
	return suggestionTag(suggestion), currentModel.proposalTitle(suggestion)
}

// addedLines are the lines of text a context change adds, leaving out empty lines and headings.
func addedLines(before string, after string) []string {
	var added []string
	for _, line := range lineDiff(before, after) {
		if line.kind == addedLine && strings.TrimSpace(line.text) != "" && !strings.HasPrefix(strings.TrimSpace(line.text), "#") {
			added = append(added, strings.TrimSpace(line.text))
		}
	}
	return added
}

// contextPreview is the start of a proposed context without its headings, at most a few lines, for a card.
func contextPreview(text string, maximumLines int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(lines) == maximumLines {
			return append(lines, "…")
		}
		lines = append(lines, line)
	}
	return lines
}

func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}
