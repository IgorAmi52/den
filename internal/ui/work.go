package ui

import (
	"fmt"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

// updateWork handles the narrow panel beside the agents.
func (currentModel model) updateWork(message tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	var updated tea.Model = currentModel
	switch typedMessage := message.(type) {
	case tea.KeyPressMsg:
		updated, command = currentModel.updateWorkKey(typedMessage)
	case tea.MouseMsg:
		if currentModel.prompt == nil && currentModel.confirming == nil && !currentModel.picking {
			updated, command = currentModel.updateMouse(typedMessage)
		}
	default:
		if currentModel.prompt != nil {
			updated, command = currentModel.updatePrompt(message)
		}
	}
	afterUpdate := updated.(model)
	afterUpdate.scrollToCursor()
	return afterUpdate, command
}

func (currentModel model) updateWorkKey(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if currentModel.prompt != nil {
		return currentModel.updatePrompt(keyPress)
	}
	if currentModel.confirming != nil {
		return currentModel.updateConfirmation(keyPress)
	}
	if currentModel.picking {
		return currentModel.updatePicker(keyPress)
	}
	if currentModel.grabbed != nil {
		return currentModel.updateGrab(keyPress)
	}
	currentModel.clearStatus()
	keys := currentModel.keys
	selected, exists := currentModel.selectedRow()
	switch {
	case key.Matches(keyPress, keys.quit):
		currentModel.askToQuit()
	case key.Matches(keyPress, keys.up):
		currentModel.listCursor = max(currentModel.listCursor-1, 0)
	case key.Matches(keyPress, keys.down):
		currentModel.listCursor = min(currentModel.listCursor+1, len(currentModel.rows())-1)
	case key.Matches(keyPress, keys.newSession):
		return currentModel, currentModel.startNewSession(currentModel.folderForNewItems(), false)
	case key.Matches(keyPress, keys.newWithout):
		return currentModel, currentModel.startNewSession(currentModel.folderForNewItems(), true)
	case key.Matches(keyPress, keys.jump):
		currentModel.jumpToAgent(keyPress.String())
	case key.Matches(keyPress, keys.organize):
		currentModel.toggleOrganize()
	case key.Matches(keyPress, keys.review):
		currentModel.openReview()
	case key.Matches(keyPress, keys.chat):
		return currentModel, currentModel.openChat(nil)
	case key.Matches(keyPress, keys.help):
		currentModel.openScreen(helpScreen)
	case key.Matches(keyPress, keys.newFolder):
		return currentModel, currentModel.askForNewFolder(currentModel.folderForNewItems())
	case !exists:
	case key.Matches(keyPress, keys.open):
		currentModel.openRow(selected)
	case key.Matches(keyPress, keys.accept):
		if selected.kind == sessionRow {
			currentModel.acceptFilingFor(selected.session.SessionId)
		}
	case key.Matches(keyPress, keys.dismiss):
		if selected.kind == sessionRow {
			currentModel.rejectFilingFor(selected.session.SessionId)
		}
	case key.Matches(keyPress, keys.move):
		currentModel.startGrab()
	// Shortcuts for terminals that send Shift+arrows; Terminal.app sends them as plain arrows, so grab mode is the main way.
	case keyPress.String() == "shift+down" || keyPress.String() == "J":
		currentModel.moveSelected(1)
	case keyPress.String() == "shift+up" || keyPress.String() == "K":
		currentModel.moveSelected(-1)
	case key.Matches(keyPress, keys.rename):
		return currentModel, currentModel.askForRename(selected, true)
	case key.Matches(keyPress, keys.archive):
		if selected.kind == sessionRow {
			currentModel.setArchived([]string{selected.session.SessionId}, !selected.session.Archived)
		}
	case key.Matches(keyPress, keys.editContext):
		return currentModel, currentModel.editContext(selected.folderId())
	case key.Matches(keyPress, keys.delete):
		currentModel.askToDelete(selected)
	}
	return currentModel, nil
}

// openRow opens or closes a folder, focuses an open agent, or resumes a saved session.
func (currentModel *model) openRow(row listRow) {
	switch row.kind {
	case inboxRow:
		currentModel.inboxCollapsed = !currentModel.inboxCollapsed
	case archiveRow:
		currentModel.archiveExpanded = !currentModel.archiveExpanded
	case folderRow:
		currentModel.toggleFolder(row.folder.Id)
	case newAgentRow:
		currentModel.focusPane(row.open.paneId)
	case sessionRow:
		if row.open != nil {
			currentModel.focusPane(row.open.paneId)
		} else {
			currentModel.resumeSession(row.session.SessionId)
		}
	}
}

// startNewSession opens an agent in the folder, asking which agent first when more than one is installed.
func (currentModel *model) startNewSession(folderId string, withoutContext bool) tea.Cmd {
	if len(currentModel.openAgents) >= maximumAgents {
		currentModel.setError(fmt.Sprintf("%d agents open, close one first", maximumAgents))
		return nil
	}
	currentModel.newSessionFolderId = folderId
	currentModel.newSessionWithoutContext = withoutContext
	if len(currentModel.installedAgents) == 1 {
		currentModel.openAgent(currentModel.installedAgents[0])
		return nil
	}
	currentModel.picking = true
	currentModel.pickerCursor = 0
	return nil
}

func (currentModel model) updatePicker(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keys := currentModel.keys
	switch {
	case key.Matches(keyPress, keys.pickPrevious):
		currentModel.pickerCursor = max(currentModel.pickerCursor-1, 0)
	case key.Matches(keyPress, keys.pickNext):
		currentModel.pickerCursor = min(currentModel.pickerCursor+1, len(currentModel.installedAgents)-1)
	case key.Matches(keyPress, keys.open):
		currentModel.picking = false
		currentModel.openAgent(currentModel.installedAgents[currentModel.pickerCursor])
	case key.Matches(keyPress, keys.back), key.Matches(keyPress, keys.quit):
		currentModel.picking = false
	}
	return currentModel, nil
}

func (currentModel model) updateConfirmation(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirming := currentModel.confirming
	currentModel.confirming = nil
	if !key.Matches(keyPress, currentModel.keys.confirmYes) {
		return currentModel, nil
	}
	return currentModel, confirming.accept(&currentModel)
}

func (currentModel *model) askToQuit() {
	currentModel.confirming = &confirmation{question: "Quit den?", detail: "every agent in it closes", accept: func(currentModel *model) tea.Cmd {
		if currentModel.reportError(tmux.KillWindow(currentModel.windowId)) {
			return nil
		}
		return tea.Quit
	}}
}

// askToDelete confirms in the footer. Deleting a session forgets it in den and closes its pane;
// the agent keeps its own history. Deleting a folder moves what's inside up a level.
func (currentModel *model) askToDelete(row listRow) {
	switch row.kind {
	case folderRow:
		folderId := row.folder.Id
		destination := currentModel.repository.FolderName(row.folder.ParentId)
		currentModel.confirming = &confirmation{
			question: "Delete " + row.folder.Name + "?",
			detail:   "what's in it moves to " + destination,
			accept: func(currentModel *model) tea.Cmd {
				parentId := currentModel.repository.FindFolder(folderId).ParentId
				currentModel.repository.DeleteFolder(folderId)
				for _, open := range currentModel.openAgents {
					if open.folderId == folderId {
						currentModel.reportError(tmux.SetPaneOption(open.paneId, paneFolderOption, parentId))
						currentModel.reportError(tmux.SetPaneOption(open.paneId, paneFolderNameOption, currentModel.repository.FolderName(parentId)))
					}
				}
				currentModel.reportError(currentModel.refreshAgents())
				currentModel.saveRepository()
				currentModel.clampCursor()
				return nil
			},
		}
	case newAgentRow:
		paneId := row.open.paneId
		currentModel.confirming = &confirmation{question: "Close this agent?", detail: "nothing was saved yet", accept: func(currentModel *model) tea.Cmd {
			currentModel.reportError(tmux.KillPane(paneId))
			currentModel.reportError(currentModel.refreshAgents())
			currentModel.clampCursor()
			return nil
		}}
	case sessionRow:
		currentModel.askToDeleteSessions([]string{row.session.SessionId})
	}
}

func (currentModel *model) askToDeleteSessions(sessionIds []string) {
	question, detail := fmt.Sprintf("Delete %d sessions?", len(sessionIds)), "they stay in the agents' history"
	if len(sessionIds) == 1 {
		question, detail = "Delete this session?", "it stays in "+currentModel.repository.FindSession(sessionIds[0]).Tool+"'s history"
	}
	currentModel.confirming = &confirmation{question: question, detail: detail, accept: func(currentModel *model) tea.Cmd {
		for _, sessionId := range sessionIds {
			if open := currentModel.openAgentFor(sessionId); open != nil {
				currentModel.reportError(tmux.KillPane(open.paneId))
			}
			currentModel.repository.ForgetSessionSuggestions(sessionId)
		}
		currentModel.repository.Sessions = slices.DeleteFunc(currentModel.repository.Sessions, func(saved store.Session) bool {
			return slices.Contains(sessionIds, saved.SessionId)
		})
		currentModel.saveRepository()
		currentModel.reportError(currentModel.refreshAgents())
		currentModel.clampCursor()
		currentModel.organize.clearSelection()
		return nil
	}}
}

// setArchived archives sessions, or brings them back. Archived sessions stay resumable.
func (currentModel *model) setArchived(sessionIds []string, archived bool) {
	for _, sessionId := range sessionIds {
		if session := currentModel.repository.FindSession(sessionId); session != nil {
			session.Archived = archived
			currentModel.repository.ForgetSessionSuggestions(sessionId)
		}
	}
	currentModel.saveRepository()
	currentModel.clampCursor()
	if archived {
		currentModel.setStatus(fmt.Sprintf("archived %s, still resumable", countedSessions(len(sessionIds))))
	} else {
		currentModel.setStatus(fmt.Sprintf("brought back %s", countedSessions(len(sessionIds))))
	}
}

// acceptFilingFor accepts the suggestion shown as ghost text on the session's row.
func (currentModel *model) acceptFilingFor(sessionId string) {
	suggestion := currentModel.repository.PendingFor(sessionId)
	if suggestion == nil {
		currentModel.setStatus("no suggestion here · m to move it yourself")
		return
	}
	currentModel.acceptSuggestion(suggestion.Id)
	currentModel.cursorTo(func(row listRow) bool {
		return row.kind == sessionRow && row.session.SessionId == sessionId
	})
}

func (currentModel *model) rejectFilingFor(sessionId string) {
	if suggestion := currentModel.repository.PendingFor(sessionId); suggestion != nil {
		currentModel.rejectSuggestion(suggestion.Id)
	}
}

// acceptSuggestion carries out a suggestion and says what happened in the footer.
func (currentModel *model) acceptSuggestion(suggestionId string) {
	suggestion := currentModel.repository.FindSuggestion(suggestionId)
	if suggestion == nil {
		return
	}
	done := currentModel.describeDone(*suggestion)
	destinationFolderId := suggestion.FolderId
	if currentModel.reportError(currentModel.repository.Accept(suggestionId)) {
		return
	}
	currentModel.expandFolder(destinationFolderId)
	currentModel.updatePaneFolders()
	currentModel.saveRepository()
	currentModel.refreshContextTokens()
	currentModel.setStatus(done)
}

func (currentModel *model) rejectSuggestion(suggestionId string) {
	currentModel.repository.Reject(suggestionId)
	currentModel.saveRepository()
	currentModel.setStatus("rejected · den won't offer that again")
}

func (currentModel model) describeDone(suggestion store.Suggestion) string {
	switch suggestion.Kind {
	case store.SuggestMove:
		return fmt.Sprintf("filed %s in %s", countedSessions(len(suggestion.SessionIds)), currentModel.repository.FolderName(suggestion.FolderId))
	case store.SuggestSubfolder:
		return "made ⎇ " + suggestion.FolderName
	case store.SuggestCleanup:
		return fmt.Sprintf("archived %s, still resumable", countedSessions(len(suggestion.SessionIds)))
	}
	return "updated " + currentModel.contextFileName(suggestion)
}

// updatePaneFolders keeps each open pane's border naming the folder its session is in now.
func (currentModel *model) updatePaneFolders() {
	for _, open := range currentModel.openAgents {
		session := currentModel.repository.FindSession(open.sessionId)
		if session == nil || session.FolderId == open.folderId {
			continue
		}
		currentModel.reportError(tmux.SetPaneOption(open.paneId, paneFolderOption, session.FolderId))
		currentModel.reportError(tmux.SetPaneOption(open.paneId, paneFolderNameOption, currentModel.repository.FolderName(session.FolderId)))
	}
	currentModel.reportError(currentModel.refreshAgents())
}

func countedSessions(count int) string {
	if count == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", count)
}
