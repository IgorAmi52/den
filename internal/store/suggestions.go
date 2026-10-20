package store

import (
	"path/filepath"
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
	// SuggestRule adds a rule to the user's own files that agents load in every repo, like ~/.claude/CLAUDE.md.
	SuggestRule SuggestionKind = "rule"
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
	LinkedPath string `json:"linkedPath,omitempty"`
	// Evidence are the quotes from the session den based the suggestion on, so it can show and explain them.
	Evidence []string `json:"evidence,omitempty"`
	// Rule is the line a rule adds; FilePaths are the laptop files that don't have it yet.
	Rule      string    `json:"rule,omitempty"`
	FilePaths []string  `json:"filePaths,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// CallId is the model call that proposed it, for the decision log.
	CallId string `json:"callId,omitempty"`
	// KeepKind is what a card review card keeps: notes, folder, skill or rule. A session has at most one waiting card of each,
	// and cards from den's free rules have none.
	KeepKind string `json:"keepKind,omitempty"`
}

// maximumCardRecords is how many of a session's cards it remembers.
const maximumCardRecords = 10

// CardSummary is a card in one line, for the card review to read what it already proposed.
func CardSummary(suggestion Suggestion) string {
	var what string
	switch suggestion.Kind {
	case SuggestFolder:
		what = "new folder " + suggestion.FolderName
		if suggestion.Skill {
			what = "new skill folder " + suggestion.FolderName
		}
	case SuggestRule:
		what = "rule for every repo: " + suggestion.Rule
	case SuggestLink:
		what = "link " + filepath.Base(suggestion.LinkedPath) + " as a folder's context"
	default:
		what = "notes for a folder's context"
	}
	if suggestion.Reason == "" {
		return what
	}
	return what + " · " + suggestion.Reason
}

// RejectionKeys identify what was rejected, so the same suggestion is never offered again.
func (suggestion Suggestion) RejectionKeys() []string {
	switch suggestion.Kind {
	case SuggestLink:
		return []string{"link:" + suggestion.FolderId + ":" + suggestion.LinkedPath}
	case SuggestRule:
		return []string{"rule:" + normalizedWords(suggestion.Rule)}
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
// of the same kind waiting, another one waits to change the same folder's context, or enough are waiting already.
// A rule stands beside a session's other suggestions, since it's about the user, not the work. It reports whether the queue changed.
func (repository *Repository) Offer(suggestion Suggestion) bool {
	if repository.IsRejected(suggestion) || len(repository.Suggestions) >= maximumPendingSuggestions {
		return false
	}
	for _, pending := range repository.Suggestions {
		if slices.Equal(pending.RejectionKeys(), suggestion.RejectionKeys()) {
			return false
		}
		isAboutWork := suggestion.Kind != SuggestRule && pending.Kind != SuggestRule
		sharesSession := slices.ContainsFunc(suggestion.SessionIds, func(sessionId string) bool { return slices.Contains(pending.SessionIds, sessionId) })
		if isAboutWork && sharesSession && pending.KeepKind == suggestion.KeepKind {
			return false
		}
		if isAboutWork && suggestion.Kind != SuggestFolder && pending.Kind != SuggestFolder && pending.FolderId == suggestion.FolderId {
			return false
		}
	}
	suggestion.Id = newId()
	if suggestion.CreatedAt.IsZero() {
		suggestion.CreatedAt = time.Now()
	}
	repository.Suggestions = append(repository.Suggestions, suggestion)
	if suggestion.KeepKind != "" {
		for _, sessionId := range suggestion.SessionIds {
			if session := repository.FindSession(sessionId); session != nil {
				session.Cards = append(session.Cards, CardRecord{SuggestionId: suggestion.Id, Kind: suggestion.KeepKind, Summary: CardSummary(suggestion), Outcome: "waiting"})
				session.Cards = session.Cards[max(len(session.Cards)-maximumCardRecords, 0):]
			}
		}
	}
	return true
}

// Replace offers a suggestion in place of a waiting one, which goes only if the new one is offered. It reports whether it was.
func (repository *Repository) Replace(waitingId string, suggestion Suggestion) bool {
	waiting := repository.FindSuggestion(waitingId)
	if waiting == nil {
		return repository.Offer(suggestion)
	}
	kept := *waiting
	repository.Dismiss(waitingId)
	if !repository.Offer(suggestion) {
		repository.Suggestions = append(repository.Suggestions, kept)
		return false
	}
	repository.recordCardOutcome(waitingId, "replaced")
	return true
}

// Withdraw drops a waiting suggestion den itself no longer stands behind. Unlike a rejection, nothing is remembered against it.
func (repository *Repository) Withdraw(suggestionId string) {
	repository.recordCardOutcome(suggestionId, "withdrawn")
	repository.Dismiss(suggestionId)
}

// WaitingFor returns the waiting suggestions that came from a session, oldest first.
func (repository *Repository) WaitingFor(sessionId string) []Suggestion {
	var waiting []Suggestion
	for _, suggestion := range repository.Suggestions {
		if slices.Contains(suggestion.SessionIds, sessionId) {
			waiting = append(waiting, suggestion)
		}
	}
	return waiting
}

// recordCardOutcome notes what became of a card in the history of the sessions it came from.
func (repository *Repository) recordCardOutcome(suggestionId string, outcome string) {
	for index := range repository.Sessions {
		for cardIndex := range repository.Sessions[index].Cards {
			if repository.Sessions[index].Cards[cardIndex].SuggestionId == suggestionId {
				repository.Sessions[index].Cards[cardIndex].Outcome = outcome
			}
		}
	}
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
	repository.recordCardOutcome(suggestionId, "rejected")
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
	case SuggestRule:
		for _, filePath := range suggestion.FilePaths {
			if _, err := AppendRule(filePath, suggestion.Rule); err != nil {
				return "", err
			}
		}
	}
	repository.recordCardOutcome(suggestionId, "accepted")
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

// PendingFolderFor returns the waiting new folder for this session, if any.
func (repository *Repository) PendingFolderFor(sessionId string) *Suggestion {
	for index := range repository.Suggestions {
		if repository.Suggestions[index].Kind == SuggestFolder && slices.Contains(repository.Suggestions[index].SessionIds, sessionId) {
			return &repository.Suggestions[index]
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

// PendingRule returns the first waiting rule for the laptop files.
func (repository *Repository) PendingRule() *Suggestion {
	for index := range repository.Suggestions {
		if repository.Suggestions[index].Kind == SuggestRule {
			return &repository.Suggestions[index]
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
