package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	// minimumNotesFileSize keeps scratch files out: a file worth linking is a real plan or spec.
	minimumNotesFileSize = 800
	minimumPlanLength    = 200
)

// notNotesFiles are markdown files every repo has, and the agents' own skill and memory files,
// which are never a folder's notes.
var notNotesFiles = toSet("readme.md changelog.md claude.md agents.md license.md contributing.md skill.md memory.md")

// folderNotes offers a first context to each folder that has none: a file an agent wrote there,
// or a plan the user approved there. Only one at a time per folder.
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
		for _, propose := range []func(Observation, store.Folder, []store.Session) (store.Suggestion, bool){writtenNotes, approvedPlan} {
			if suggestion, found := propose(observation, folder, sessions); found && !repository.IsRejected(suggestion) {
				suggestions = append(suggestions, suggestion)
				break
			}
		}
	}
	return suggestions
}

// writtenNotes finds the latest markdown file of real size an agent wrote in one of the folder's sessions.
func writtenNotes(observation Observation, folder store.Folder, sessions []store.Session) (store.Suggestion, bool) {
	for _, session := range sessions {
		digest := observation.Digests[session.SessionId]
		for index := len(digest.TouchedFiles) - 1; index >= 0; index-- {
			filePath := absoluteIn(digest.WorkingDirectory, digest.TouchedFiles[index])
			if !isNotesFile(observation.Repository, filePath) {
				continue
			}
			return store.Suggestion{
				Kind:       store.SuggestLink,
				FolderId:   folder.Id,
				SessionIds: []string{session.SessionId},
				LinkedPath: filePath,
				Source:     session.Title,
				Reason:     fmt.Sprintf("An agent wrote it in \"%s\", and the folder has no context yet.", truncateText(session.Title, 40)),
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
			FolderId:     folder.Id,
			SessionIds:   []string{session.SessionId},
			ProposedText: plan + "\n",
			Source:       session.Title,
			Reason:       fmt.Sprintf("You approved this plan in \"%s\", and the folder has no context yet.", truncateText(session.Title, 40)),
		}, true
	}
	return store.Suggestion{}, false
}

func absoluteIn(directory string, filePath string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(directory, filePath)
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
