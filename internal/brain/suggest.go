// Package brain is what den thinks with. For now it runs local rules only, with no model calls;
// the rules turn what den observes into suggestions the user confirms.
package brain

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	// staleAfter is how long a session sits untouched before den offers to archive it.
	staleAfter = 21 * 24 * time.Hour
	// mergedGracePeriod keeps a session around for a while after its branch merged.
	mergedGracePeriod = 2 * 24 * time.Hour
)

var defaultBranches = map[string]bool{"": true, "HEAD": true, "main": true, "master": true, "develop": true, "dev": true, "trunk": true}

// Observation is everything the rules look at. All of it is local and free to read.
type Observation struct {
	Repository     *store.Repository
	Digests        map[string]agent.Digest
	OpenSessionIds map[string]bool
	MergedBranches map[string]bool
	CommitSubjects []string
	// ContextTokens has an entry for each folder with context, the repo's under "".
	ContextTokens map[string]int
	LaptopFiles   []LaptopFile
	// ConventionalRepositories names the other repos on this machine whose commits are conventional.
	ConventionalRepositories []string
	Now                      time.Time
}

// Suggest runs every rule. The store drops the suggestions the user already rejected or is still looking at.
// Docs go first, since grouping by them also gives the sessions context; then issues, since one issue can span branches.
// The cleanup comes last and may hold no sessions, which clears a cleanup that no longer applies.
func Suggest(observation Observation) []store.Suggestion {
	var suggestions []store.Suggestion
	suggestions = append(suggestions, fileByBranch(observation)...)
	suggestions = append(suggestions, documentSuggestions(observation)...)
	suggestions = append(suggestions, issueSubfolders(observation)...)
	suggestions = append(suggestions, branchSubfolders(observation)...)
	suggestions = append(suggestions, folderNotes(observation)...)
	suggestions = append(suggestions, laptopRules(observation)...)
	suggestions = append(suggestions, questions(observation)...)
	return append(suggestions, cleanup(observation))
}

// fileByBranch files an Inbox session into the folder that already holds other sessions of its branch,
// or into a folder whose name appears in its branch or title. Sessions that only asked questions are left for the cleanup.
func fileByBranch(observation Observation) []store.Suggestion {
	repository := observation.Repository
	var suggestions []store.Suggestion
	for _, session := range repository.Sessions {
		if session.FolderId != "" || session.Archived || OnlyAskedQuestions(observation.Digests, session.SessionId) {
			continue
		}
		branch := branchOf(session, observation.Digests)
		if folderId, count := folderHoldingBranch(repository, branch, session.SessionId, observation.Digests); folderId != "" {
			reason := fmt.Sprintf("%s on %s %s in %s.", countedSessions(count, "other session"), branch, isOrAre(count), repository.FolderName(folderId))
			suggestions = append(suggestions, store.Suggestion{Kind: store.SuggestMove, SessionIds: []string{session.SessionId}, FolderId: folderId, Reason: reason})
			continue
		}
		if folder, where := folderNamedIn(repository, session, branch); folder != nil {
			reason := fmt.Sprintf("Its %s mentions %s.", where, strings.ToLower(folder.Name))
			suggestions = append(suggestions, store.Suggestion{Kind: store.SuggestMove, SessionIds: []string{session.SessionId}, FolderId: folder.Id, Reason: reason})
		}
	}
	return suggestions
}

// branchSubfolders offers a ⎇ subfolder once two or more sessions in a folder share a branch,
// and files later sessions of that branch into the subfolder that already exists.
func branchSubfolders(observation Observation) []store.Suggestion {
	repository := observation.Repository
	var suggestions []store.Suggestion
	for _, folder := range repository.Folders {
		if folder.Branch != "" || folder.IssueKey != "" {
			continue
		}
		sessionIdsByBranch := map[string][]string{}
		var branches []string
		for _, session := range repository.Sessions {
			branch := branchOf(session, observation.Digests)
			if session.FolderId != folder.Id || session.Archived || defaultBranches[branch] {
				continue
			}
			if _, seen := sessionIdsByBranch[branch]; !seen {
				branches = append(branches, branch)
			}
			sessionIdsByBranch[branch] = append(sessionIdsByBranch[branch], session.SessionId)
		}
		for _, branch := range branches {
			sessionIds := sessionIdsByBranch[branch]
			if existing := branchSubfolder(repository, folder.Id, branch); existing != nil {
				reason := fmt.Sprintf("%s is the subfolder for %s.", "⎇ "+existing.Name, branch)
				suggestions = append(suggestions, store.Suggestion{Kind: store.SuggestMove, SessionIds: sessionIds, FolderId: existing.Id, Reason: reason})
				continue
			}
			if len(sessionIds) < 2 {
				continue
			}
			suggestions = append(suggestions, store.Suggestion{
				Kind:           store.SuggestSubfolder,
				SessionIds:     sessionIds,
				ParentFolderId: folder.Id,
				Branch:         branch,
				FolderName:     ShortBranch(branch),
				Reason:         fmt.Sprintf("%d sessions in %s are on %s.", len(sessionIds), folder.Name, branch),
			})
		}
	}
	return suggestions
}

