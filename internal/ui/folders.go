package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/tmux"
)

// textPrompt asks for one line of text and hands it to submit on enter.
// With inlineRow set, the input replaces that row in the tree, as a rename in place.
type textPrompt struct {
	question  string
	input     textinput.Model
	submit    func(currentModel *model, value string)
	inlineRow func(listRow) bool
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
		switch {
		case key.Matches(keyPress, currentModel.keys.save):
			value := strings.TrimSpace(currentModel.prompt.input.Value())
			submit := currentModel.prompt.submit
			currentModel.prompt = nil
			if value != "" {
				submit(&currentModel, value)
			}
			return currentModel, nil
		case key.Matches(keyPress, currentModel.keys.back):
			currentModel.prompt = nil
			return currentModel, nil
		}
	}
	var command tea.Cmd
	currentModel.prompt.input, command = currentModel.prompt.input.Update(message)
	return currentModel, command
}

func (currentModel *model) askForNewFolder(parentId string) tea.Cmd {
	question := "New folder"
	if parentId != "" {
		question = "New folder in " + currentModel.repository.FolderName(parentId)
	}
	return currentModel.askForText(question, "", func(currentModel *model, name string) {
		folder := currentModel.repository.AddFolder(name, parentId)
		currentModel.expandFolder(parentId)
		currentModel.saveRepository()
		folderId := folder.Id
		currentModel.cursorTo(func(row listRow) bool { return row.kind == folderRow && row.folder.Id == folderId })
		currentModel.setStatus("made " + name + " · e to give it context")
	})
}

// askForRename renames a folder or session; a manual rename pins a session's title so it stops updating by itself.
func (currentModel *model) askForRename(row listRow, inline bool) tea.Cmd {
	var command tea.Cmd
	switch row.kind {
	case folderRow:
		folderId := row.folder.Id
		command = currentModel.askForText("Rename folder", row.folder.Name, func(currentModel *model, name string) {
			currentModel.repository.FindFolder(folderId).Name = name
			currentModel.saveRepository()
			currentModel.updatePaneFolderNames(folderId, name)
		})
	case sessionRow:
		sessionId := row.session.SessionId
		command = currentModel.askForText("Rename session", row.session.Title, func(currentModel *model, title string) {
			session := currentModel.repository.FindSession(sessionId)
			session.Title, session.TitlePinned = title, true
			if open := currentModel.openAgentFor(sessionId); open != nil {
				currentModel.reportError(tmux.SetPaneOption(open.paneId, paneTitleOption, title))
			}
			currentModel.saveRepository()
		})
	case newAgentRow:
		currentModel.setStatus("send a first prompt before renaming")
		return nil
	default:
		return nil
	}
	if inline {
		currentModel.prompt.inlineRow = sameItemAs(row)
	}
	return command
}

func (currentModel *model) updatePaneFolderNames(folderId string, name string) {
	for _, open := range currentModel.openAgents {
		if open.folderId == folderId {
			currentModel.reportError(tmux.SetPaneOption(open.paneId, paneFolderNameOption, name))
		}
	}
}

func (currentModel *model) toggleFolder(folderId string) {
	folder := currentModel.repository.FindFolder(folderId)
	folder.Collapsed = !folder.Collapsed
	currentModel.saveRepository()
}

// expandFolder opens the folder and its parents, so whatever just went into it is visible.
func (currentModel *model) expandFolder(folderId string) {
	if folderId == "" {
		currentModel.inboxCollapsed = false
	}
	for folder := currentModel.repository.FindFolder(folderId); folder != nil; folder = currentModel.repository.FindFolder(folder.ParentId) {
		folder.Collapsed = false
	}
}

