package store

import (
	"slices"
	"time"
)

type SuggestionKind string

const (
	// SuggestContext rewrites den's notes of a folder, or the repo's context for the top level.
	SuggestContext SuggestionKind = "context"
	// SuggestLink links a markdown file an agent wrote as a folder's context.
	SuggestLink SuggestionKind = "link"
	// SuggestFolder makes a folder with its context and moves the session it came from into it.
	SuggestFolder SuggestionKind = "folder"
)

// maximumPendingSuggestions keeps den quiet: past it, new suggestions wait until the user answered some.
const maximumPendingSuggestions = 5

type Suggestion struct {
	Id     string         `json:"id"`
	Kind   SuggestionKind `json:"kind"`
	Reason string         `json:"reason"`
	// SessionIds are the sessions a new folder takes in, or the session a suggestion was learned in.
	SessionIds []string `json:"sessionIds,omitempty"`
	// FolderId is the folder whose context an edit or link changes.
	FolderId string `json:"folderId,omitempty"`
	// ParentFolderId and FolderName describe a new folder. Skill marks a folder for a way of working,
	// like research or reviews, rather than for a piece of work.
	ParentFolderId string `json:"parentFolderId,omitempty"`
	FolderName     string `json:"folderName,omitempty"`
	Skill          bool   `json:"skill,omitempty"`
	// ProposedText is the whole new notes of a context edit or a new folder; Source names the session it came from.
	ProposedText string `json:"proposedText,omitempty"`
	Source       string `json:"source,omitempty"`
	// LinkedPath is the file a link proposes, or a new folder's linked context.
	LinkedPath string    `json:"linkedPath,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// RejectionKeys identify what was rejected, so the same suggestion is never offered again.
func (suggestion Suggestion) RejectionKeys() []string {
	switch suggestion.Kind {
	case SuggestLink:
		return []string{"link:" + suggestion.FolderId + ":" + suggestion.LinkedPath}
	case SuggestFolder:
		var keys []string
		for _, sessionId := range suggestion.SessionIds {
			keys = append(keys, "folder:"+sessionId)
		}
		return append(keys, "folder:"+suggestion.FolderName)
	}
	keys := []string{"context:" + suggestion.FolderId + ":" + suggestion.ProposedText}
	for _, sessionId := range suggestion.SessionIds {
		keys = append(keys, "context:"+sessionId)
	}
	return keys
}

func (repository *Repository) IsRejected(suggestion Suggestion) bool {
	for _, key := range suggestion.RejectionKeys() {
		if slices.Contains(repository.RejectedKeys, key) {
			return true
		}
	}
	return false
}

// Offer queues a suggestion unless it was rejected before, the same one is already waiting, its session already has one
// waiting, or enough are waiting already. It reports whether the queue changed.
func (repository *Repository) Offer(suggestion Suggestion) bool {
	if repository.IsRejected(suggestion) || len(repository.Suggestions) >= maximumPendingSuggestions {
		return false
	}
	for _, pending := range repository.Suggestions {
		if slices.Equal(pending.RejectionKeys(), suggestion.RejectionKeys()) {
			return false
		}
		if slices.ContainsFunc(suggestion.SessionIds, func(sessionId string) bool { return slices.Contains(pending.SessionIds, sessionId) }) {
			return false
		}
		if suggestion.Kind != SuggestFolder && pending.Kind != SuggestFolder && pending.FolderId == suggestion.FolderId {
			return false
		}
	}
	suggestion.Id = newId()
	if suggestion.CreatedAt.IsZero() {
		suggestion.CreatedAt = time.Now()
	}
	repository.Suggestions = append(repository.Suggestions, suggestion)
	return true
}

func (repository *Repository) FindSuggestion(suggestionId string) *Suggestion {
	for index := range repository.Suggestions {
		if repository.Suggestions[index].Id == suggestionId {
			return &repository.Suggestions[index]
		}
	}
	return nil
}

func (repository *Repository) Dismiss(suggestionId string) {
	repository.Suggestions = slices.DeleteFunc(repository.Suggestions, func(suggestion Suggestion) bool {
		return suggestion.Id == suggestionId
	})
}

// Reject drops the suggestion and remembers it, so it is never offered again.
func (repository *Repository) Reject(suggestionId string) {
	if suggestion := repository.FindSuggestion(suggestionId); suggestion != nil {
		repository.RejectedKeys = append(repository.RejectedKeys, suggestion.RejectionKeys()...)
	}
	repository.Dismiss(suggestionId)
}

// Accept carries the suggestion out and drops it from the queue. It returns the folder the change is in.
func (repository *Repository) Accept(suggestionId string) (string, error) {
	found := repository.FindSuggestion(suggestionId)
	if found == nil {
		return "", nil
	}
	suggestion := *found
	folderId := suggestion.FolderId
	switch suggestion.Kind {
	case SuggestContext:
		if err := repository.WriteContext(suggestion.FolderId, suggestion.ProposedText); err != nil {
			return "", err
		}
	case SuggestLink:
		if folder := repository.FindFolder(suggestion.FolderId); folder != nil {
			folder.LinkedPath = suggestion.LinkedPath
		}
	case SuggestFolder:
		created := repository.AddFolder(suggestion.FolderName, suggestion.ParentFolderId)
		folderId = created.Id
		repository.FindFolder(folderId).LinkedPath = suggestion.LinkedPath
		if suggestion.ProposedText != "" {
			if err := repository.WriteContext(folderId, suggestion.ProposedText); err != nil {
				return "", err
			}
		}
		for _, sessionId := range suggestion.SessionIds {
			if session := repository.FindSession(sessionId); session != nil {
				session.FolderId, session.Archived, session.ArchivedAt = folderId, false, time.Time{}
			}
		}
	}
	repository.Dismiss(suggestionId)
	return folderId, nil
}

// Unlink takes a folder's linked file away, which counts as rejecting that link, so den doesn't offer it back.
func (repository *Repository) Unlink(folderId string) {
	folder := repository.FindFolder(folderId)
	if folder == nil || folder.LinkedPath == "" {
		return
	}
	repository.RejectedKeys = append(repository.RejectedKeys, Suggestion{Kind: SuggestLink, FolderId: folderId, LinkedPath: folder.LinkedPath}.RejectionKeys()...)
	folder.LinkedPath = ""
}

// PendingFor returns the waiting suggestion that came from this session, if any.
func (repository *Repository) PendingFor(sessionId string) *Suggestion {
	for index := range repository.Suggestions {
		if slices.Contains(repository.Suggestions[index].SessionIds, sessionId) {
			return &repository.Suggestions[index]
		}
	}
	return nil
}

// PendingForFolder returns the waiting edit or link of a folder's context, or of the repo's for the top level.
func (repository *Repository) PendingForFolder(folderId string) *Suggestion {
	for index := range repository.Suggestions {
		suggestion := &repository.Suggestions[index]
		if suggestion.Kind != SuggestFolder && suggestion.FolderId == folderId {
			return suggestion
		}
	}
	return nil
}

// ForgetSessionSuggestions drops the waiting suggestions that came from a session the user archived or deleted.
func (repository *Repository) ForgetSessionSuggestions(sessionId string) {
	repository.Suggestions = slices.DeleteFunc(repository.Suggestions, func(suggestion Suggestion) bool {
		return slices.Contains(suggestion.SessionIds, sessionId)
	})
}

// MoveByHand files a session where the user put it.
func (repository *Repository) MoveByHand(sessionId string, folderId string) {
	if session := repository.FindSession(sessionId); session != nil {
		session.FolderId, session.Archived, session.ArchivedAt = folderId, false, time.Time{}
	}
}
