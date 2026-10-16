package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/store"
)

const folderPickerWidth = 36

// folderPicker is the overlay that asks which folder something goes to.
type folderPicker struct {
	title     string
	folderIds []string
	cursor    int
	onPick    func(currentModel *model, folderId string)
}

// pickedFolder is a folder in the picker, with its depth in the tree.
type pickedFolder struct {
	folder *store.Folder
	depth  int
}

func (currentModel *model) pickFolder(title string, onPick func(currentModel *model, folderId string)) {
	folderIds := []string{""}
	for _, entry := range currentModel.allFolders() {
		folderIds = append(folderIds, entry.folder.Id)
	}
	currentModel.picker = &folderPicker{title: title, folderIds: folderIds, onPick: onPick}
}

// askWhereToMove opens the folder picker for the sessions.
func (currentModel *model) askWhereToMove(sessionIds []string) {
	title := "Move " + countedSessions(len(sessionIds)) + " to"
	if len(sessionIds) == 1 {
		title = "Move " + truncate(currentModel.repository.FindSession(sessionIds[0]).Title, 24) + " to"
	}
	currentModel.pickFolder(title, func(currentModel *model, folderId string) {
		currentModel.moveSessionsToFolder(sessionIds, folderId)
		currentModel.selected = map[string]bool{}
		currentModel.clampCursor()
	})
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
		left := []segment{{text: prefix + currentModel.repository.FolderName(folderId), style: theme.text}}
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
