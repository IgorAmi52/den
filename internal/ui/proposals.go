package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

// proposalFor finds what den proposes for a row: filing or archiving a session, a folder's first context
// or an edit of it, a rule for the laptop files, or on the repo row, a question.
func (currentModel model) proposalFor(row listRow) *store.Suggestion {
	repository := &currentModel.repository
	switch row.kind {
	case sessionRow:
		return repository.PendingFor(row.session.SessionId)
	case folderRow:
		return repository.PendingForFolder(row.folder.Id)
	case contextFileRow:
		if row.laptopTool == "" {
			return repository.PendingForFolder("")
		}
		laptopFile, _ := currentModel.laptopFile(row.laptopTool)
		if suggestion := repository.PendingOfKind(store.SuggestLaptopRule); suggestion != nil && slices.Contains(suggestion.FilePaths, laptopFile.Path) {
			return suggestion
		}
	case repositoryRow:
		for _, suggestion := range []*store.Suggestion{repository.PendingOfKind(store.SuggestQuestion), repository.PendingForFolder(""), repository.PendingOfKind(store.SuggestLaptopRule)} {
			if suggestion != nil {
				return suggestion
			}
		}
	}
	return nil
}

// ghostText is the short form of a session's proposal, in faint text on its row: where it would go.
func (currentModel model) ghostText(sessionId string) string {
	suggestion := currentModel.repository.PendingFor(sessionId)
	if suggestion == nil {
		return ""
	}
	switch suggestion.Kind {
	case store.SuggestSubfolder:
		return "→ " + subfolderLabel(*suggestion)
	case store.SuggestCleanup:
		return "→ archive"
	}
	return "→ " + currentModel.repository.FolderName(suggestion.FolderId)
}

// proposalTitle says in a few words what saying yes does.
func (currentModel model) proposalTitle(suggestion store.Suggestion) string {
	switch suggestion.Kind {
	case store.SuggestMove:
		return "file into " + currentModel.repository.FolderName(suggestion.FolderId)
	case store.SuggestSubfolder:
		switch {
		case suggestion.LinkedPath != "":
			return "group in " + suggestion.FolderName + ", with " + filepath.Base(suggestion.LinkedPath) + " as its context"
		case suggestion.Branch == "":
			return "group in a new subfolder for " + suggestion.FolderName
		}
		return "group in a new ⎇ subfolder"
	case store.SuggestCleanup:
		return "archive it"
	case store.SuggestLink:
		return "link " + filepath.Base(suggestion.LinkedPath) + " as its context"
	case store.SuggestLaptopRule:
		return "add a rule to " + laptopFileNames(suggestion.FilePaths)
	case store.SuggestQuestion:
		return suggestion.Question
	}
	switch suggestion.Starter {
	case store.StarterPlan:
		return "save the plan as its notes"
	case store.StarterDraft:
		return "start notes for " + currentModel.contextHeading(suggestion.FolderId)
	}
	added, removed := diffCounts(lineDiff(currentModel.currentContextText(suggestion), suggestion.ProposedText))
	return fmt.Sprintf("update %s  +%d −%d", currentModel.contextFileName(suggestion), added, removed)
}

// subfolderLabel names a proposed subfolder, with ⎇ when it groups a branch.
func subfolderLabel(suggestion store.Suggestion) string {
	if suggestion.Branch != "" {
		return "⎇ " + suggestion.FolderName
	}
	return suggestion.FolderName
}

// laptopFileNames names the laptop files a rule goes to, like "CLAUDE.md and AGENTS.md".
func laptopFileNames(filePaths []string) string {
	var names []string
	for _, filePath := range filePaths {
		names = append(names, filepath.Base(filePath))
	}
	return strings.Join(names, " and ")
}

// proposalReason says why den proposes it, for the session on this row when the proposal covers several.
func (currentModel model) proposalReason(suggestion store.Suggestion, row listRow) string {
	switch suggestion.Kind {
	case store.SuggestCleanup:
		if row.kind == sessionRow {
			if currentModel.mergedBranches[row.session.Branch] {
				return "Its branch merged."
			}
			if brain.OnlyAskedQuestions(currentModel.digests, row.session.SessionId) {
				return "It only asked questions."
			}
			return fmt.Sprintf("Untouched for %d days.", int(time.Since(row.session.LastActivityAt).Hours()/24))
		}
	case store.SuggestContext:
		reason := suggestion.Reason
		if suggestion.Source != "" && suggestion.Starter == "" && !strings.HasPrefix(reason, "You said") {
			reason = "Learned in " + suggestion.Source + ". " + reason
		}
		if !currentModel.isWide() {
			reason += " z shows it."
		}
		return reason
	case store.SuggestLink:
		if !currentModel.isWide() {
			return suggestion.Reason + " z shows it."
		}
	case store.SuggestLaptopRule:
		return "\"" + suggestion.Rule + "\" " + suggestion.Reason
	}
	return suggestion.Reason
}

// suggestionSummary is a suggestion's kind and one line about it, for lists and the chat.
func (currentModel model) suggestionSummary(suggestion store.Suggestion) (label string, summary string) {
	switch suggestion.Kind {
	case store.SuggestMove:
		title := countedSessions(len(suggestion.SessionIds))
		if len(suggestion.SessionIds) == 1 {
			if session := currentModel.repository.FindSession(suggestion.SessionIds[0]); session != nil {
				title = session.Title
			}
		}
		return "Move", truncate(title, 28) + " → " + currentModel.repository.FolderName(suggestion.FolderId)
	case store.SuggestSubfolder:
		return "Subfolder", fmt.Sprintf("%s in %s (%d)", subfolderLabel(suggestion), currentModel.repository.FolderName(suggestion.ParentFolderId), len(suggestion.SessionIds))
	case store.SuggestCleanup:
		return "Cleanup", countedSessions(len(suggestion.SessionIds)) + " look finished"
	case store.SuggestLink:
		return "Link", filepath.Base(suggestion.LinkedPath) + " → ◆ " + currentModel.contextHeading(suggestion.FolderId)
	case store.SuggestLaptopRule:
		return "Laptop rule", truncate(suggestion.Rule, 40)
	case store.SuggestQuestion:
		return "Question", suggestion.Question
	}
	if suggestion.Starter != "" {
		return "Notes", currentModel.proposalTitle(suggestion)
	}
	added, removed := diffCounts(lineDiff(currentModel.currentContextText(suggestion), suggestion.ProposedText))
	return "Context", fmt.Sprintf("%s  +%d −%d", currentModel.contextFileName(suggestion), added, removed)
}
