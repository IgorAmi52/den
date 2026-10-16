package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	foldersColumnWidth  = 28
	itemsColumnWidth    = 54
	previewMinimumWidth = 30
)

type organizeColumn int

const (
	foldersColumn organizeColumn = iota
	itemsColumn
)

type organizeEntryKind int

const (
	reviewEntry organizeEntryKind = iota
	repositoryEntry
	inboxEntry
	folderEntry
	archiveEntry
)

// organizeEntry is a line of the folders column.
type organizeEntry struct {
	kind   organizeEntryKind
	folder *store.Folder
	depth  int
}

type organizeItemKind int

const (
	sessionItem organizeItemKind = iota
	newAgentItem
	suggestionItem
	contextFileItem
)

// organizeItem is a line of the middle column: a session, a suggestion, or one of the repo's context files.
type organizeItem struct {
	kind       organizeItemKind
	session    *store.Session
	open       *openAgent
	suggestion *store.Suggestion
	// guide names the guide a context file item shows; empty means the repo's context.md.
	guide string
}

type organizeState struct {
	focus         organizeColumn
	folderCursor  int
	itemCursor    int
	previewOffset int
	selected      map[string]bool
	filter        textinput.Model
	filtering     bool
	picker        *folderPicker
}

// folderPicker is the overlay that asks which folder something goes to.
type folderPicker struct {
	title     string
	folderIds []string
	cursor    int
	onPick    func(currentModel *model, folderId string)
}

func newOrganizeState() organizeState {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "filter sessions"
	filter.CharLimit = 80
	return organizeState{folderCursor: 2, selected: map[string]bool{}, filter: filter}
}

func (state *organizeState) clearSelection() {
	state.selected = map[string]bool{}
}

// selectInOrganizeFromPanel puts the organize cursors on the panel's selected row: its folder, and the row itself.
func (currentModel *model) selectInOrganizeFromPanel() {
	selected, exists := currentModel.selectedRow()
	if !exists {
		return
	}
	state := &currentModel.organize
	entryKind, folderId := folderEntry, selected.folderId()
	switch {
	case selected.kind == archiveRow || (selected.kind == sessionRow && selected.session.Archived):
		entryKind = archiveEntry
	case folderId == "":
		entryKind = inboxEntry
	}
	for index, entry := range currentModel.organizeEntries() {
		if entry.kind == entryKind && (entryKind != folderEntry || entry.folder.Id == folderId) {
			state.folderCursor, state.itemCursor, state.focus, state.previewOffset = index, 0, foldersColumn, 0
		}
	}
	for index, item := range currentModel.organizeItems() {
		isSameSession := item.kind == sessionItem && selected.kind == sessionRow && item.session.SessionId == selected.session.SessionId
		isSameAgent := item.kind == newAgentItem && selected.kind == newAgentRow && item.open.paneId == selected.open.paneId
		if isSameSession || isSameAgent {
			state.itemCursor, state.focus = index, itemsColumn
		}
	}
}

// selectInPanelFromOrganize puts the panel cursor on the session or folder selected in organize, opening its folder.
func (currentModel *model) selectInPanelFromOrganize() {
	if item, exists := currentModel.selectedItem(); exists && currentModel.organize.focus == itemsColumn {
		switch item.kind {
		case sessionItem:
			sessionId := item.session.SessionId
			if item.session.Archived {
				currentModel.archiveExpanded = true
			} else {
				currentModel.expandFolder(item.session.FolderId)
			}
			currentModel.cursorTo(func(row listRow) bool { return row.kind == sessionRow && row.session.SessionId == sessionId })
			return
		case newAgentItem:
			paneId := item.open.paneId
			currentModel.expandFolder(item.open.folderId)
			currentModel.cursorTo(func(row listRow) bool { return row.open != nil && row.open.paneId == paneId })
			return
		}
	}
	entry := currentModel.selectedEntry()
	switch entry.kind {
	case folderEntry:
		folderId := entry.folder.Id
		currentModel.expandFolder(entry.folder.ParentId)
		currentModel.cursorTo(func(row listRow) bool { return row.kind == folderRow && row.folder.Id == folderId })
	case archiveEntry:
		currentModel.cursorTo(func(row listRow) bool { return row.kind == archiveRow })
	default:
		currentModel.cursorTo(func(row listRow) bool { return row.kind == inboxRow })
	}
}

