package brain

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

const HabitStartingDocuments = "starting-documents"

// documentGroup is the sessions of one folder that start from the same doc.
type documentGroup struct {
	folderId   string
	filePath   string
	sessionIds []string
}

// documentGroups groups each folder's sessions by the doc their first prompt points at. A session pointing at several
// counts for the one most sessions in its folder start from. Sessions already in a folder linked to their doc,
// and those in branch or issue subfolders, are grouped already.
func documentGroups(observation Observation) []documentGroup {
	repository := observation.Repository
	type folderFile struct{ folderId, filePath string }
	counts := map[folderFile]int{}
	var eligible []store.Session
	for _, session := range repository.Sessions {
		folder := repository.FindFolder(session.FolderId)
		if session.Archived || len(observation.Digests[session.SessionId].StartingFiles) == 0 || (folder != nil && (folder.Branch != "" || folder.IssueKey != "")) {
			continue
		}
		eligible = append(eligible, session)
		for _, filePath := range observation.Digests[session.SessionId].StartingFiles {
			counts[folderFile{session.FolderId, filePath}]++
		}
	}
	var groups []documentGroup
	for _, session := range eligible {
		chosen := ""
		for _, filePath := range observation.Digests[session.SessionId].StartingFiles {
			if chosen == "" || counts[folderFile{session.FolderId, filePath}] > counts[folderFile{session.FolderId, chosen}] {
				chosen = filePath
			}
		}
		if folder := repository.FindFolder(session.FolderId); folder != nil && folder.LinkedPath == chosen {
			continue
		}
		groupIndex := -1
		for index, group := range groups {
			if group.folderId == session.FolderId && group.filePath == chosen {
				groupIndex = index
			}
		}
		if groupIndex < 0 {
			groups = append(groups, documentGroup{folderId: session.FolderId, filePath: chosen})
			groupIndex = len(groups) - 1
		}
		groups[groupIndex].sessionIds = append(groups[groupIndex].sessionIds, session.SessionId)
	}
	return groups
}

// groupableDocuments are the docs that 2+ sessions of a folder start from, with no subfolder for them yet.
func groupableDocuments(observation Observation) []documentGroup {
	var groupable []documentGroup
	for _, group := range documentGroups(observation) {
		if len(group.sessionIds) >= 2 && documentSubfolder(observation.Repository, group.folderId, group.filePath) == nil {
			groupable = append(groupable, group)
		}
	}
	return groupable
}

// documentSuggestions, once the habit is on, gives each doc's sessions the doc as context: by linking it to their folder
// when every session there starts from it, or else in a subfolder linked to it. Later sessions starting from a doc
// are filed into its subfolder.
func documentSuggestions(observation Observation) []store.Suggestion {
	repository := observation.Repository
	if !repository.IsHabitActive(HabitStartingDocuments) {
		return nil
	}
	var suggestions []store.Suggestion
	for _, group := range documentGroups(observation) {
		fileName := filepath.Base(group.filePath)
		if existing := documentSubfolder(repository, group.folderId, group.filePath); existing != nil {
			reason := fmt.Sprintf("It starts from %s.", fileName)
			suggestions = append(suggestions, store.Suggestion{Kind: store.SuggestMove, SessionIds: group.sessionIds, FolderId: existing.Id, Reason: reason, Habit: HabitStartingDocuments})
			continue
		}
		if len(group.sessionIds) < 2 {
			continue
		}
		folder := repository.FindFolder(group.folderId)
		if folder != nil && folder.LinkedPath == "" && len(group.sessionIds) == activeSessionCount(repository, folder.Id) {
			suggestions = append(suggestions, store.Suggestion{
				Kind:       store.SuggestLink,
				FolderId:   folder.Id,
				LinkedPath: group.filePath,
				Habit:      HabitStartingDocuments,
				Reason:     fmt.Sprintf("All %d sessions here start from it.", len(group.sessionIds)),
			})
			continue
		}
		suggestions = append(suggestions, store.Suggestion{
			Kind:           store.SuggestSubfolder,
			SessionIds:     group.sessionIds,
			ParentFolderId: group.folderId,
			LinkedPath:     group.filePath,
			FolderName:     documentFolderName(group.filePath),
			Habit:          HabitStartingDocuments,
			Reason:         fmt.Sprintf("They all start from %s.", fileName),
		})
	}
	return suggestions
}

func documentSubfolder(repository *store.Repository, parentId string, filePath string) *store.Folder {
	for index := range repository.Folders {
		if repository.Folders[index].ParentId == parentId && repository.Folders[index].LinkedPath == filePath {
			return &repository.Folders[index]
		}
	}
	return nil
}

func activeSessionCount(repository *store.Repository, folderId string) int {
	count := 0
	for _, session := range repository.Sessions {
		if session.FolderId == folderId && !session.Archived {
			count++
		}
	}
	return count
}

// documentFolderName names a folder after its doc: EVAL_PROJECT.md becomes "Eval project".
func documentFolderName(filePath string) string {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))
	name = strings.Join(strings.FieldsFunc(name, func(character rune) bool { return character == '_' || character == '-' }), " ")
	if name == "" {
		return filepath.Base(filePath)
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
