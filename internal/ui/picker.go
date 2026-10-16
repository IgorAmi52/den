package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/store"
)

const folderPickerWidth = 36

// archiveTarget stands for the archive in the picker; folder ids are hex, so it can't clash with one.
const archiveTarget = "archive"

// folderPicker is the overlay that asks which folder something goes to.
// topLevelLabel names the top level: the Inbox for sessions, the top of the tree for folders.
type folderPicker struct {
	title         string
	topLevelLabel string
	folderIds     []string
	cursor        int
	onPick        func(currentModel *model, folderId string)
}

// pickedFolder is a folder in the picker, with its depth in the tree.
type pickedFolder struct {
	folder *store.Folder
	depth  int
}

// pickFolder opens the picker with its cursor on currentFolderId. The folder being moved, if any,
// is left out with its subfolders, since a folder can't go inside itself.
// With withArchive, the archive comes last, for sessions.
func (currentModel *model) pickFolder(title string, topLevelLabel string, currentFolderId string, movingFolderId string, withArchive bool, onPick func(currentModel *model, folderId string)) {
	picker := &folderPicker{title: title, topLevelLabel: topLevelLabel, folderIds: []string{""}, onPick: onPick}
	for _, entry := range currentModel.allFolders() {
		isInsideMovingFolder := false
		for ancestor := entry.folder; ancestor != nil; ancestor = currentModel.repository.FindFolder(ancestor.ParentId) {
			isInsideMovingFolder = isInsideMovingFolder || ancestor.Id == movingFolderId
		}
		if !isInsideMovingFolder {
			picker.folderIds = append(picker.folderIds, entry.folder.Id)
		}
	}
	if withArchive {
		picker.folderIds = append(picker.folderIds, archiveTarget)
	}
	picker.cursor = max(slices.Index(picker.folderIds, currentFolderId), 0)
	currentModel.picker = picker
}

// askWhereToMove opens the picker for the selected session, new agent or folder.
func (currentModel *model) askWhereToMove(row listRow) {
	switch row.kind {
	case sessionRow:
		sessionId := row.session.SessionId
		currentFolderId := row.session.FolderId
		if row.session.Archived {
			currentFolderId = archiveTarget
		}
		currentModel.pickFolder("Move "+truncate(row.session.Title, 24)+" to", "Inbox", currentFolderId, "", true, func(currentModel *model, folderId string) {
			if folderId == archiveTarget {
				currentModel.setArchived([]string{sessionId}, true)
				return
			}
			currentModel.moveSessionToFolder(sessionId, folderId)
		})
	case newAgentRow:
		paneId := row.open.paneId
		currentModel.pickFolder("Move new "+row.open.tool+" to", "Inbox", row.open.folderId, "", false, func(currentModel *model, folderId string) {
			for agentIndex := range currentModel.openAgents {
				if currentModel.openAgents[agentIndex].paneId == paneId {
					currentModel.moveRowToFolder(listRow{kind: newAgentRow, open: &currentModel.openAgents[agentIndex]}, folderId)
				}
			}
		})
	case folderRow:
		folderId := row.folder.Id
		currentModel.pickFolder("Move "+truncate(row.folder.Name, 24)+" into", "top level", row.folder.ParentId, folderId, false, func(currentModel *model, parentId string) {
			currentModel.moveFolder(folderId, parentId)
		})
	default:
		currentModel.setStatus("only sessions and folders move")
	}
}

// allFolders lists every folder in tree order, collapsed or not.
func (currentModel model) allFolders() []pickedFolder {
	var folders []pickedFolder
	var addFolders func(parentId string, depth int)
	addFolders = func(parentId string, depth int) {
		for _, folder := range currentModel.subfolders(parentId) {
			folders = append(folders, pickedFolder{folder: folder, depth: depth})
			addFolders(folder.Id, depth+1)
		}
	}
	addFolders("", 0)
	return folders
}

func (currentModel model) updateFolderPicker(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	picker := currentModel.picker
	keys := currentModel.keys
	switch {
	case key.Matches(keyPress, keys.up):
		picker.cursor = max(picker.cursor-1, 0)
	case key.Matches(keyPress, keys.down):
		picker.cursor = min(picker.cursor+1, len(picker.folderIds)-1)
	case key.Matches(keyPress, keys.open):
		currentModel.picker = nil
		picker.onPick(&currentModel, picker.folderIds[picker.cursor])
	case key.Matches(keyPress, keys.back), key.Matches(keyPress, keys.quit):
		currentModel.picker = nil
	}
	return currentModel, nil
}

func (currentModel model) folderPickerBox() string {
	picker := currentModel.picker
	theme := currentModel.theme
	boxWidth := min(folderPickerWidth, currentModel.width-4)
	lines := []string{theme.bold.Render(truncate(picker.title, boxWidth)), ""}
	depthById := map[string]int{"": 0}
	for _, entry := range currentModel.allFolders() {
		depthById[entry.folder.Id] = entry.depth
	}
	for index, folderId := range picker.folderIds {
		prefix := strings.Repeat("  ", depthById[folderId])
		if folder := currentModel.repository.FindFolder(folderId); folder != nil && folder.Branch != "" {
			prefix += "⎇ "
		}
		left := []segment{{text: prefix + currentModel.repository.FolderName(folderId), style: theme.folder}}
		switch folderId {
		case "":
			left[0].text = picker.topLevelLabel
		case archiveTarget:
			left = []segment{{text: "Archive", style: theme.archive}}
		}
		var right []segment
		if _, hasContext := currentModel.contextTokens[folderId]; hasContext && folderId != "" {
			right = []segment{{text: "◆", style: theme.accent}}
		}
		lines = append(lines, currentModel.layoutRow(left, right, boxWidth, index == picker.cursor))
	}
	lines = append(lines, "", strings.Join(currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.open, "move here"), relabeled(currentModel.keys.back, "cancel")}, boxWidth, 1), ""))
	return theme.overlay.Render(strings.Join(lines, "\n"))
}

// withOverlay draws a box centered over the content.
func (currentModel model) withOverlay(content string, box string) string {
	x := max((currentModel.width-lipgloss.Width(box))/2, 0)
	y := max((currentModel.height-lipgloss.Height(box))/3, 0)
	canvas := lipgloss.NewCanvas(currentModel.width, max(currentModel.height, lipgloss.Height(content)))
	canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(content), lipgloss.NewLayer(box).X(x).Y(y).Z(1)))
	return canvas.Render()
}