// selectOrganizeFolder puts the folders column's cursor on the folder, as after making it.
func (currentModel *model) selectOrganizeFolder(folderId string) {
	for index, entry := range currentModel.organizeEntries() {
		if entry.kind == folderEntry && entry.folder.Id == folderId {
			currentModel.organize.folderCursor, currentModel.organize.focus, currentModel.organize.itemCursor = index, foldersColumn, 0
		}
	}
}

func (currentModel model) organizeEntries() []organizeEntry {
	entries := []organizeEntry{{kind: reviewEntry}, {kind: repositoryEntry}, {kind: inboxEntry}}
	var addFolders func(parentId string, depth int)
	addFolders = func(parentId string, depth int) {
		for _, folder := range currentModel.subfolders(parentId) {
			entries = append(entries, organizeEntry{kind: folderEntry, folder: folder, depth: depth})
			if !folder.Collapsed {
				addFolders(folder.Id, depth+1)
			}
		}
	}
	addFolders("", 0)
	if currentModel.repository.ArchivedCount() > 0 {
		entries = append(entries, organizeEntry{kind: archiveEntry})
	}
	return entries
}

func (currentModel model) selectedEntry() organizeEntry {
	entries := currentModel.organizeEntries()
	return entries[min(currentModel.organize.folderCursor, len(entries)-1)]
}

// entryFolderId is the folder new sessions and folders go into from this entry.
func (entry organizeEntry) entryFolderId() string {
	if entry.kind == folderEntry {
		return entry.folder.Id
	}
	return ""
}

func (currentModel model) isFiltering() bool {
	return currentModel.organize.filtering || currentModel.organize.filter.Value() != ""
}

func (currentModel model) organizeItems() []organizeItem {
	if currentModel.isFiltering() {
		return currentModel.filteredItems(strings.ToLower(currentModel.organize.filter.Value()))
	}
	var items []organizeItem
	entry := currentModel.selectedEntry()
	switch entry.kind {
	case reviewEntry:
		for index := range currentModel.repository.Suggestions {
			items = append(items, organizeItem{kind: suggestionItem, suggestion: &currentModel.repository.Suggestions[index]})
		}
	case repositoryEntry:
		items = append(items, organizeItem{kind: contextFileItem})
		for _, guide := range store.Guides {
			items = append(items, organizeItem{kind: contextFileItem, guide: guide})
		}
	case archiveEntry:
		for _, session := range currentModel.archivedSessions() {
			items = append(items, organizeItem{kind: sessionItem, session: session, open: currentModel.openAgentFor(session.SessionId)})
		}
	default:
		folderId := entry.entryFolderId()
		for agentIndex := range currentModel.openAgents {
			open := &currentModel.openAgents[agentIndex]
			if open.folderId == folderId && (open.sessionId == "" || currentModel.repository.FindSession(open.sessionId) == nil) {
				items = append(items, organizeItem{kind: newAgentItem, open: open})
			}
		}
		for _, session := range currentModel.sessionsIn(folderId) {
			items = append(items, organizeItem{kind: sessionItem, session: session, open: currentModel.openAgentFor(session.SessionId)})
		}
	}
	return items
}

// filteredItems finds sessions anywhere, archived too, whose title, branch or folder contains the filter text.
func (currentModel model) filteredItems(filterText string) []organizeItem {
	var items []organizeItem
	for index := range currentModel.repository.Sessions {
		session := &currentModel.repository.Sessions[index]
		searchable := strings.ToLower(session.Title + " " + session.Branch + " " + currentModel.repository.FolderName(session.FolderId))
		if filterText == "" || strings.Contains(searchable, filterText) {
			items = append(items, organizeItem{kind: sessionItem, session: session, open: currentModel.openAgentFor(session.SessionId)})
		}
	}
	return items
}

