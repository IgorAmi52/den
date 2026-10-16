package ui

import (
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

type rowKind int

const (
	// repositoryRow holds what applies to the whole repo: its context and the guides, shown as contextFileRow children.
	repositoryRow rowKind = iota
	contextFileRow
	inboxRow
	folderRow
	archiveRow
	sessionRow
	// newAgentRow is an agent opened from den that has no first prompt yet, so no saved session.
	newAgentRow
)

// listRow is one line of the panel tree. A saved session that is open in a pane has both session and open set.
// A contextFileRow names its guide, or none for the repo's context.md.
type listRow struct {
	kind    rowKind
	folder  *store.Folder
	session *store.Session
	open    *openAgent
	guide   string
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

// isMovable tells whether the row is a session or new agent, which can be dragged into a folder.
func (row listRow) isMovable() bool {
	return row.kind == sessionRow || row.kind == newAgentRow
}

// rows lists the tree: the repo's context and guides, the Inbox, the folders by name, and the archive when it holds anything.
// While filtering, only matching sessions show, under every folder that holds one.
func (currentModel model) rows() []listRow {
	filterText := strings.ToLower(currentModel.filter.Value())
	if filterText != "" {
		return currentModel.filteredRows(filterText)
	}
	return currentModel.treeRows(false)
}

// treeRows builds the whole tree, opening every folder when expandAll is set.
func (currentModel model) treeRows(expandAll bool) []listRow {
	rows := []listRow{{kind: repositoryRow}}
	if currentModel.repositoryExpanded && !expandAll {
		rows = append(rows, listRow{kind: contextFileRow, depth: 1})
		for _, guide := range store.Guides {
			rows = append(rows, listRow{kind: contextFileRow, guide: guide, depth: 1})
		}
	}
	rows = append(rows, listRow{kind: inboxRow})
	if !currentModel.inboxCollapsed || expandAll {
		rows = append(rows, currentModel.rowsInFolder("", 1, expandAll)...)
	}
	for _, folder := range currentModel.subfolders("") {
		rows = append(rows, listRow{kind: folderRow, folder: folder})
		if !folder.Collapsed || expandAll {
			rows = append(rows, currentModel.rowsInFolder(folder.Id, 1, expandAll)...)
		}
	}
	if currentModel.repository.ArchivedCount() > 0 {
		rows = append(rows, listRow{kind: archiveRow})
		if currentModel.archiveExpanded || expandAll {
			for _, session := range currentModel.archivedSessions() {
				rows = append(rows, listRow{kind: sessionRow, session: session, open: currentModel.openAgentFor(session.SessionId), depth: 1})
			}
		}
	}
	return rows
}

// filteredRows keeps the sessions whose title, branch or folder contains the text, and the folders above them.
func (currentModel model) filteredRows(filterText string) []listRow {
	allRows := currentModel.treeRows(true)
	keep := make([]bool, len(allRows))
	for index, row := range allRows {
		if row.kind == sessionRow {
			searchable := strings.ToLower(row.session.Title + " " + row.session.Branch + " " + currentModel.repository.FolderName(row.session.FolderId))
			keep[index] = strings.Contains(searchable, filterText)
		}
	}
	// A folder stays when anything below it, up to the next row at its depth or above, stays.
	for index := len(allRows) - 1; index >= 0; index-- {
		if allRows[index].kind == sessionRow || allRows[index].kind == repositoryRow {
			continue
		}
		for below := index + 1; below < len(allRows) && allRows[below].depth > allRows[index].depth; below++ {
			if keep[below] {
				keep[index] = true
				break
			}
		}
	}
	var rows []listRow
	for index, row := range allRows {
		if keep[index] {
			rows = append(rows, row)
		}
	}
	return rows
}

// rowsInFolder lists one folder's contents: new agents first, then subfolders by name with their contents
// unless collapsed, then sessions newest first. The top level's subfolders are listed by rows, beside the Inbox.
func (currentModel model) rowsInFolder(folderId string, depth int, expandAll bool) []listRow {
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
			if !folder.Collapsed || expandAll {
				rows = append(rows, currentModel.rowsInFolder(folder.Id, depth+1, expandAll)...)
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
	if selected, exists := currentModel.selectedRow(); exists && selected.kind != archiveRow && selected.kind != repositoryRow && selected.kind != contextFileRow {
		if selected.kind == sessionRow && selected.session.Archived {
			return ""
		}
		return selected.folderId()
	}
	return ""
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
	case contextFileRow:
		guide := row.guide
		return func(candidate listRow) bool { return candidate.kind == contextFileRow && candidate.guide == guide }
	}
	kind := row.kind
	return func(candidate listRow) bool { return candidate.kind == kind }
}
