package ui

import (
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

type rowKind int

const (
	inboxRow rowKind = iota
	folderRow
	archiveRow
	sessionRow
	// newAgentRow is an agent opened from den that has no first prompt yet, so no saved session.
	newAgentRow
)

// listRow is one line of the panel tree. A saved session that is open in a pane has both session and open set.
type listRow struct {
	kind    rowKind
	folder  *store.Folder
	session *store.Session
	open    *openAgent
	depth   int
}

func (row listRow) folderId() string {
	switch row.kind {
	case folderRow:
		return row.folder.Id
	case sessionRow:
		return row.session.FolderId
	case newAgentRow:
		return row.open.folderId
	}
	return ""
}

// isMovable tells whether the row is something that can be carried into a folder.
func (row listRow) isMovable() bool {
	return row.kind == sessionRow || row.kind == newAgentRow
}

// rows lists the tree: the Inbox first, then the folders by name, then the archive when it holds anything.
func (currentModel model) rows() []listRow {
	rows := []listRow{{kind: inboxRow}}
	if !currentModel.inboxCollapsed {
		rows = append(rows, currentModel.rowsInFolder("", 1)...)
	}
	for _, folder := range currentModel.subfolders("") {
		rows = append(rows, listRow{kind: folderRow, folder: folder})
		if !folder.Collapsed {
			rows = append(rows, currentModel.rowsInFolder(folder.Id, 1)...)
		}
	}
	if currentModel.repository.ArchivedCount() > 0 {
		rows = append(rows, listRow{kind: archiveRow})
		if currentModel.archiveExpanded {
			for _, session := range currentModel.archivedSessions() {
				rows = append(rows, listRow{kind: sessionRow, session: session, open: currentModel.openAgentFor(session.SessionId), depth: 1})
			}
		}
	}
	return rows
}

// rowsInFolder lists one folder's contents: new agents first, then subfolders by name with their contents
// unless collapsed, then sessions newest first. The top level's subfolders are listed by rows, beside the Inbox.
func (currentModel model) rowsInFolder(folderId string, depth int) []listRow {
	var rows []listRow
	for agentIndex := range currentModel.openAgents {
		open := &currentModel.openAgents[agentIndex]
		isUntracked := open.sessionId == "" || currentModel.repository.FindSession(open.sessionId) == nil
		if isUntracked && open.folderId == folderId {
			rows = append(rows, listRow{kind: newAgentRow, open: open, depth: depth})
		}
	}
	if folderId != "" {
		for _, folder := range currentModel.subfolders(folderId) {
			rows = append(rows, listRow{kind: folderRow, folder: folder, depth: depth})
			if !folder.Collapsed {
				rows = append(rows, currentModel.rowsInFolder(folder.Id, depth+1)...)
			}
		}
	}
	for _, session := range currentModel.sessionsIn(folderId) {
		rows = append(rows, listRow{kind: sessionRow, session: session, open: currentModel.openAgentFor(session.SessionId), depth: depth})
	}
	return rows
}

// sessionsIn lists the folder's own sessions that aren't archived, newest first.
func (currentModel model) sessionsIn(folderId string) []*store.Session {
	var sessions []*store.Session
	for sessionIndex := range currentModel.repository.Sessions {
		session := &currentModel.repository.Sessions[sessionIndex]
		if session.FolderId == folderId && !session.Archived {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

func (currentModel model) archivedSessions() []*store.Session {
	var sessions []*store.Session
	for sessionIndex := range currentModel.repository.Sessions {
		if currentModel.repository.Sessions[sessionIndex].Archived {
			sessions = append(sessions, &currentModel.repository.Sessions[sessionIndex])
		}
	}
	return sessions
}

func (currentModel model) subfolders(parentId string) []*store.Folder {
	var subfolders []*store.Folder
	for folderIndex := range currentModel.repository.Folders {
		if currentModel.repository.Folders[folderIndex].ParentId == parentId {
			subfolders = append(subfolders, &currentModel.repository.Folders[folderIndex])
		}
	}
	slices.SortFunc(subfolders, func(first *store.Folder, second *store.Folder) int {
		return strings.Compare(strings.ToLower(first.Name), strings.ToLower(second.Name))
	})
	return subfolders
}

func (currentModel model) selectedRow() (listRow, bool) {
	rows := currentModel.rows()
	if currentModel.listCursor >= len(rows) {
		return listRow{}, false
	}
	return rows[currentModel.listCursor], true
}

func (currentModel *model) cursorTo(matches func(row listRow) bool) {
	if rowIndex := slices.IndexFunc(currentModel.rows(), matches); rowIndex >= 0 {
		currentModel.listCursor = rowIndex
	}
}

// clampCursor keeps the cursor on a row after rows went away.
func (currentModel *model) clampCursor() {
	currentModel.listCursor = max(min(currentModel.listCursor, len(currentModel.rows())-1), 0)
}

// folderForNewItems is the folder a new session or folder goes into: the selected folder, or the selected session's folder.
func (currentModel model) folderForNewItems() string {
	if selected, exists := currentModel.selectedRow(); exists && selected.kind != archiveRow {
		if selected.kind == sessionRow && selected.session.Archived {
			return ""
		}
		return selected.folderId()
	}
	return ""
}

// moveTargets lists where a carried session can go, in screen order: the Inbox, then every visible folder.
func (currentModel model) moveTargets() []string {
	var targets []string
	for _, row := range currentModel.rows() {
		switch row.kind {
		case inboxRow:
			targets = append(targets, "")
		case folderRow:
			targets = append(targets, row.folder.Id)
		}
	}
	return targets
}

// sameItemAs matches the row showing the same folder, session or pane, even after the list was rebuilt.
func sameItemAs(row listRow) func(listRow) bool {
	switch row.kind {
	case folderRow:
		folderId := row.folder.Id
		return func(candidate listRow) bool { return candidate.kind == folderRow && candidate.folder.Id == folderId }
	case sessionRow:
		sessionId := row.session.SessionId
		return func(candidate listRow) bool {
			return candidate.kind == sessionRow && candidate.session.SessionId == sessionId
		}
	case newAgentRow:
		paneId := row.open.paneId
		return func(candidate listRow) bool { return candidate.open != nil && candidate.open.paneId == paneId }
	}
	kind := row.kind
	return func(candidate listRow) bool { return candidate.kind == kind }
}