// moveRowToFolder puts a session, or a new agent without a first prompt yet, into a folder and keeps it selected.
func (currentModel *model) moveRowToFolder(row listRow, folderId string) {
	if !row.isMovable() || (row.folderId() == folderId && !(row.session != nil && row.session.Archived)) {
		return
	}
	currentModel.expandFolder(folderId)
	if row.session != nil {
		currentModel.repository.MoveByHand(row.session.SessionId, folderId)
	} else {
		currentModel.reportError(tmux.SetPaneOption(row.open.paneId, paneFolderOption, folderId))
		row.open.folderId = folderId
	}
	matchesRow := sameItemAs(row)
	currentModel.saveRepository()
	currentModel.updatePaneFolders()
	currentModel.cursorTo(matchesRow)
}

// moveSessionToFolder files a session where the user picked. Taking it out of a folder counts as rejecting that folder.
func (currentModel *model) moveSessionToFolder(sessionId string, folderId string) {
	fromFolderId := currentModel.repository.FindSession(sessionId).FolderId
	currentModel.repository.MoveByHand(sessionId, folderId)
	currentModel.repository.RememberMovedOut(sessionId, fromFolderId)
	currentModel.expandFolder(folderId)
	currentModel.saveRepository()
	currentModel.updatePaneFolders()
	currentModel.cursorTo(func(row listRow) bool { return row.kind == sessionRow && row.session.SessionId == sessionId })
	currentModel.setStatus("moved to " + currentModel.repository.FolderName(folderId))
}

// moveFolder puts a folder, with everything in it, under another folder or at the top level, and keeps it selected.
func (currentModel *model) moveFolder(folderId string, parentId string) {
	if currentModel.reportError(currentModel.repository.MoveFolder(folderId, parentId)) {
		return
	}
	currentModel.expandFolder(parentId)
	currentModel.saveRepository()
	currentModel.cursorTo(func(row listRow) bool { return row.kind == folderRow && row.folder.Id == folderId })
	if parentId == "" {
		currentModel.setStatus("moved to the top level")
	} else {
		currentModel.setStatus("moved into " + currentModel.repository.FolderName(parentId))
	}
}

// updateMouse selects the clicked row and moves a session dragged onto another row into that row's folder.
// A click on the ✦ badge opens the review queue.
func (currentModel model) updateMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := message.Mouse()
	if mouse.Button != tea.MouseLeft {
		return currentModel, nil
	}
	if _, isClick := message.(tea.MouseClickMsg); isClick && mouse.Y == 0 && mouse.X >= currentModel.panelWidth()-4 {
		currentModel.openReview()
		return currentModel, nil
	}
	rows := currentModel.rows()
	rowIndex, isOverRow := currentModel.rowAtLine(mouse.Y)
	isOverRow = isOverRow && mouse.X < currentModel.treeWidth()
	var command tea.Cmd
	switch message.(type) {
	case tea.MouseClickMsg:
		currentModel.dragging = nil
		if isOverRow {
			if currentModel.listCursor == rowIndex && !rows[rowIndex].isMovable() {
				command = currentModel.openRow(rows[rowIndex])
			}
			currentModel.listCursor = rowIndex
			if rows[rowIndex].isMovable() {
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
		target := rows[rowIndex]
		sourceIndex := slices.IndexFunc(rows, dragging)
		isIntoArchive := target.kind == archiveRow || (target.kind == sessionRow && target.session.Archived)
		if isIntoArchive {
			if sourceIndex >= 0 && rows[sourceIndex].kind == sessionRow && !rows[sourceIndex].session.Archived {
				currentModel.setArchived([]string{rows[sourceIndex].session.SessionId}, true)
			}
			return currentModel, nil
		}
		if target.kind != inboxRow && target.kind != folderRow && !target.isMovable() {
			return currentModel, nil
		}
		if sourceIndex >= 0 && sourceIndex != rowIndex {
			source := rows[sourceIndex]
			fromFolderId := source.folderId()
			// Saving sorts the sessions, so the row's pointer can't be trusted after the move.
			movedSessionId := ""
			if source.kind == sessionRow {
				movedSessionId = source.session.SessionId
			}
			currentModel.moveRowToFolder(source, target.folderId())
			if movedSessionId != "" {
				currentModel.repository.RememberMovedOut(movedSessionId, fromFolderId)
				currentModel.saveRepository()
			}
		}
	}
	return currentModel, command
}
