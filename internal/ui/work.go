package ui

import (
	"fmt"
	"path/filepath"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

// updateWork handles the tree, narrow beside the agents or wide with the preview.
func (currentModel model) updateWork(message tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	var updated tea.Model = currentModel
	switch typedMessage := message.(type) {
	case tea.KeyPressMsg:
		updated, command = currentModel.updateWorkKey(typedMessage)
	case tea.MouseMsg:
		if currentModel.prompt == nil && currentModel.confirming == nil && !currentModel.picking && currentModel.picker == nil {
			updated, command = currentModel.updateMouse(typedMessage)
		}
	default:
		if currentModel.contextPane.open && currentModel.contextPane.mode == contextPaneEditing {
			currentModel.contextPane.editor, command = currentModel.contextPane.editor.Update(message)
			updated = currentModel
		} else if currentModel.contextPane.open && currentModel.contextPane.mode == contextPaneAsking {
			currentModel.contextPane.prompt, command = currentModel.contextPane.prompt.Update(message)
			updated = currentModel
		} else if currentModel.prompt != nil {
			updated, command = currentModel.updatePrompt(message)
		} else if currentModel.filtering {
			currentModel.filter, command = currentModel.filter.Update(message)
			updated = currentModel
		}
	}
	afterUpdate := updated.(model)
	afterUpdate.clampCursor()
	afterUpdate.scrollToCursor()
	return afterUpdate, command
}

func (currentModel model) updateWorkKey(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keys := currentModel.keys
	if key.Matches(keyPress, keys.newFromAgent) && currentModel.prompt == nil && !currentModel.filtering {
		currentModel.confirming, currentModel.picker = nil, nil
		return currentModel, currentModel.startNewSession(currentModel.folderForNewItems(), false, "")
	}
	switch {
	case currentModel.prompt != nil:
		return currentModel.updatePrompt(keyPress)
	case currentModel.filtering:
		return currentModel.updateFilter(keyPress)
	case currentModel.confirming != nil:
		return currentModel.updateConfirmation(keyPress)
	case currentModel.picking:
		return currentModel.updatePicker(keyPress)
	case currentModel.picker != nil:
		return currentModel.updateFolderPicker(keyPress)
	case currentModel.contextPane.open:
		return currentModel.updateContextPane(keyPress)
	case currentModel.dock.focused:
		return currentModel.updateDock(keyPress)
	}
	currentModel.clearStatus()
	selected, exists := currentModel.selectedRow()
	switch {
	case key.Matches(keyPress, keys.quit):
		currentModel.askToQuit()
	case key.Matches(keyPress, keys.back):
		currentModel.stepBack()
	case key.Matches(keyPress, keys.up):
		currentModel.listCursor = max(currentModel.listCursor-1, 0)
		currentModel.previewOffset = 0
	case key.Matches(keyPress, keys.down):
		currentModel.listCursor = min(currentModel.listCursor+1, len(currentModel.rows())-1)
		currentModel.previewOffset = 0
	case key.Matches(keyPress, keys.scrollPreview):
		if keyPress.String() == "pgup" || keyPress.String() == "K" {
			currentModel.previewOffset = max(currentModel.previewOffset-5, 0)
		} else {
			currentModel.previewOffset += 5
		}
	case key.Matches(keyPress, keys.newSession):
		return currentModel, currentModel.startNewSession(currentModel.folderForNewItems(), false, "")
	case key.Matches(keyPress, keys.newWithout):
		return currentModel, currentModel.startNewSession(currentModel.folderForNewItems(), true, "")
	case key.Matches(keyPress, keys.jump):
		currentModel.jumpToAgent(keyPress.String())
	case key.Matches(keyPress, keys.wide):
		currentModel.toggleWide()
	case key.Matches(keyPress, keys.chat):
		return currentModel, currentModel.openChatAbout(selected, exists)
	case key.Matches(keyPress, keys.help):
		currentModel.openScreen(helpScreen)
	case key.Matches(keyPress, keys.filter):
		currentModel.filtering = true
		currentModel.listCursor, currentModel.listOffset = 0, 0
		currentModel.filter.SetWidth(currentModel.panelWidth() - 4)
		focusCommand := currentModel.filter.Focus()
		return currentModel, focusCommand
	case key.Matches(keyPress, keys.accept):
		return currentModel, currentModel.focusDock()
	case key.Matches(keyPress, keys.newFolder):
		return currentModel, currentModel.askForNewFolder("")
	case key.Matches(keyPress, keys.newSubfolder):
		if parentId := currentModel.folderForNewItems(); parentId != "" {
			return currentModel, currentModel.askForNewFolder(parentId)
		}
		currentModel.setStatus("select a folder to add a subfolder to it")
	case !exists:
	case keyPress.String() == "right" && hasContextPane(selected):
		currentModel.openContextPane(selected.folderId())
		return currentModel, nil
	case key.Matches(keyPress, keys.open):
		return currentModel, currentModel.openRow(selected)
	case key.Matches(keyPress, keys.move):
		currentModel.askWhereToMove(selected)
	case key.Matches(keyPress, keys.rename):
		return currentModel, currentModel.askForRename(selected, true)
	case key.Matches(keyPress, keys.archive):
		if selected.kind == sessionRow {
			currentModel.setArchived([]string{selected.session.SessionId}, !selected.session.Archived)
		}
	case key.Matches(keyPress, keys.editContext):
		if selected.kind == contextFileRow {
			return currentModel, currentModel.editContextFile(selected.laptopTool)
		}
		return currentModel, currentModel.editContext(selected.folderId())
	case key.Matches(keyPress, keys.delete):
		currentModel.askToDelete(selected)
	}
	return currentModel, nil
}

// stepBack undoes one layer: the filter, then the wide view.
func (currentModel *model) stepBack() {
	switch {
	case currentModel.filter.Value() != "":
		currentModel.filter.SetValue("")
		currentModel.listCursor, currentModel.listOffset = 0, 0
	case currentModel.isWide() && len(currentModel.openAgents) > 0:
		currentModel.toggleWide()
	}
}

func (currentModel model) updateFilter(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(keyPress, currentModel.keys.back):
		currentModel.filtering = false
		currentModel.filter.SetValue("")
		currentModel.filter.Blur()
		currentModel.listCursor, currentModel.listOffset = 0, 0
		return currentModel, nil
	// Only the arrows leave the filter, since j and k are letters someone may be typing.
	case key.Matches(keyPress, currentModel.keys.open), keyPress.String() == "down", keyPress.String() == "up":
		currentModel.filtering = false
		currentModel.filter.Blur()
		currentModel.listCursor = 0
		for rowIndex, row := range currentModel.rows() {
			if row.kind == sessionRow {
				currentModel.listCursor = rowIndex
				break
			}
		}
		return currentModel, nil
	}
	var command tea.Cmd
	currentModel.filter, command = currentModel.filter.Update(keyPress)
	currentModel.listCursor, currentModel.listOffset = 0, 0
	return currentModel, command
}