func (currentModel model) selectedItem() (organizeItem, bool) {
	items := currentModel.organizeItems()
	if currentModel.organize.itemCursor >= len(items) {
		return organizeItem{}, false
	}
	return items[currentModel.organize.itemCursor], true
}

// actedOnSessions are the sessions a bulk key acts on: the selection, or else the session under the cursor.
func (currentModel model) actedOnSessions() []string {
	if len(currentModel.organize.selected) > 0 {
		var sessionIds []string
		for _, session := range currentModel.repository.Sessions {
			if currentModel.organize.selected[session.SessionId] {
				sessionIds = append(sessionIds, session.SessionId)
			}
		}
		return sessionIds
	}
	if item, exists := currentModel.selectedItem(); exists && currentModel.organize.focus == itemsColumn && item.kind == sessionItem {
		return []string{item.session.SessionId}
	}
	return nil
}

func (currentModel model) updateOrganize(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.KeyPressMsg:
		return currentModel.updateOrganizeKey(typedMessage)
	case tea.MouseClickMsg:
		return currentModel.updateOrganizeClick(typedMessage)
	}
	if currentModel.prompt != nil {
		return currentModel.updatePrompt(message)
	}
	if currentModel.organize.filtering {
		var command tea.Cmd
		currentModel.organize.filter, command = currentModel.organize.filter.Update(message)
		return currentModel, command
	}
	return currentModel, nil
}

func (currentModel model) updateOrganizeKey(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if currentModel.prompt != nil {
		return currentModel.updatePrompt(keyPress)
	}
	if currentModel.confirming != nil {
		return currentModel.updateConfirmation(keyPress)
	}
	if currentModel.picking {
		return currentModel.updatePicker(keyPress)
	}
	if currentModel.organize.picker != nil {
		return currentModel.updateFolderPicker(keyPress)
	}
	if currentModel.organize.filtering {
		return currentModel.updateFilter(keyPress)
	}
	currentModel.clearStatus()
	keys := currentModel.keys
	state := &currentModel.organize
	switch {
	case key.Matches(keyPress, keys.quit):
		currentModel.askToQuit()
	case key.Matches(keyPress, keys.back):
		currentModel.organizeBack()
	case key.Matches(keyPress, keys.up):
		currentModel.moveOrganizeCursor(-1)
	case key.Matches(keyPress, keys.down):
		currentModel.moveOrganizeCursor(1)
	case key.Matches(keyPress, keys.left):
		state.focus = foldersColumn
		state.previewOffset = 0
	case key.Matches(keyPress, keys.right):
		if len(currentModel.organizeItems()) > 0 {
			state.focus = itemsColumn
			state.previewOffset = 0
		}
	case key.Matches(keyPress, keys.scrollPreview):
		if keyPress.String() == "pgup" || keyPress.String() == "K" {
			state.previewOffset = max(state.previewOffset-5, 0)
		} else {
			state.previewOffset += 5
		}
	case key.Matches(keyPress, keys.filter):
		state.filtering = true
		state.focus = itemsColumn
		state.itemCursor = 0
		return currentModel, state.filter.Focus()
	case key.Matches(keyPress, keys.organize):
		currentModel.toggleOrganize()
	case key.Matches(keyPress, keys.jump):
		currentModel.jumpToAgent(keyPress.String())
	case key.Matches(keyPress, keys.review):
		currentModel.openReview()
	case key.Matches(keyPress, keys.chat):
		return currentModel, currentModel.openChat(nil)
	case key.Matches(keyPress, keys.help):
		currentModel.openScreen(helpScreen)
	case key.Matches(keyPress, keys.newSession):
		return currentModel, currentModel.startNewSession(currentModel.organizeFolderId(), false)
	case key.Matches(keyPress, keys.newWithout):
		return currentModel, currentModel.startNewSession(currentModel.organizeFolderId(), true)
	case key.Matches(keyPress, keys.newFolder):
		return currentModel, currentModel.askForNewFolder(currentModel.organizeFolderId())
	case state.focus == foldersColumn:
		return currentModel.updateOrganizeFolders(keyPress)
	default:
		return currentModel.updateOrganizeItems(keyPress)
	}
	return currentModel, nil
}

