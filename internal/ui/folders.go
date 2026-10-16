package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/tmux"
)

// textPrompt asks for one line of text in the panel and hands it to submit on enter.
type textPrompt struct {
	question string
	input    textinput.Model
	submit   func(currentModel *model, value string)
}

func (currentModel *model) askForText(question string, initialValue string, submit func(currentModel *model, value string)) tea.Cmd {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 200
	input.SetValue(initialValue)
	focusCommand := input.Focus()
	currentModel.prompt = &textPrompt{question: question, input: input, submit: submit}
	return focusCommand
}

func (currentModel model) updatePrompt(message tea.Msg) (tea.Model, tea.Cmd) {
	if keyPress, isKeyPress := message.(tea.KeyPressMsg); isKeyPress {
		switch keyPress.String() {
		case "enter":
			value := strings.TrimSpace(currentModel.prompt.input.Value())
			submit := currentModel.prompt.submit
			currentModel.prompt = nil
			if value != "" {
				submit(&currentModel, value)
			}
			return currentModel, nil
		case "esc":
			currentModel.prompt = nil
			return currentModel, nil
		}
	}
	var command tea.Cmd
	currentModel.prompt.input, command = currentModel.prompt.input.Update(message)
	return currentModel, command
}

func (currentModel *model) askForNewFolder() tea.Cmd {
	parentId := currentModel.folderForNewItems()
	return currentModel.askForText("New folder name", "", func(currentModel *model, name string) {
		folder := currentModel.repository.AddFolder(name, parentId)
		currentModel.expandFolder(parentId)
		currentModel.saveRepository()
		currentModel.cursorTo(sameItemAs(listRow{folder: &folder}))
	})
}

func (currentModel *model) askForRename() tea.Cmd {
	selected, exists := currentModel.selectedRow()
	if !exists {
		return nil
	}
	if selected.folder != nil {
		folderId := selected.folder.Id
		return currentModel.askForText("Rename folder", selected.folder.Name, func(currentModel *model, name string) {
			currentModel.repository.FindFolder(folderId).Name = name
			currentModel.saveRepository()
			currentModel.cursorTo(sameItemAs(selected))
		})
	}
	if selected.session == nil {
		currentModel.statusMessage = "send a first prompt before renaming"
		return nil
	}
	sessionId := selected.session.SessionId
	return currentModel.askForText("Rename session", selected.session.Title, func(currentModel *model, title string) {
		currentModel.repository.FindSession(sessionId).Title = title
		if open := currentModel.openAgentFor(sessionId); open != nil {
			currentModel.reportError(tmux.SetPaneOption(open.paneId, paneTitleOption, title))
		}
		currentModel.saveRepository()
	})
}

func (currentModel *model) toggleFolder(folderId string) {
	folder := currentModel.repository.FindFolder(folderId)
	folder.Collapsed = !folder.Collapsed
	currentModel.saveRepository()
}

func (currentModel *model) expandFolder(folderId string) {
	if folder := currentModel.repository.FindFolder(folderId); folder != nil {
		folder.Collapsed = false
	}
}

// moveSelected moves the selected session into the next or previous folder on screen.
func (currentModel *model) moveSelected(direction int) {
	selected, exists := currentModel.selectedRow()
	if !exists || selected.folder != nil {
		return
	}
	targets := currentModel.moveTargets()
	currentIndex := slices.Index(targets, selected.folderId())
	targetIndex := currentIndex + direction
	if currentIndex < 0 || targetIndex < 0 || targetIndex >= len(targets) {
		return
	}
	currentModel.moveRowToFolder(selected, targets[targetIndex])
}

// moveRowToFolder puts a session, or a new agent without a first prompt yet, into a folder and keeps it selected.
func (currentModel *model) moveRowToFolder(row listRow, folderId string) {
	if row.folder != nil || row.folderId() == folderId {
		return
	}
	currentModel.expandFolder(folderId)
	if row.session != nil {
		row.session.FolderId = folderId
	} else {
		currentModel.reportError(tmux.SetPaneOption(row.open.paneId, paneFolderOption, folderId))
		row.open.folderId = folderId
	}
	matchesRow := sameItemAs(row)
	currentModel.saveRepository()
	currentModel.cursorTo(matchesRow)
}

// startGrab picks up the selected session, so plain arrows carry it through the folders until it is dropped.
func (currentModel *model) startGrab() {
	selected, exists := currentModel.selectedRow()
	if !exists || selected.folder != nil {
		return
	}
	currentModel.grabbed = sameItemAs(selected)
	currentModel.grabbedFromFolderId = selected.folderId()
}

func (currentModel model) updateGrab(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k", "shift+up", "K":
		currentModel.moveSelected(-1)
	case "down", "j", "shift+down", "J":
		currentModel.moveSelected(1)
	case "esc":
		if selected, exists := currentModel.selectedRow(); exists {
			currentModel.moveRowToFolder(selected, currentModel.grabbedFromFolderId)
		}
		currentModel.grabbed = nil
	case "enter", "space", "m":
		currentModel.grabbed = nil
	}
	return currentModel, nil
}

// updateMouse selects the clicked row and moves a session dragged onto another row into that row's folder.
func (currentModel model) updateMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := message.Mouse()
	rows := currentModel.rows()
	rowIndex := mouse.Y - listTopLine
	if mouse.Button != tea.MouseLeft || currentModel.grabbed != nil {
		return currentModel, nil
	}
	isOverRow := rowIndex >= 0 && rowIndex < len(rows)
	switch message.(type) {
	case tea.MouseClickMsg:
		currentModel.dragging = nil
		if isOverRow {
			currentModel.listCursor = rowIndex
			if rows[rowIndex].folder == nil {
				currentModel.dragging = sameItemAs(rows[rowIndex])
			}
		}
	case tea.MouseMotionMsg:
		currentModel.dropTargetRow = -1
		if currentModel.dragging != nil && isOverRow {
			currentModel.dropTargetRow = rowIndex
		}
	case tea.MouseReleaseMsg:
		dragging := currentModel.dragging
		currentModel.dragging, currentModel.dropTargetRow = nil, -1
		if dragging == nil || !isOverRow {
			return currentModel, nil
		}
		if sourceIndex := slices.IndexFunc(rows, dragging); sourceIndex >= 0 && sourceIndex != rowIndex {
			currentModel.moveRowToFolder(rows[sourceIndex], rows[rowIndex].folderId())
		}
	}
	return currentModel, nil
}

// sameItemAs matches the row showing the same folder, session or pane, even after the list was rebuilt.
func sameItemAs(row listRow) func(listRow) bool {
	if row.folder != nil {
		folderId := row.folder.Id
		return func(candidate listRow) bool { return candidate.folder != nil && candidate.folder.Id == folderId }
	}
	if row.session != nil {
		sessionId := row.session.SessionId
		return func(candidate listRow) bool {
			return candidate.session != nil && candidate.session.SessionId == sessionId
		}
	}
	paneId := row.open.paneId
	return func(candidate listRow) bool { return candidate.open != nil && candidate.open.paneId == paneId }
}
