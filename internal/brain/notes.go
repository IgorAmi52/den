package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	// minimumNotesFileSize keeps scratch files out: a file worth linking is a real plan or spec.
	minimumNotesFileSize = 800
	minimumPlanLength    = 200
	// draftAfterSessions and draftAfterPrompts are when a folder has enough real work for den to draft its notes.
	draftAfterSessions = 3
	draftAfterPrompts  = 3
)

// notNotesFiles are markdown files every repo has, and the agents' own skill and memory files,
// which are never a folder's notes.
var notNotesFiles = toSet("readme.md changelog.md claude.md agents.md license.md contributing.md skill.md memory.md")

// folderNotes offers a first context to each folder that has none: a file an agent wrote there,
// a plan the user approved there, or a draft once the folder holds real work. Only one at a time per folder.
func folderNotes(observation Observation) []store.Suggestion {
	repository := observation.Repository
	var suggestions []store.Suggestion
	for _, folder := range repository.Folders {
		if _, hasContext := observation.ContextTokens[folder.Id]; hasContext || folder.LinkedPath != "" || repository.PendingForFolder(folder.Id) != nil {
			continue
		}
		var sessions []store.Session
		for _, session := range repository.Sessions {
			if session.FolderId == folder.Id && !session.Archived {
				sessions = append(sessions, session)
			}
		}
		for _, propose := range []func(Observation, store.Folder, []store.Session) (store.Suggestion, bool){writtenNotes, approvedPlan, draftNotes} {
			if suggestion, found := propose(observation, folder, sessions); found && !repository.IsRejected(suggestion) {
				suggestions = append(suggestions, suggestion)
				break
			}
		}
	}
	return suggestions
}

// StaleDrafts are the folders with a waiting draft that no longer has enough real sessions for one,
// because sessions moved out or were archived since.
func StaleDrafts(observation Observation) []string {
	var folderIds []string
	for _, suggestion := range observation.Repository.Suggestions {
		if suggestion.Kind != store.SuggestContext || suggestion.Starter != store.StarterDraft {
			continue
		}
		worked := 0
		for _, session := range observation.Repository.Sessions {
			if session.FolderId == suggestion.FolderId && !session.Archived && observation.Digests[session.SessionId].PromptCount >= draftAfterPrompts {
				worked++
			}
		}
		if worked < draftAfterSessions {
			folderIds = append(folderIds, suggestion.FolderId)
		}
	}
	return folderIds
}

// writtenNotes finds the latest markdown file of real size an agent wrote in one of the folder's sessions.
func writtenNotes(observation Observation, folder store.Folder, sessions []store.Session) (store.Suggestion, bool) {
	for _, session := range sessions {
		digest := observation.Digests[session.SessionId]
		for index := len(digest.TouchedFiles) - 1; index >= 0; index-- {
			filePath := digest.TouchedFiles[index]
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(digest.WorkingDirectory, filePath)
			}
			if !isNotesFile(observation.Repository, filePath) {
				continue
			}
			return store.Suggestion{
				Kind:       store.SuggestLink,
				FolderId:   folder.Id,
				LinkedPath: filePath,
				Source:     session.Title,
				Reason:     fmt.Sprintf("An agent wrote it in \"%s\".", truncateText(session.Title, 40)),
			}, true
		}
	}
	return store.Suggestion{}, false
}

// approvedPlan offers the latest plan approved in one of the folder's sessions as the folder's notes.
func approvedPlan(observation Observation, folder store.Folder, sessions []store.Session) (store.Suggestion, bool) {
	for _, session := range sessions {
		plan := strings.TrimSpace(observation.Digests[session.SessionId].Plan)
		if len(plan) < minimumPlanLength {
			continue
		}
		return store.Suggestion{
			Kind:         store.SuggestContext,
			Starter:      store.StarterPlan,
			FolderId:     folder.Id,
			ProposedText: plan + "\n",
			Source:       session.Title,
			Reason:       fmt.Sprintf("You approved it in \"%s\".", truncateText(session.Title, 40)),
		}, true
	}
	return store.Suggestion{}, false
}

// draftNotes drafts notes for a folder with a few real sessions: what they worked on, where the code is, the branches.
// Until a model is connected, it's a starting point built from facts only.
func draftNotes(observation Observation, folder store.Folder, sessions []store.Session) (store.Suggestion, bool) {
	var worked []store.Session
	var digests []agent.Digest
	for _, session := range sessions {
		if digest, found := observation.Digests[session.SessionId]; found && digest.PromptCount >= draftAfterPrompts {
			worked = append(worked, session)
			digests = append(digests, digest)
		}
	}
	if len(worked) < draftAfterSessions {
		return store.Suggestion{}, false
	}
	var draft strings.Builder
	fmt.Fprintf(&draft, "# %s\n\n## Work so far\n", folder.Name)
	for _, session := range worked[:min(8, len(worked))] {
		fmt.Fprintf(&draft, "- %s\n", session.Title)
	}
	writeCodeSections(&draft, digests, observation.Repository.RepositoryPath)
	return store.Suggestion{
		Kind:         store.SuggestContext,
		Starter:      store.StarterDraft,
		FolderId:     folder.Id,
		ProposedText: draft.String(),
		Reason:       fmt.Sprintf("%s here, no notes yet.", countedSessions(len(worked), "session")),
	}, true
}

func isNotesFile(repository *store.Repository, filePath string) bool {
	if !strings.EqualFold(filepath.Ext(filePath), ".md") || notNotesFiles[strings.ToLower(filepath.Base(filePath))] {
		return false
	}
	// Claude keeps its memories as markdown files in a memory directory of each project.
	if slices.Contains(strings.Split(filepath.Dir(filePath), string(filepath.Separator)), "memory") {
		return false
	}
	if dataDirectory, err := store.DataDirectory(); err == nil && strings.HasPrefix(filePath, dataDirectory) {
		return false
	}
	if slices.ContainsFunc(repository.Folders, func(folder store.Folder) bool { return folder.LinkedPath == filePath }) {
		return false
	}
	information, err := os.Stat(filePath)
	return err == nil && !information.IsDir() && information.Size() >= minimumNotesFileSize
}

func truncateText(text string, maximumLength int) string {
	runes := []rune(text)
	if len(runes) <= maximumLength {
		return text
	}
	return string(runes[:maximumLength-1]) + "…"
}
