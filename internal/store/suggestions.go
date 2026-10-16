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
	// SuggestContext rewrites a folder's context.md, or the repo's for the top level.
	SuggestContext SuggestionKind = "context"
	// SuggestCleanup archives sessions that look finished.
	SuggestCleanup SuggestionKind = "cleanup"
	// SuggestLink links a markdown file an agent wrote as a folder's context.
	SuggestLink SuggestionKind = "link"
	// SuggestLaptopRule adds a rule to the user's own files that agents load in every repo.
	SuggestLaptopRule SuggestionKind = "laptopRule"
	// SuggestQuestion asks about a habit den isn't sure of; a yes turns it on.
	SuggestQuestion SuggestionKind = "question"
)

// Starters propose the first context of a folder that has none.
const (
	StarterPlan  = "plan"
	StarterDraft = "draft"
)

type Suggestion struct {
	Id     string         `json:"id"`
	Kind   SuggestionKind `json:"kind"`
	Reason string         `json:"reason"`
	// SessionIds are the sessions a move, subfolder or cleanup acts on.
	SessionIds []string `json:"sessionIds,omitempty"`
	// FolderId is where a move goes, or whose context a context edit changes.
	FolderId string `json:"folderId,omitempty"`
	// ParentFolderId, FolderName and the Branch or IssueKey it groups describe the subfolder to create.
	ParentFolderId string `json:"parentFolderId,omitempty"`
	Branch         string `json:"branch,omitempty"`
	IssueKey       string `json:"issueKey,omitempty"`
	FolderName     string `json:"folderName,omitempty"`
	// ProposedText is the whole new file for a context edit; Source says where it was learned.
	ProposedText string `json:"proposedText,omitempty"`
	Source       string `json:"source,omitempty"`
	// Starter says a context edit is a folder's first: an approved plan, or a draft den made.
	Starter string `json:"starter,omitempty"`
	// LinkedPath is the file a link proposes, or the context of a subfolder for the sessions that start from it.
	LinkedPath string `json:"linkedPath,omitempty"`
	// Rule is the line a laptop rule or a question's yes adds; FilePaths are the laptop files that don't have it yet.
	Rule      string   `json:"rule,omitempty"`
	FilePaths []string `json:"filePaths,omitempty"`
	// ByModel is set once den's model wrote the proposed text, so it isn't asked again.
	ByModel bool `json:"byModel,omitempty"`
	// Habit names the habit a suggestion comes from, so the answer counts for or against it.
	Habit     string    `json:"habit,omitempty"`
	Question  string    `json:"question,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// isSessionKind tells whether a suggestion acts on sessions, so it goes away when its sessions do.
func isSessionKind(kind SuggestionKind) bool {
	return kind == SuggestMove || kind == SuggestSubfolder || kind == SuggestCleanup
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
		return []string{"subfolder:" + suggestion.ParentFolderId + ":" + suggestion.Branch + suggestion.IssueKey + suggestion.LinkedPath}
	case SuggestCleanup:
		var keys []string
		for _, sessionId := range suggestion.SessionIds {
			keys = append(keys, "cleanup:"+sessionId)
		}
		return keys
	case SuggestLink:
		return []string{"link:" + suggestion.FolderId + ":" + suggestion.LinkedPath}
	case SuggestLaptopRule:
		return []string{"laptop:" + suggestion.Rule}
	case SuggestQuestion:
		return []string{"question:" + suggestion.Habit}
	}
	if suggestion.Starter == StarterDraft {
		return []string{"draft:" + suggestion.FolderId}
	}
	return []string{"context:" + suggestion.FolderId + ":" + suggestion.ProposedText}
}

// IsRejected tells whether the user already said no to this suggestion, for every session it names.
func (repository *Repository) IsRejected(suggestion Suggestion) bool {
	return repository.isRejected(suggestion)
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
	if !isSessionKind(suggestion.Kind) {
		if repository.isRejected(suggestion) {
			return false
		}
		if suggestion.Kind == SuggestLaptopRule {
			return repository.replaceLaptopRule(suggestion)
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
		if suggestion.Habit != "" {
			repository.yieldToHabit(suggestion.SessionIds)
		}
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
	if suggestion.Kind == SuggestQuestion {
		repository.LastQuestionAt = suggestion.CreatedAt
	}
	repository.Suggestions = append(repository.Suggestions, suggestion)
	return true
}

// yieldToHabit takes sessions out of den's plain filing and grouping proposals, since a habit the user turned on
// groups them now; proposals left with too few sessions go away.
func (repository *Repository) yieldToHabit(sessionIds []string) {
	for index := range repository.Suggestions {
		pending := &repository.Suggestions[index]
		if pending.Habit != "" || (pending.Kind != SuggestMove && pending.Kind != SuggestSubfolder) {
			continue
		}
		pending.SessionIds = slices.DeleteFunc(pending.SessionIds, func(sessionId string) bool {
			return slices.Contains(sessionIds, sessionId)
		})
	}
	repository.Suggestions = slices.DeleteFunc(repository.Suggestions, func(pending Suggestion) bool {
		return (pending.Kind == SuggestSubfolder && len(pending.SessionIds) < 2) || (pending.Kind == SuggestMove && len(pending.SessionIds) == 0)
	})
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

// replaceLaptopRule keeps a waiting laptop rule current as the files change: it names the files that still lack it,
// and goes away once none do.
func (repository *Repository) replaceLaptopRule(rule Suggestion) bool {
	pendingIndex := slices.IndexFunc(repository.Suggestions, func(pending Suggestion) bool {
		return pending.Kind == SuggestLaptopRule && pending.Rule == rule.Rule
	})
	if pendingIndex < 0 {
		if len(rule.FilePaths) == 0 {
			return false
		}
		rule.Id = newId()
		rule.CreatedAt = time.Now()
		repository.Suggestions = append(repository.Suggestions, rule)
		return true
	}
	pending := &repository.Suggestions[pendingIndex]
	if len(rule.FilePaths) == 0 {
		repository.Dismiss(pending.Id)
		return true
	}
	if slices.Equal(pending.FilePaths, rule.FilePaths) && pending.Reason == rule.Reason {
		return false
	}
	pending.FilePaths, pending.Reason = rule.FilePaths, rule.Reason
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
// A no to a question retires its habit; a no to a habit's suggestion counts against the habit.
func (repository *Repository) Reject(suggestionId string) {
	if suggestion := repository.FindSuggestion(suggestionId); suggestion != nil {
		repository.RejectedKeys = append(repository.RejectedKeys, suggestion.RejectionKeys()...)
		if suggestion.Kind == SuggestQuestion {
			repository.setHabitState(suggestion.Habit, HabitRetired)
		} else if suggestion.Habit != "" {
			repository.countHabitAnswer(suggestion.Habit, false)
		}
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
		createdFolder := repository.FindFolder(subfolder.Id)
		createdFolder.Branch, createdFolder.IssueKey, createdFolder.LinkedPath = suggestion.Branch, suggestion.IssueKey, suggestion.LinkedPath
		repository.moveSessions(suggestion.SessionIds, subfolder.Id)
	case SuggestCleanup:
		for _, sessionId := range suggestion.SessionIds {
			repository.SetArchived(sessionId, true)
		}
	case SuggestContext:
		if err := repository.WriteContext(suggestion.FolderId, suggestion.ProposedText); err != nil {
			return err
		}
		if suggestion.Starter != "" {
			repository.ForgetFolderStarters(suggestion.FolderId)
		}
	case SuggestLink:
		if folder := repository.FindFolder(suggestion.FolderId); folder != nil {
			folder.LinkedPath = suggestion.LinkedPath
		}
		repository.ForgetFolderStarters(suggestion.FolderId)
	case SuggestLaptopRule:
		for _, filePath := range suggestion.FilePaths {
			if _, err := AppendRule(filePath, suggestion.Rule); err != nil {
				return err
			}
		}
	case SuggestQuestion:
		repository.setHabitState(suggestion.Habit, HabitActive)
		if suggestion.Rule != "" {
			contextPath, err := repository.ContextPath("")
			if err != nil {
				return err
			}
			if _, err := AppendRule(contextPath, suggestion.Rule); err != nil {
				return err
			}
		}
	}
	if suggestion.Habit != "" && suggestion.Kind != SuggestQuestion {
		repository.countHabitAnswer(suggestion.Habit, true)
	}
	repository.Dismiss(suggestionId)
	return nil
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

// ForgetFolderStarters drops what den proposed as a folder's first context, once the folder has one.
func (repository *Repository) ForgetFolderStarters(folderId string) {
	repository.Suggestions = slices.DeleteFunc(repository.Suggestions, func(suggestion Suggestion) bool {
		return suggestion.FolderId == folderId && (suggestion.Kind == SuggestLink || (suggestion.Kind == SuggestContext && suggestion.Starter != ""))
	})
}

// PendingFor returns the waiting suggestion that would file or archive this session, if any.
func (repository *Repository) PendingFor(sessionId string) *Suggestion {
	for index := range repository.Suggestions {
		suggestion := &repository.Suggestions[index]
		if isSessionKind(suggestion.Kind) && slices.Contains(suggestion.SessionIds, sessionId) {
			return suggestion
		}
	}
	return nil
}

// PendingForFolder returns the waiting edit or link of a folder's context, or of the repo's for the top level.
func (repository *Repository) PendingForFolder(folderId string) *Suggestion {
	for index := range repository.Suggestions {
		suggestion := &repository.Suggestions[index]
		if (suggestion.Kind == SuggestContext || suggestion.Kind == SuggestLink) && suggestion.FolderId == folderId {
			return suggestion
		}
	}
	return nil
}

// PendingOfKind returns the first waiting suggestion of a kind, like the open question.
func (repository *Repository) PendingOfKind(kind SuggestionKind) *Suggestion {
	for index := range repository.Suggestions {
		if repository.Suggestions[index].Kind == kind {
			return &repository.Suggestions[index]
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
	repository.SetArchived(sessionId, true)
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
		return isSessionKind(suggestion.Kind) && len(suggestion.SessionIds) == 0
	})
}

// MoveByHand files a session where the user put it, dropping what den suggested for it.
func (repository *Repository) MoveByHand(sessionId string, folderId string) {
	if session := repository.FindSession(sessionId); session != nil {
		session.FolderId, session.Archived, session.ArchivedAt = folderId, false, time.Time{}
		session.FiledByHand = folderId != ""
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
			session.Archived, session.ArchivedAt = false, time.Time{}
		}
	}
	for _, sessionId := range sessionIds {
		repository.ForgetSessionSuggestions(sessionId)
	}
}
