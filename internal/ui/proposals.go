package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"

	"github.com/IgorAmi52/den/internal/store"
)

// proposalFor finds what den proposes for a row: filing or archiving a session, or an edit of a folder's context,
// the repo's context or a guide.
func (currentModel model) proposalFor(row listRow) *store.Suggestion {
	switch row.kind {
	case sessionRow:
		return currentModel.repository.PendingFor(row.session.SessionId)
	case folderRow:
		return currentModel.repository.PendingContextFor(row.folder.Id, "")
	case contextFileRow:
		return currentModel.repository.PendingContextFor("", row.guide)
	case repositoryRow:
		if suggestion := currentModel.repository.PendingContextFor("", ""); suggestion != nil {
			return suggestion
		}
		for _, guide := range store.Guides {
			if suggestion := currentModel.repository.PendingContextFor("", guide); suggestion != nil {
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
		return "→ ⎇ " + suggestion.FolderName
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
		return "group in a new ⎇ subfolder"
	case store.SuggestCleanup:
		return "archive it"
	}
	added, removed := diffCounts(lineDiff(currentModel.currentContextText(suggestion), suggestion.ProposedText))
	return fmt.Sprintf("update %s  +%d −%d", currentModel.contextFileName(suggestion), added, removed)
}

// proposalReason says why den proposes it, for the session on this row when the proposal covers several.
func (currentModel model) proposalReason(suggestion store.Suggestion, row listRow) string {
	switch suggestion.Kind {
	case store.SuggestCleanup:
		if row.kind == sessionRow {
			if currentModel.mergedBranches[row.session.Branch] {
				return "Its branch merged. Archived sessions stay resumable."
			}
			return fmt.Sprintf("Untouched for %d days. Archived sessions stay resumable.", int(time.Since(row.session.LastActivityAt).Hours()/24))
		}
	case store.SuggestContext:
		reason := suggestion.Reason
		if suggestion.Source != "" {
			reason = "Learned in " + suggestion.Source + ". " + reason
		}
		if !currentModel.isWide() {
			reason += " z shows the change."
		}
		return reason
	}
	return suggestion.Reason
}

// proposalLines explain the selected row's proposal right under it: what it is, why, and the keys.
func (currentModel model) proposalLines(row listRow, width int) []string {
	suggestion := currentModel.proposalFor(row)
	if suggestion == nil {
		return nil
	}
	theme := currentModel.theme
	indent := " " + strings.Repeat("  ", row.depth) + "  "
	textWidth := width - len(indent) - 1
	var lines []string
	for _, line := range wrapText("✦ "+currentModel.proposalTitle(*suggestion), textWidth) {
		lines = append(lines, indent+theme.suggestionBold.Render(line))
	}
	for _, line := range wrapText(currentModel.proposalReason(*suggestion, row), textWidth) {
		lines = append(lines, indent+theme.dim.Render(line))
	}
	hints := []key.Binding{currentModel.keys.accept, currentModel.keys.reject}
	return append(lines, indent+strings.Join(currentModel.hintLines(hints, textWidth, 1), ""))
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
		return "Subfolder", fmt.Sprintf("⎇ %s in %s (%d)", suggestion.FolderName, currentModel.repository.FolderName(suggestion.ParentFolderId), len(suggestion.SessionIds))
	case store.SuggestCleanup:
		return "Cleanup", countedSessions(len(suggestion.SessionIds)) + " look finished"
	}
	added, removed := diffCounts(lineDiff(currentModel.currentContextText(suggestion), suggestion.ProposedText))
	return "Context", fmt.Sprintf("%s  +%d −%d", currentModel.contextFileName(suggestion), added, removed)
}

// acceptProposal says yes to the row's proposal; for a cleanup, only this row's session is archived.
func (currentModel *model) acceptProposal(row listRow) {
	suggestion := currentModel.proposalFor(row)
	if suggestion == nil {
		currentModel.setStatus("nothing proposed here")
		return
	}
	if suggestion.Kind == store.SuggestCleanup && row.kind == sessionRow {
		if currentModel.reportError(currentModel.repository.AcceptFor(suggestion.Id, row.session.SessionId)) {
			return
		}
		currentModel.saveRepository()
		currentModel.setStatus("archived, still resumable")
		return
	}
	currentModel.acceptSuggestion(suggestion.Id)
}

// rejectProposal says no to the row's proposal, so den won't offer it again.
func (currentModel *model) rejectProposal(row listRow) {
	suggestion := currentModel.proposalFor(row)
	if suggestion == nil {
		currentModel.setStatus("nothing proposed here")
		return
	}
	if suggestion.Kind == store.SuggestCleanup && row.kind == sessionRow {
		currentModel.repository.RejectFor(suggestion.Id, row.session.SessionId)
		currentModel.saveRepository()
		currentModel.setStatus("kept · den won't offer that again")
		return
	}
	currentModel.rejectSuggestion(suggestion.Id)
}