func (currentModel model) updateOrganizeFolders(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keys := currentModel.keys
	entry := currentModel.selectedEntry()
	switch {
	case key.Matches(keyPress, keys.open):
		if entry.kind == reviewEntry {
			currentModel.openReview()
		} else if len(currentModel.organizeItems()) > 0 {
			currentModel.organize.focus = itemsColumn
		}
	case key.Matches(keyPress, keys.collapse):
		if entry.kind == folderEntry {
			currentModel.toggleFolder(entry.folder.Id)
		}
	case key.Matches(keyPress, keys.editContext):
		switch entry.kind {
		case folderEntry:
			return currentModel, currentModel.editContext(entry.folder.Id)
		case repositoryEntry, inboxEntry:
			return currentModel, currentModel.editContext("")
		}
	case key.Matches(keyPress, keys.rename):
		if entry.kind == folderEntry {
			return currentModel, currentModel.askForRename(listRow{kind: folderRow, folder: entry.folder}, false)
		}
	case key.Matches(keyPress, keys.delete):
		if entry.kind == folderEntry {
			currentModel.askToDelete(listRow{kind: folderRow, folder: entry.folder})
		}
	}
	return currentModel, nil
}

func (currentModel model) updateOrganizeItems(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keys := currentModel.keys
	item, exists := currentModel.selectedItem()
	if !exists {
		return currentModel, nil
	}
	sessionIds := currentModel.actedOnSessions()
	switch {
	case key.Matches(keyPress, keys.selectRow):
		if item.kind == sessionItem {
			currentModel.toggleSelected(item.session.SessionId)
			currentModel.moveOrganizeCursor(1)
		}
	case key.Matches(keyPress, keys.open):
		return currentModel.openItem(item)
	case key.Matches(keyPress, keys.accept):
		if item.kind == suggestionItem {
			currentModel.acceptSuggestion(item.suggestion.Id)
		} else {
			currentModel.acceptFilingForAll(sessionIds)
		}
		currentModel.clampItemCursor()
	case key.Matches(keyPress, keys.dismiss):
		if item.kind == suggestionItem {
			currentModel.rejectSuggestion(item.suggestion.Id)
		} else {
			for _, sessionId := range sessionIds {
				currentModel.rejectFilingFor(sessionId)
			}
		}
		currentModel.clampItemCursor()
	case key.Matches(keyPress, keys.move):
		if len(sessionIds) > 0 {
			currentModel.askWhereToMove(sessionIds)
		} else if item.kind == newAgentItem {
			paneId := item.open.paneId
			currentModel.pickFolder("Move new "+item.open.tool+" to", func(currentModel *model, folderId string) {
				for agentIndex := range currentModel.openAgents {
					if currentModel.openAgents[agentIndex].paneId == paneId {
						currentModel.moveRowToFolder(listRow{kind: newAgentRow, open: &currentModel.openAgents[agentIndex]}, folderId)
					}
				}
				currentModel.clampItemCursor()
			})
		}
	case key.Matches(keyPress, keys.archive):
		if len(sessionIds) > 0 {
			currentModel.setArchived(sessionIds, !currentModel.repository.FindSession(sessionIds[0]).Archived)
			currentModel.organize.clearSelection()
			currentModel.clampItemCursor()
		}
	case key.Matches(keyPress, keys.delete):
		if len(sessionIds) > 0 {
			currentModel.askToDeleteSessions(sessionIds)
		} else if item.kind == newAgentItem {
			currentModel.askToDelete(listRow{kind: newAgentRow, open: item.open})
		}
	case key.Matches(keyPress, keys.rename):
		if item.kind == sessionItem {
			return currentModel, currentModel.askForRename(listRow{kind: sessionRow, session: item.session}, false)
		}
	case key.Matches(keyPress, keys.editContext):
		switch item.kind {
		case contextFileItem:
			return currentModel, currentModel.editContextFile(item.guide)
		case sessionItem:
			return currentModel, currentModel.editContext(item.session.FolderId)
		}
	}
	return currentModel, nil
}

