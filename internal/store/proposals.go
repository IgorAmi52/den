package store

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

func ContextVersion(text string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
}

func (repository *Repository) ContextDestination(folderIdentifier string) string {
	if folderIdentifier == "" {
		return "Repo context"
	}
	var names []string
	for _, folder := range repository.FolderPath(folderIdentifier) {
		names = append(names, folder.Name)
	}
	return strings.Join(names, " → ")
}

func (repository *Repository) InheritedContext(folderIdentifier string) string {
	text := repository.ReadContext("")
	for _, folder := range repository.FolderPath(folderIdentifier) {
		text += "\n" + repository.ReadContext(folder.Id)
	}
	return text
}

func (repository *Repository) VisibleSuggestions() []Suggestion {
	var visible []Suggestion
	for _, suggestion := range repository.Suggestions {
		if !suggestion.Deferred {
			visible = append(visible, suggestion)
		}
	}
	return visible
}

func (repository *Repository) CanShow(candidate Suggestion) bool {
	visible := repository.VisibleSuggestions()
	count := 0
	for _, waiting := range visible {
		if waiting.Id != candidate.Id {
			count++
		}
	}
	if count >= maximumPendingSuggestions {
		return false
	}
	for _, waiting := range visible {
		if waiting.Id != candidate.Id && (candidate.Kind == SuggestContext || candidate.Kind == SuggestLink) &&
			(waiting.Kind == SuggestContext || waiting.Kind == SuggestLink) && waiting.FolderId == candidate.FolderId {
			return false
		}
	}
	return true
}

func (repository *Repository) PrepareSuggestion(suggestion *Suggestion) error {
	if suggestion.FolderId != "" && repository.FindFolder(suggestion.FolderId) == nil {
		return fmt.Errorf("destination folder no longer exists")
	}
	if repository.IsRejected(*suggestion) {
		return fmt.Errorf("this information was rejected")
	}
	switch suggestion.Kind {
	case SuggestContext:
		current := repository.ReadContext(suggestion.FolderId)
		if len(suggestion.AddedLines) == 0 {
			if strings.TrimSpace(suggestion.ProposedText) == "" || strings.TrimSpace(suggestion.ProposedText) == strings.TrimSpace(current) {
				return fmt.Errorf("there is no context change to propose")
			}
			if suggestion.ContextVersion != "" && suggestion.ContextVersion != ContextVersion(current) {
				return fmt.Errorf("context changed; revise this proposal against the current notes")
			}
		} else {
			text := current
			if strings.TrimSpace(text) == "" {
				text = "# " + repository.ContextDestination(suggestion.FolderId) + "\n"
			}
			added := 0
			for _, line := range suggestion.AddedLines {
				if !HasRule(repository.InheritedContext(suggestion.FolderId), line) && !HasRule(text, line) {
					text = AddToSection(text, "Findings", line)
					added++
				}
			}
			if added == 0 {
				return fmt.Errorf("the destination or its parents already contain this information")
			}
			suggestion.ProposedText = text
		}
		suggestion.ContextVersion = ContextVersion(current)
	case SuggestFolder:
		if strings.TrimSpace(suggestion.FolderName) == "" {
			return fmt.Errorf("a folder needs a name")
		}
		if suggestion.ParentFolderId != "" && repository.FindFolder(suggestion.ParentFolderId) == nil {
			return fmt.Errorf("parent folder no longer exists")
		}
		for _, folder := range repository.Folders {
			if folder.ParentId == suggestion.ParentFolderId && strings.EqualFold(folder.Name, suggestion.FolderName) {
				return fmt.Errorf("this folder already exists")
			}
		}
	case SuggestRule:
		missing := false
		for _, filePath := range suggestion.FilePaths {
			missing = missing || !HasRule(ReadText(filePath), suggestion.Rule)
		}
		if !missing {
			return fmt.Errorf("the rule is already recorded")
		}
	case SuggestLink:
		if suggestion.FolderId == "" || suggestion.LinkedPath == "" {
			return fmt.Errorf("a linked file needs an existing folder and a path")
		}
	default:
		return fmt.Errorf("unknown proposal kind")
	}
	if suggestion.LinkedPath != "" {
		if _, err := os.Stat(suggestion.LinkedPath); err != nil {
			return fmt.Errorf("linked file is unavailable: %w", err)
		}
	}
	return nil
}