// cleanup offers to archive sessions that look finished: untouched for weeks, on a branch that merged,
// or that only asked questions, which are done once their pane closes.
func cleanup(observation Observation) store.Suggestion {
	var sessionIds []string
	for _, session := range observation.Repository.Sessions {
		if session.Archived || observation.OpenSessionIds[session.SessionId] {
			continue
		}
		idle := observation.Now.Sub(session.LastActivityAt)
		if idle > staleAfter || (observation.MergedBranches[branchOf(session, observation.Digests)] && idle > mergedGracePeriod) || OnlyAskedQuestions(observation.Digests, session.SessionId) {
			sessionIds = append(sessionIds, session.SessionId)
		}
	}
	reason := fmt.Sprintf("Untouched for %d+ days, their branch merged, or they only asked questions.", int(staleAfter.Hours()/24))
	return store.Suggestion{Kind: store.SuggestCleanup, SessionIds: sessionIds, Reason: reason}
}

// OnlyAskedQuestions tells whether a session changed no files, so it was a question rather than work.
func OnlyAskedQuestions(digests map[string]agent.Digest, sessionId string) bool {
	digest, found := digests[sessionId]
	return found && digest.PromptCount > 0 && len(digest.TouchedFiles) == 0
}

// ShortBranch drops the owner prefix from a branch name: igor/eval-pagination becomes eval-pagination.
func ShortBranch(branch string) string {
	return branch[strings.LastIndex(branch, "/")+1:]
}

func branchOf(session store.Session, digests map[string]agent.Digest) string {
	if digest, found := digests[session.SessionId]; found && digest.Branch != "" {
		return digest.Branch
	}
	return session.Branch
}

func folderHoldingBranch(repository *store.Repository, branch string, exceptSessionId string, digests map[string]agent.Digest) (string, int) {
	if defaultBranches[branch] {
		return "", 0
	}
	countByFolder := map[string]int{}
	bestFolderId := ""
	for _, session := range repository.Sessions {
		if session.SessionId == exceptSessionId || session.FolderId == "" || branchOf(session, digests) != branch {
			continue
		}
		countByFolder[session.FolderId]++
		if countByFolder[session.FolderId] > countByFolder[bestFolderId] {
			bestFolderId = session.FolderId
		}
	}
	return bestFolderId, countByFolder[bestFolderId]
}

// folderNamedIn finds a folder whose name is a whole word of the session's branch or title.
func folderNamedIn(repository *store.Repository, session store.Session, branch string) (*store.Folder, string) {
	branchWords, titleWords := words(branch), words(session.Title)
	for index := range repository.Folders {
		folder := &repository.Folders[index]
		if folder.Branch != "" || folder.IssueKey != "" {
			continue
		}
		folderWords := words(folder.Name)
		if len(folderWords) == 0 {
			continue
		}
		if !defaultBranches[branch] && containsAll(branchWords, folderWords) {
			return folder, "branch " + branch
		}
		if containsAll(titleWords, folderWords) {
			return folder, "title"
		}
	}
	return nil, ""
}

func branchSubfolder(repository *store.Repository, parentId string, branch string) *store.Folder {
	for index := range repository.Folders {
		if repository.Folders[index].ParentId == parentId && repository.Folders[index].Branch == branch {
			return &repository.Folders[index]
		}
	}
	return nil
}

// words splits text into lowercase words, breaking on anything that isn't a letter or digit.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
		return !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9')
	})
}

func containsAll(haystack []string, needles []string) bool {
	for _, needle := range needles {
		if !slices.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func countedSessions(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func isOrAre(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