func (currentModel model) openItem(item organizeItem) (tea.Model, tea.Cmd) {
	switch item.kind {
	case sessionItem:
		if item.open != nil {
			currentModel.focusPane(item.open.paneId)
		} else {
			currentModel.resumeSession(item.session.SessionId)
		}
	case newAgentItem:
		currentModel.focusPane(item.open.paneId)
	case suggestionItem:
		currentModel.openReview()
		currentModel.review.startAt = item.suggestion.Id
	case contextFileItem:
		return currentModel, currentModel.editContextFile(item.guide)
	}
	return currentModel, nil
}

func (currentModel *model) editContextFile(guide string) tea.Cmd {
	if guide == "" {
		return currentModel.editContext("")
	}
	return currentModel.editGuide(guide)
}

func (currentModel *model) acceptFilingForAll(sessionIds []string) {
	accepted := 0
	for _, sessionId := range sessionIds {
		if suggestion := currentModel.repository.PendingFor(sessionId); suggestion != nil {
			currentModel.acceptSuggestion(suggestion.Id)
			accepted++
		}
	}
	if accepted == 0 {
		currentModel.setStatus("no suggestion for that · m to move it yourself")
	} else if accepted > 1 {
		currentModel.setStatus(fmt.Sprintf("accepted %d suggestions", accepted))
	}
	currentModel.organize.clearSelection()
}

func (currentModel *model) toggleSelected(sessionId string) {
	if currentModel.organize.selected[sessionId] {
		delete(currentModel.organize.selected, sessionId)
	} else {
		currentModel.organize.selected[sessionId] = true
	}
}

// organizeBack steps back: out of the selection, then the filter, then the sessions column.
func (currentModel *model) organizeBack() {
	state := &currentModel.organize
	switch {
	case len(state.selected) > 0:
		state.clearSelection()
	case state.filter.Value() != "":
		state.filter.SetValue("")
		state.itemCursor = 0
	case state.focus == itemsColumn:
		state.focus = foldersColumn
	case len(currentModel.openAgents) > 0 && currentModel.zoomedHere():
		currentModel.toggleOrganize()
	}
}

func (currentModel model) zoomedHere() bool {
	return len(currentModel.openAgents) > 0 && currentModel.width >= organizeMinimumWidth
}

func (currentModel *model) moveOrganizeCursor(direction int) {
	state := &currentModel.organize
	state.previewOffset = 0
	if state.focus == foldersColumn {
		state.folderCursor = max(min(state.folderCursor+direction, len(currentModel.organizeEntries())-1), 0)
		state.itemCursor = 0
		return
	}
	state.itemCursor = max(min(state.itemCursor+direction, len(currentModel.organizeItems())-1), 0)
}

func (currentModel *model) clampItemCursor() {
	state := &currentModel.organize
	itemCount := len(currentModel.organizeItems())
	state.itemCursor = max(min(state.itemCursor, itemCount-1), 0)
	if itemCount == 0 {
		state.focus = foldersColumn
	}
	state.folderCursor = max(min(state.folderCursor, len(currentModel.organizeEntries())-1), 0)
}

// organizeFolderId is the folder the selected entry or session belongs to, where new things go.
func (currentModel model) organizeFolderId() string {
	if item, exists := currentModel.selectedItem(); exists && currentModel.organize.focus == itemsColumn && item.kind == sessionItem && !item.session.Archived {
		return item.session.FolderId
	}
	return currentModel.selectedEntry().entryFolderId()
}