// openRow opens or closes a folder, focuses an open agent, resumes a saved session, or edits a context file.
func (currentModel *model) openRow(row listRow) tea.Cmd {
	switch row.kind {
	case repositoryRow:
		currentModel.repositoryExpanded = !currentModel.repositoryExpanded
	case contextFileRow:
		return currentModel.editContextFile(row.laptopTool)
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
	return nil
}

func (currentModel *model) editContextFile(laptopTool string) tea.Cmd {
	if laptopTool == "" {
		return currentModel.editContext("")
	}
	return currentModel.editLaptopFile(laptopTool)
}

// startNewSession opens an agent in the folder, asking which agent first when more than one is installed.
// A handoff, when given, is added to the context the agent starts with.
func (currentModel *model) startNewSession(folderId string, withoutContext bool, handoff string) tea.Cmd {
	if len(currentModel.openAgents) >= maximumAgents {
		currentModel.setError(fmt.Sprintf("%d agents open, close one first", maximumAgents))
		return nil
	}
	currentModel.newSessionFolderId = folderId
	currentModel.newSessionWithoutContext = withoutContext
	currentModel.newSessionHandoff = handoff
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
			currentModel.repository.IgnoredSessionIds = append(currentModel.repository.IgnoredSessionIds, sessionId)
		}
		currentModel.repository.Sessions = slices.DeleteFunc(currentModel.repository.Sessions, func(saved store.Session) bool {
			return slices.Contains(sessionIds, saved.SessionId)
		})
		currentModel.saveRepository()
		currentModel.reportError(currentModel.refreshAgents())
		currentModel.clampCursor()
		return nil
	}}
}

// setArchived archives sessions, or brings them back. Archived sessions stay resumable until den forgets them.
func (currentModel *model) setArchived(sessionIds []string, archived bool) {
	for _, sessionId := range sessionIds {
		currentModel.repository.SetArchived(sessionId, archived)
	}
	currentModel.saveRepository()
	currentModel.clampCursor()
	if archived {
		currentModel.setStatus(fmt.Sprintf("archived %s · kept %d days", countedSessions(len(sessionIds)), archiveKeepDays()))
	} else {
		currentModel.setStatus(fmt.Sprintf("brought back %s", countedSessions(len(sessionIds))))
	}
}

// acceptSuggestion carries out a suggestion and says what happened in the footer.
func (currentModel *model) acceptSuggestion(suggestionId string) {
	suggestion := currentModel.repository.FindSuggestion(suggestionId)
	if suggestion == nil {
		return
	}
	done := currentModel.describeDone(*suggestion)
	destinationFolderId, err := currentModel.repository.Accept(suggestionId)
	if currentModel.reportError(err) {
		return
	}
	currentModel.expandFolder(destinationFolderId)
	currentModel.updatePaneFolders()
	currentModel.refreshContextTokens()
	currentModel.saveRepository()
	currentModel.setStatus(done)
}

func (currentModel *model) rejectSuggestion(suggestionId string) {
	currentModel.repository.Reject(suggestionId)
	currentModel.saveRepository()
	currentModel.setStatus("noted · den won't bring that up again")
}

func (currentModel model) describeDone(suggestion store.Suggestion) string {
	switch suggestion.Kind {
	case store.SuggestLink:
		return "linked " + filepath.Base(suggestion.LinkedPath) + " · new sessions in " + currentModel.contextHeading(suggestion.FolderId) + " start with it"
	case store.SuggestFolder:
		return "made " + suggestion.FolderName + " · new sessions there start with its context"
	case store.SuggestRule:
		return "added to " + laptopFileNames(suggestion.FilePaths) + " · agents follow it in every repo"
	}
	return "saved to " + currentModel.contextHeading(suggestion.FolderId) + "'s notes · new sessions there start with them"
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

func archiveAfterDays() int {
	return int(store.ArchiveAfter.Hours() / 24)
}

func archiveKeepDays() int {
	return int(store.ArchiveKeepsFor.Hours() / 24)
}

func countedSessions(count int) string {
	if count == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", count)
}
