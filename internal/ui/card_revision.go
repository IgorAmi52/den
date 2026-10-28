package ui

import (
	"slices"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

func (currentModel *model) moveCard(suggestion *store.Suggestion, destination string) {
	if (suggestion.Kind == store.SuggestFolder && suggestion.ParentFolderId == destination) ||
		((suggestion.Kind == store.SuggestContext || suggestion.Kind == store.SuggestLink) && suggestion.FolderId == destination) {
		currentModel.afterDockAnswer()
		return
	}
	candidate := *suggestion
	candidate.Conversation = slices.Clone(suggestion.Conversation)
	candidate.AddedLines = slices.Clone(suggestion.AddedLines)
	switch candidate.Kind {
	case store.SuggestContext:
		if len(candidate.AddedLines) == 0 {
			for _, line := range addedLines(currentModel.repository.ReadContext(candidate.FolderId), candidate.ProposedText) {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
				if line != "" && !strings.HasPrefix(line, "#") {
					candidate.AddedLines = append(candidate.AddedLines, line)
				}
			}
			if len(candidate.AddedLines) == 0 {
				currentModel.setError("Ask DEN to revise this rewrite for the new destination.")
				return
			}
		}
		candidate.FolderId, candidate.ContextVersion = destination, ""
	case store.SuggestRule:
		candidate.Kind, candidate.KeepKind, candidate.FolderId = store.SuggestContext, "notes", destination
		candidate.AddedLines, candidate.Rule, candidate.FilePaths = []string{candidate.Rule}, "", nil
		candidate.FutureUse = "Future sessions in " + currentModel.repository.ContextDestination(destination) + " follow this preference."
	case store.SuggestFolder:
		candidate.ParentFolderId = destination
	case store.SuggestLink:
		if destination == "" {
			currentModel.setError("A linked file needs a folder; ask DEN to propose repo notes instead.")
			return
		}
		candidate.FolderId = destination
	}
	if currentModel.reportError(currentModel.repository.PrepareSuggestion(&candidate)) {
		return
	}
	candidate.Deferred = !currentModel.repository.CanShow(candidate)
	candidate.RevalidateAfter = time.Time{}
	candidate.Revision++
	candidate.Conversation = append(candidate.Conversation, store.CardMessage{Text: "Move this proposal to " + currentModel.repository.ContextDestination(destination)}, store.CardMessage{FromDen: true, Text: "Destination updated. The source session stays where it is; nothing has been written."})
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "dock", Action: "move proposal", Outcome: "revised", RejectionReason: "Wrong destination", Explanation: currentModel.repository.ContextDestination(destination), CallId: suggestion.CallId, Suggestion: suggestion}))
	currentModel.repository.CardFeedback = append(currentModel.repository.CardFeedback, store.CardFeedback{At: time.Now(), Suggestion: *suggestion, Reason: "Wrong destination", Explanation: "User moved the proposal to " + currentModel.repository.ContextDestination(destination)})
	*suggestion = candidate
	currentModel.repository.RefreshCardRecord(candidate)
	currentModel.saveRepository()
	currentModel.afterDockAnswer()
}

func (currentModel *model) reviseCard(suggestion *store.Suggestion, action brain.ChatAction) bool {
	candidate := *suggestion
	candidate.ReviewConcern = ""
	candidate.AddedLines, candidate.ContextVersion = nil, ""
	candidate.FolderId, candidate.ParentFolderId, candidate.FolderName = "", "", ""
	candidate.ProposedText, candidate.LinkedPath, candidate.Rule = "", "", ""
	candidate.FilePaths, candidate.Skill = nil, false
	switch action.Kind {
	case "editNotes":
		candidate.Kind, candidate.KeepKind, candidate.FolderId, candidate.ProposedText = store.SuggestContext, "notes", action.FolderId, action.Text
	case "addRule":
		if action.Target == "laptop" {
			candidate.Kind, candidate.KeepKind, candidate.Rule = store.SuggestRule, "rule", action.Rule
			for path := range currentModel.laptopTexts() {
				candidate.FilePaths = append(candidate.FilePaths, path)
			}
			slices.Sort(candidate.FilePaths)
		} else {
			candidate.Kind, candidate.KeepKind, candidate.FolderId = store.SuggestContext, "notes", action.FolderId
			candidate.AddedLines, candidate.Rule, candidate.FilePaths = []string{action.Rule}, "", nil
			candidate.FutureUse = "Future sessions in " + currentModel.repository.ContextDestination(action.FolderId) + " follow this preference."
		}
	case "link":
		candidate.Kind, candidate.KeepKind, candidate.FolderId, candidate.LinkedPath = store.SuggestLink, "", action.FolderId, action.LinkedPath
	case "createFolder":
		candidate.Kind, candidate.KeepKind, candidate.FolderName, candidate.ParentFolderId, candidate.ProposedText = store.SuggestFolder, "folder", action.Name, action.FolderId, action.Text
		if suggestion.Skill {
			candidate.Skill, candidate.KeepKind = true, "skill"
		}
	default:
		currentModel.setError("Ask for a revision of this card's text or destination.")
		return false
	}
	if currentModel.reportError(currentModel.repository.PrepareSuggestion(&candidate)) {
		return false
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "revise", Action: "proposal revision", Outcome: "superseded", CallId: action.CallId, Suggestion: suggestion}))
	candidate.Revision++
	candidate.CallId = action.CallId
	candidate.Deferred = !currentModel.repository.CanShow(candidate)
	candidate.RevalidateAfter = time.Time{}
	*suggestion = candidate
	currentModel.repository.RefreshCardRecord(candidate)
	return true
}
