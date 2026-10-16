package store

import (
	"slices"
	"time"
)

type SuggestionKind string

const (
	// SuggestMove files sessions into an existing folder.
	SuggestMove SuggestionKind = "move"
	// SuggestSubfolder creates a branch or worktree subfolder and moves its sessions in.
	SuggestSubfolder SuggestionKind = "subfolder"
	// SuggestContext rewrites a folder's context.md, or a guide when Guide is set.
	SuggestContext SuggestionKind = "context"
	// SuggestCleanup archives sessions that look finished.
	SuggestCleanup SuggestionKind = "cleanup"
)

type Suggestion struct {
	Id     string         `json:"id"`
	Kind   SuggestionKind `json:"kind"`
	Reason string         `json:"reason"`
	// SessionIds are the sessions a move, subfolder or cleanup acts on.
	SessionIds []string `json:"sessionIds,omitempty"`
	// FolderId is where a move goes, or whose context a context edit changes.
	FolderId string `json:"folderId,omitempty"`
	// ParentFolderId, Branch and FolderName describe the subfolder to create.
	ParentFolderId string `json:"parentFolderId,omitempty"`
	Branch         string `json:"branch,omitempty"`
	FolderName     string `json:"folderName,omitempty"`
	// Guide names the guide a context edit changes instead of a folder's context.
	Guide string `json:"guide,omitempty"`
	// ProposedText is the whole new file for a context edit; Source says where it was learned.
	ProposedText string    `json:"proposedText,omitempty"`
	Source       string    `json:"source,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// RejectionKeys identify what was rejected, per session where it applies,
// so a rejected suggestion is not offered again even when it comes back in another batch.
func (suggestion Suggestion) RejectionKeys() []string {
	switch suggestion.Kind {
	case SuggestMove:
		var keys []string
		for _, sessionId := range suggestion.SessionIds {
			keys = append(keys, "move:"+sessionId+":"+suggestion.FolderId)
		}
		return keys
	case SuggestSubfolder:
		return []string{"subfolder:" + suggestion.ParentFolderId + ":" + suggestion.Branch}
	case SuggestCleanup:
		var keys []string
		for _, sessionId := range suggestion.SessionIds {
			keys = append(keys, "cleanup:"+sessionId)
		}
		return keys
	}
	return []string{"context:" + suggestion.FolderId + ":" + suggestion.Guide + ":" + suggestion.ProposedText}
}

func (repository *Repository) isRejected(suggestion Suggestion) bool {
	for _, key := range suggestion.RejectionKeys() {
		if slices.Contains(repository.RejectedKeys, key) {
			return true
		}
	}
	return false
}

// Offer queues a suggestion unless it was rejected before or the same one is already waiting.
// It reports whether the queue changed. Sessions already rejected for this suggestion are left out of it,
// a session gets at most one filing suggestion, and a cleanup replaces the waiting one, since each covers all finished sessions.
func (repository *Repository) Offer(suggestion Suggestion) bool {
	if suggestion.Kind == SuggestContext {
		if repository.isRejected(suggestion) {
			return false
		}
	} else {
		suggestion.SessionIds = slices.DeleteFunc(slices.Clone(suggestion.SessionIds), func(sessionId string) bool {
			single := suggestion
			single.SessionIds = []string{sessionId}
			return repository.isRejected(single)
		})
	}
	if suggestion.Kind == SuggestCleanup {
		// Each session shows one proposal, so one already waiting to be filed stays out of the cleanup.
		suggestion.SessionIds = slices.DeleteFunc(suggestion.SessionIds, func(sessionId string) bool {
			pending := repository.PendingFor(sessionId)
			return pending != nil && pending.Kind != SuggestCleanup
		})
		return repository.replaceCleanup(suggestion)
	}
	if suggestion.Kind == SuggestMove || suggestion.Kind == SuggestSubfolder {
		suggestion.SessionIds = slices.DeleteFunc(suggestion.SessionIds, func(sessionId string) bool {
			return repository.PendingFor(sessionId) != nil
		})
		if len(suggestion.SessionIds) == 0 || (suggestion.Kind == SuggestSubfolder && len(suggestion.SessionIds) < 2) {
			return false
		}
	}
	for _, pending := range repository.Suggestions {
		if slices.Equal(pending.RejectionKeys(), suggestion.RejectionKeys()) {
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

func (repository *Repository) replaceCleanup(cleanup Suggestion) bool {
	pendingIndex := slices.IndexFunc(repository.Suggestions, func(pending Suggestion) bool {
		return pending.Kind == SuggestCleanup
	})
	if pendingIndex < 0 {
		if len(cleanup.SessionIds) == 0 {
			return false
		}
		cleanup.Id = newId()
		cleanup.CreatedAt = time.Now()
		repository.Suggestions = append(repository.Suggestions, cleanup)
		return true
	}
	pending := &repository.Suggestions[pendingIndex]
	if len(cleanup.SessionIds) == 0 {
		repository.Dismiss(pending.Id)
		return true
	}
	isSameSet := len(pending.SessionIds) == len(cleanup.SessionIds) && !slices.ContainsFunc(cleanup.SessionIds, func(sessionId string) bool {
		return !slices.Contains(pending.SessionIds, sessionId)
	})
	if isSameSet && pending.Reason == cleanup.Reason {
		return false
	}
	pending.SessionIds, pending.Reason = cleanup.SessionIds, cleanup.Reason
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

// Accept carries the suggestion out and drops it from the queue.
func (repository *Repository) Accept(suggestionId string) error {
	found := repository.FindSuggestion(suggestionId)
	if found == nil {
		return nil
	}
	suggestion := *found
	suggestion.SessionIds = slices.Clone(found.SessionIds)
	switch suggestion.Kind {
	case SuggestMove:
		repository.moveSessions(suggestion.SessionIds, suggestion.FolderId)
	case SuggestSubfolder:
		subfolder := repository.AddFolder(suggestion.FolderName, suggestion.ParentFolderId)
		repository.FindFolder(subfolder.Id).Branch = suggestion.Branch
		repository.moveSessions(suggestion.SessionIds, subfolder.Id)
	case SuggestCleanup:
		for _, sessionId := range suggestion.SessionIds {
			if session := repository.FindSession(sessionId); session != nil {
				session.Archived = true
			}
		}
		for _, sessionId := range suggestion.SessionIds {
			repository.ForgetSessionSuggestions(sessionId)
		}
	case SuggestContext:
		var err error
		if suggestion.Guide != "" {
			err = repository.WriteGuide(suggestion.Guide, suggestion.ProposedText)
		} else {
			err = repository.WriteContext(suggestion.FolderId, suggestion.ProposedText)
		}
		if err != nil {
			return err
		}
	}
	repository.Dismiss(suggestionId)
	return nil
}

// PendingFor returns the waiting suggestion that would file or archive this session, if any.
func (repository *Repository) PendingFor(sessionId string) *Suggestion {
	for index := range repository.Suggestions {
		suggestion := &repository.Suggestions[index]
		if suggestion.Kind != SuggestContext && slices.Contains(suggestion.SessionIds, sessionId) {
			return suggestion
		}
	}
	return nil
}

// PendingContextFor returns the waiting edit of a folder's context, or of a guide when guide is set.
func (repository *Repository) PendingContextFor(folderId string, guide string) *Suggestion {
	for index := range repository.Suggestions {
		suggestion := &repository.Suggestions[index]
		if suggestion.Kind == SuggestContext && suggestion.FolderId == folderId && suggestion.Guide == guide {
			return suggestion
		}
	}
	return nil
}

// AcceptFor answers yes for one session's row. A cleanup only archives that session and keeps offering the rest;
// every other kind is accepted whole.
func (repository *Repository) AcceptFor(suggestionId string, sessionId string) error {
	suggestion := repository.FindSuggestion(suggestionId)
	if suggestion == nil || suggestion.Kind != SuggestCleanup {
		return repository.Accept(suggestionId)
	}
	if session := repository.FindSession(sessionId); session != nil {
		session.Archived = true
	}
	repository.ForgetSessionSuggestions(sessionId)
	return nil
}

// RejectFor answers no for one session's row. A cleanup keeps only that session and still offers the rest;
// every other kind is rejected whole.
func (repository *Repository) RejectFor(suggestionId string, sessionId string) {
	suggestion := repository.FindSuggestion(suggestionId)
	if suggestion == nil || suggestion.Kind != SuggestCleanup {
		repository.Reject(suggestionId)
		return
	}
	repository.RejectedKeys = append(repository.RejectedKeys, "cleanup:"+sessionId)
	repository.ForgetSessionSuggestions(sessionId)
}

// ForgetSessionSuggestions drops waiting suggestions about sessions the user just filed or archived by hand,
// along with subfolders left with fewer than two sessions to group.
func (repository *Repository) ForgetSessionSuggestions(sessionId string) {
	for index := range repository.Suggestions {
		suggestion := &repository.Suggestions[index]
		suggestion.SessionIds = slices.DeleteFunc(suggestion.SessionIds, func(candidate string) bool {
			return candidate == sessionId
		})
	}
	repository.Suggestions = slices.DeleteFunc(repository.Suggestions, func(suggestion Suggestion) bool {
		if suggestion.Kind == SuggestSubfolder {
			return len(suggestion.SessionIds) < 2
		}
		return suggestion.Kind != SuggestContext && len(suggestion.SessionIds) == 0
	})
}

// MoveByHand files a session where the user put it, dropping what den suggested for it.
func (repository *Repository) MoveByHand(sessionId string, folderId string) {
	if session := repository.FindSession(sessionId); session != nil {
		session.FolderId, session.Archived = folderId, false
		repository.ForgetSessionSuggestions(sessionId)
	}
}

// RememberMovedOut records that the user took a session out of a folder, which counts as rejecting that folder,
// so den never suggests moving it back.
func (repository *Repository) RememberMovedOut(sessionId string, fromFolderId string) {
	session := repository.FindSession(sessionId)
	if session != nil && fromFolderId != "" && session.FolderId != fromFolderId {
		repository.RejectedKeys = append(repository.RejectedKeys, "move:"+sessionId+":"+fromFolderId)
		repository.ForgetSessionSuggestions(sessionId)
	}
}

func (repository *Repository) moveSessions(sessionIds []string, folderId string) {
	for _, sessionId := range sessionIds {
		if session := repository.FindSession(sessionId); session != nil {
			session.FolderId = folderId
			session.Archived = false
		}
	}
	for _, sessionId := range sessionIds {
		repository.ForgetSessionSuggestions(sessionId)
	}
}