func (currentModel model) updateFilter(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	state := &currentModel.organize
	switch {
	case key.Matches(keyPress, currentModel.keys.back):
		state.filtering = false
		state.filter.SetValue("")
		state.filter.Blur()
		state.itemCursor = 0
		return currentModel, nil
	case key.Matches(keyPress, currentModel.keys.open), keyPress.String() == "down", keyPress.String() == "up":
		state.filtering = false
		state.filter.Blur()
		if len(currentModel.organizeItems()) == 0 {
			state.filter.SetValue("")
		}
		return currentModel, nil
	}
	var command tea.Cmd
	state.filter, command = state.filter.Update(keyPress)
	state.itemCursor = 0
	return currentModel, command
}

// askWhereToMove opens the folder picker for the sessions.
func (currentModel *model) askWhereToMove(sessionIds []string) {
	title := "Move " + countedSessions(len(sessionIds)) + " to"
	if len(sessionIds) == 1 {
		title = "Move " + truncate(currentModel.repository.FindSession(sessionIds[0]).Title, 30) + " to"
	}
	currentModel.pickFolder(title, func(currentModel *model, folderId string) {
		currentModel.moveSessionsToFolder(sessionIds, folderId)
		currentModel.organize.clearSelection()
		currentModel.clampItemCursor()
	})
}

func (currentModel *model) pickFolder(title string, onPick func(currentModel *model, folderId string)) {
	folderIds := []string{""}
	for _, entry := range currentModel.allFolderEntries() {
		folderIds = append(folderIds, entry.folder.Id)
	}
	currentModel.organize.picker = &folderPicker{title: title, folderIds: folderIds, onPick: onPick}
}

// allFolderEntries lists every folder in tree order, collapsed or not, for the picker.
func (currentModel model) allFolderEntries() []organizeEntry {
	var entries []organizeEntry
	var addFolders func(parentId string, depth int)
	addFolders = func(parentId string, depth int) {
		for _, folder := range currentModel.subfolders(parentId) {
			entries = append(entries, organizeEntry{kind: folderEntry, folder: folder, depth: depth})
			addFolders(folder.Id, depth+1)
		}
	}
	addFolders("", 0)
	return entries
}

func (currentModel model) updateFolderPicker(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	picker := currentModel.organize.picker
	keys := currentModel.keys
	switch {
	case key.Matches(keyPress, keys.up):
		picker.cursor = max(picker.cursor-1, 0)
	case key.Matches(keyPress, keys.down):
		picker.cursor = min(picker.cursor+1, len(picker.folderIds)-1)
	case key.Matches(keyPress, keys.open):
		currentModel.organize.picker = nil
		picker.onPick(&currentModel, picker.folderIds[picker.cursor])
	case key.Matches(keyPress, keys.back), key.Matches(keyPress, keys.quit):
		currentModel.organize.picker = nil
	}
	return currentModel, nil
}

// updateOrganizeClick selects the clicked folder or session.
func (currentModel model) updateOrganizeClick(click tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := click.Mouse()
	if mouse.Button != tea.MouseLeft || currentModel.organize.picker != nil || currentModel.prompt != nil {
		return currentModel, nil
	}
	columnsTop, columnsHeight := currentModel.organizeColumnsTop(), currentModel.organizeColumnsHeight()
	state := &currentModel.organize
	switch {
	case mouse.X < foldersColumnWidth:
		line := mouse.Y - columnsTop - 1
		entryIndex, isEntry := folderEntryAt(line + currentModel.foldersOffset(columnsHeight))
		if line >= 0 && isEntry && entryIndex < len(currentModel.organizeEntries()) {
			state.folderCursor, state.itemCursor, state.focus, state.previewOffset = entryIndex, 0, foldersColumn, 0
		}
	case mouse.X < foldersColumnWidth+1+currentModel.itemsWidth():
		line := mouse.Y - columnsTop - currentModel.itemsHeaderHeight()
		itemIndex := line + currentModel.itemsOffset(columnsHeight)
		if line >= 0 && itemIndex < len(currentModel.organizeItems()) {
			state.itemCursor, state.focus, state.previewOffset = itemIndex, itemsColumn, 0
		}
	}
	return currentModel, nil
}
