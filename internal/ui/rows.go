package ui

import (
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

// listRow is one line of the panel list: a folder, a saved session, or a just-opened agent
// without a first prompt yet. A saved session that is open in a pane has both session and open set.
type listRow struct {
	folder  *store.Folder
	session *store.Session
	open    *openAgent
	depth   int
}

func (row listRow) folderId() string {
	if row.folder != nil {
		return row.folder.Id
	}
	if row.session != nil {
		return row.session.FolderId
	}
	return row.open.folderId
}

func (currentModel model) rows() []listRow {
	return currentModel.rowsInFolder("", 0)
}

// rowsInFolder lists one level of the tree: new agents first, then subfolders by name with their contents
// unless collapsed, then sessions newest first.
func (currentModel model) rowsInFolder(folderId string, depth int) []listRow {
	var rows []listRow
	for agentIndex := range currentModel.openAgents {
		open := &currentModel.openAgents[agentIndex]
		isUntracked := open.sessionId == "" || currentModel.repository.FindSession(open.sessionId) == nil
		if isUntracked && open.folderId == folderId {
			rows = append(rows, listRow{open: open, depth: depth})
		}
	}
	for _, folder := range currentModel.subfolders(folderId) {
		rows = append(rows, listRow{folder: folder, depth: depth})
		if !folder.Collapsed {
			rows = append(rows, currentModel.rowsInFolder(folder.Id, depth+1)...)
		}
	}
	for sessionIndex := range currentModel.repository.Sessions {
		session := &currentModel.repository.Sessions[sessionIndex]
		if session.FolderId == folderId {
			rows = append(rows, listRow{session: session, open: currentModel.openAgentFor(session.SessionId), depth: depth})
		}
	}
	return rows
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

// folderForNewItems is the folder a new session or folder goes into: the selected folder, or the selected session's folder.
func (currentModel model) folderForNewItems() string {
	if selected, exists := currentModel.selectedRow(); exists {
		return selected.folderId()
	}
	return ""
}

// moveTargets lists where Shift+arrows can move a session, in screen order:
// every visible folder, then the top level, which is shown below the folders.
func (currentModel model) moveTargets() []string {
	var targets []string
	for _, row := range currentModel.rows() {
		if row.folder != nil {
			targets = append(targets, row.folder.Id)
		}
	}
	return append(targets, "")
}

func (currentModel model) openAgentFor(sessionId string) *openAgent {
	for agentIndex := range currentModel.openAgents {
		if currentModel.openAgents[agentIndex].sessionId == sessionId {
			return &currentModel.openAgents[agentIndex]
		}
	}
	return nil
}
