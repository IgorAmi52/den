package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/store"
)

func (currentModel model) waitingAgents() []openAgent {
	var waiting []openAgent
	for _, open := range currentModel.openAgents {
		if open.status == "waiting" {
			waiting = append(waiting, open)
		}
	}
	return waiting
}

// organizeColumnsHeight is what's left for the columns between the top bar and the key bar.
func (currentModel model) organizeColumnsHeight() int {
	return max(currentModel.height-currentModel.organizeColumnsTop()-1-len(currentModel.organizeFooter(currentModel.width)), 3)
}

// organizeColumnsTop is the screen line where the column headers are.
func (currentModel model) organizeColumnsTop() int {
	if len(currentModel.waitingAgents()) > 0 {
		return 3
	}
	return 2
}

// columnWidths fits folders, sessions and preview into the window; a narrow window drops the preview.
func (currentModel model) columnWidths() (items int, preview int) {
	available := currentModel.width - foldersColumnWidth - 2
	if available-itemsColumnWidth >= previewMinimumWidth {
		return itemsColumnWidth, available - itemsColumnWidth
	}
	if available-previewMinimumWidth >= 34 {
		return available - previewMinimumWidth, previewMinimumWidth
	}
	return available + 1, 0
}

func (currentModel model) itemsWidth() int {
	items, _ := currentModel.columnWidths()
	return items
}

func (currentModel model) organizeView() string {
	width := currentModel.width
	theme := currentModel.theme
	lines := []string{currentModel.organizeTopBar(width)}
	if waiting := currentModel.waitingAgents(); len(waiting) > 0 {
		lines = append(lines, theme.waitingBar.Render(padRight(" "+currentModel.waitingAlert(waiting), width)))
	}
	lines = append(lines, theme.border.Render(strings.Repeat("─", width)))
	footer := currentModel.organizeFooter(width)
	columnsHeight := currentModel.organizeColumnsHeight()
	itemsWidth, previewWidth := currentModel.columnWidths()
	separator := strings.TrimSuffix(strings.Repeat(theme.border.Render("│")+"\n", columnsHeight), "\n")
	columns := []string{
		strings.Join(currentModel.foldersColumnLines(foldersColumnWidth, columnsHeight), "\n"),
		separator,
		strings.Join(currentModel.itemsColumnLines(itemsWidth, columnsHeight), "\n"),
	}
	if previewWidth > 0 {
		columns = append(columns, separator, strings.Join(currentModel.previewColumnLines(previewWidth, columnsHeight), "\n"))
	}
	lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, columns...))
	lines = append(lines, theme.border.Render(strings.Repeat("─", width)))
	lines = append(lines, footer...)
	content := strings.Join(lines, "\n")
	if currentModel.organize.picker != nil {
		content = currentModel.withOverlay(content, currentModel.folderPickerBox())
	}
	return content
}

// organizeTopBar shows the view's name and every agent's status, so nothing is lost while zoomed.
func (currentModel model) organizeTopBar(width int) string {
	theme := currentModel.theme
	left := []segment{{text: " den", style: theme.accentBold}, {text: " · " + currentModel.repositoryName() + " · organize", style: theme.dim}}
	var right []segment
	if len(currentModel.openAgents) == 0 {
		right = append(right, segment{text: "no agents open · n starts one ", style: theme.faint})
	} else {
		right = append(right, segment{text: "agents ", style: theme.dim})
		for _, open := range currentModel.openAgents {
			right = append(right, segment{text: fmt.Sprint(open.number), style: theme.accentBold}, currentModel.statusGlyph(open.status), segment{text: "  ", style: theme.text})
		}
		right = append(right, segment{text: "z", style: theme.key}, segment{text: " back to agents ", style: theme.action})
	}
	return currentModel.layoutRow(left, right, width, false)
}

func (currentModel model) waitingAlert(waiting []openAgent) string {
	if len(waiting) > 1 {
		var numbers []string
		for _, open := range waiting {
			numbers = append(numbers, fmt.Sprint(open.number))
		}
		return fmt.Sprintf("! agents %s are waiting for you · press a number to jump", strings.Join(numbers, ", "))
	}
	title := "new " + waiting[0].tool
	if session := currentModel.repository.FindSession(waiting[0].sessionId); session != nil {
		title = session.Title
	}
	return fmt.Sprintf("! %d · %s is waiting for you · press %d to jump", waiting[0].number, truncate(title, 50), waiting[0].number)
}

func (currentModel model) foldersColumnLines(width int, height int) []string {
	theme := currentModel.theme
	var lines []string
	entries := currentModel.organizeEntries()
	cursor := min(currentModel.organize.folderCursor, len(entries)-1)
	for index, entry := range entries {
		left, right := currentModel.entrySegments(entry)
		isSelected := index == cursor
		marker := segment{text: " ", style: theme.text}
		if isSelected && currentModel.organize.focus != foldersColumn {
			marker = segment{text: "›", style: theme.accentBold}
		}
		left = append([]segment{marker, {text: strings.Repeat("  ", entry.depth), style: theme.text}}, left...)
		right = append(right, segment{text: " ", style: theme.text})
		lines = append(lines, currentModel.layoutRow(left, right, width, isSelected && currentModel.organize.focus == foldersColumn))
		if entry.kind == repositoryEntry {
			lines = append(lines, "")
		}
	}
	offset := currentModel.foldersOffset(height)
	lines = append([]string{theme.columnHeader.Render(" FOLDERS")}, lines[offset:]...)
	return fitLines(strings.Join(lines, "\n"), width, height)
}

// folderEntryLine is the line of an entry in the folders column, counting the blank line after the repo entry.
func folderEntryLine(entryIndex int) int {
	if entryIndex > 1 {
		return entryIndex + 1
	}
	return entryIndex
}

// folderEntryAt is the entry shown on a line of the folders column, if any.
func folderEntryAt(line int) (int, bool) {
	switch {
	case line < 2:
		return line, true
	case line == 2:
		return 0, false
	}
	return line - 1, true
}

// foldersOffset scrolls the folders column so the cursor stays on screen below its header.
func (currentModel model) foldersOffset(height int) int {
	return max(folderEntryLine(currentModel.organize.folderCursor)-(height-1)+1, 0)
}

// itemsHeaderHeight is the header of the sessions column: one line, or two with the filter input.
func (currentModel model) itemsHeaderHeight() int {
	if currentModel.isFiltering() {
		return 2
	}
	return 1
}

// itemsOffset scrolls the sessions column so the cursor stays on screen below its header.
func (currentModel model) itemsOffset(height int) int {
	return max(currentModel.organize.itemCursor-(height-currentModel.itemsHeaderHeight())+1, 0)
}

func (currentModel model) entrySegments(entry organizeEntry) (left []segment, right []segment) {
	theme := currentModel.theme
	switch entry.kind {
	case reviewEntry:
		count := len(currentModel.repository.Suggestions)
		style := theme.suggestion
		if count == 0 {
			style = theme.faint
		}
		left = []segment{{text: "✦ Review", style: style}}
		if count > 0 {
			right = []segment{{text: fmt.Sprint(count), style: theme.suggestion}}
		}
	case repositoryEntry:
		left = []segment{{text: "◆ ", style: theme.accent}, {text: currentModel.repositoryName(), style: theme.bold}}
		right = []segment{{text: "repo", style: theme.faint}}
	case inboxEntry:
		left = []segment{{text: "  Inbox", style: theme.bold}}
		if count := len(currentModel.sessionsIn("")); count > 0 {
			right = []segment{{text: fmt.Sprint(count), style: theme.dim}}
		}
	case folderEntry:
		arrow := " "
		if len(currentModel.subfolders(entry.folder.Id)) > 0 {
			arrow = folderArrow(entry.folder.Collapsed)
		}
		left = []segment{{text: arrow + " ", style: theme.dim}}
		if entry.folder.Branch != "" {
			left = append(left, segment{text: "⎇ ", style: theme.dim}, segment{text: entry.folder.Name, style: theme.text})
		} else {
			left = append(left, segment{text: entry.folder.Name, style: theme.bold})
		}
		if _, hasContext := currentModel.contextTokens[entry.folder.Id]; hasContext {
			right = append(right, segment{text: "◆ ", style: theme.accent})
		}
		right = append(right, segment{text: fmt.Sprint(currentModel.repository.SessionCount(entry.folder.Id)), style: theme.dim})
	case archiveEntry:
		left = []segment{{text: "  Archive", style: theme.dim}}
		right = []segment{{text: fmt.Sprint(currentModel.repository.ArchivedCount()), style: theme.dim}}
	}
	return left, right
}

func (currentModel model) itemsColumnLines(width int, height int) []string {
	theme := currentModel.theme
	items := currentModel.organizeItems()
	var lines []string
	if currentModel.isFiltering() {
		lines = append(lines, " "+currentModel.organize.filter.View(), theme.columnHeader.Render(fmt.Sprintf(" %d MATCHES", len(items))))
	} else {
		lines = append(lines, theme.columnHeader.Render(" "+currentModel.itemsHeader(len(items))))
	}
	listHeight := height - len(lines)
	offset := currentModel.itemsOffset(height)
	for index := offset; index < len(items) && index < offset+listHeight; index++ {
		isSelected := index == currentModel.organize.itemCursor && currentModel.organize.focus == itemsColumn
		left, right := currentModel.itemSegments(items[index], width)
		lines = append(lines, currentModel.layoutRow(left, append(right, segment{text: " ", style: theme.text}), width, isSelected))
	}
	if len(items) == 0 {
		lines = append(lines, theme.faint.Render(" "+currentModel.emptyItemsHint()))
	}
	if selectedCount := len(currentModel.organize.selected); selectedCount > 0 && len(lines) < height {
		lines = append(lines, "", theme.accent.Render(fmt.Sprintf(" %d selected", selectedCount)))
	}
	return fitLines(strings.Join(lines, "\n"), width, height)
}

func (currentModel model) itemsHeader(count int) string {
	entry := currentModel.selectedEntry()
	switch entry.kind {
	case reviewEntry:
		return fmt.Sprintf("REVIEW · %d WAITING", count)
	case repositoryEntry:
		return "CONTEXT AND GUIDES FOR THE WHOLE REPO"
	case archiveEntry:
		return fmt.Sprintf("ARCHIVE · %s", strings.ToUpper(countedSessions(count)))
	case inboxEntry:
		return fmt.Sprintf("INBOX · %s", strings.ToUpper(countedSessions(count)))
	}
	return fmt.Sprintf("%s · %s", strings.ToUpper(entry.folder.Name), strings.ToUpper(countedSessions(count)))
}

func (currentModel model) emptyItemsHint() string {
	switch currentModel.selectedEntry().kind {
	case reviewEntry:
		return "nothing to review · den suggests only when sure"
	case inboxEntry:
		return "inbox zero · n starts a session here"
	}
	if currentModel.isFiltering() {
		return "no session matches"
	}
	return "no sessions here · n starts one, m moves some in"
}

func (currentModel model) itemSegments(item organizeItem, width int) (left []segment, right []segment) {
	theme := currentModel.theme
	switch item.kind {
	case sessionItem:
		checkbox := segment{text: " [ ] ", style: theme.faint}
		if currentModel.organize.selected[item.session.SessionId] {
			checkbox = segment{text: " [x] ", style: theme.accentBold}
		}
		left = append([]segment{checkbox}, currentModel.paneMarker(item.open)...)
		titleStyle := theme.text
		if item.session.Archived {
			titleStyle = theme.dim
		}
		left = append(left, segment{text: " " + item.session.Title, style: titleStyle})
		if currentModel.isFiltering() {
			right = append(right, segment{text: truncate(currentModel.repository.FolderName(item.session.FolderId), 16) + " · ", style: theme.faint})
		}
		right = append(right, segment{text: relativeAge(item.session.LastActivityAt), style: theme.dim})
		if ghost := currentModel.ghostText(item.session.SessionId); ghost != "" {
			right = append(right, segment{text: "  " + truncate(ghost, 14), style: theme.ghost})
		}
	case newAgentItem:
		left = append([]segment{{text: "     ", style: theme.text}}, currentModel.paneMarker(item.open)...)
		left = append(left, segment{text: " new " + item.open.tool, style: theme.dim})
		right = []segment{{text: "waiting for a first prompt", style: theme.faint}}
	case suggestionItem:
		label, summary := currentModel.suggestionSummary(*item.suggestion)
		left = []segment{{text: " ✦ ", style: theme.suggestion}, {text: padRight(label, 10), style: theme.suggestion}, {text: summary, style: theme.text}}
	case contextFileItem:
		name, description, tokens := currentModel.contextFileDetails(item.guide)
		marker := segment{text: "   ", style: theme.text}
		if item.guide == "" {
			marker = segment{text: " ◆ ", style: theme.accent}
		}
		left = []segment{marker, {text: padRight(name, 18), style: theme.bold}, {text: description, style: theme.dim}}
		if tokens > 0 {
			right = []segment{{text: fmt.Sprintf("~%d tokens", tokens), style: theme.dim}}
		} else {
			right = []segment{{text: "not set · e", style: theme.faint}}
		}
	}
	return left, right
}

func (currentModel model) contextFileDetails(guide string) (name string, description string, tokens int) {
	if guide == "" {
		return "context.md", "every session", store.EstimatedTokens(currentModel.repository.ReadContext(""))
	}
	return guide + ".md", "guide", store.EstimatedTokens(currentModel.repository.ReadGuide(guide))
}

// suggestionSummary is a suggestion's kind and one line about it.
func (currentModel model) suggestionSummary(suggestion store.Suggestion) (label string, summary string) {
	switch suggestion.Kind {
	case store.SuggestMove:
		title := countedSessions(len(suggestion.SessionIds))
		if len(suggestion.SessionIds) == 1 {
			if session := currentModel.repository.FindSession(suggestion.SessionIds[0]); session != nil {
				title = session.Title
			}
		}
		return "Move", truncate(title, 28) + " → " + currentModel.repository.FolderName(suggestion.FolderId)
	case store.SuggestSubfolder:
		return "Subfolder", fmt.Sprintf("⎇ %s in %s (%d)", suggestion.FolderName, currentModel.repository.FolderName(suggestion.ParentFolderId), len(suggestion.SessionIds))
	case store.SuggestCleanup:
		return "Cleanup", countedSessions(len(suggestion.SessionIds)) + " look finished"
	}
	added, removed := diffCounts(lineDiff(currentModel.currentContextText(suggestion), suggestion.ProposedText))
	return "Context", fmt.Sprintf("%s  +%d −%d", currentModel.contextFileName(suggestion), added, removed)
}

func (currentModel model) organizeFooter(width int) []string {
	theme := currentModel.theme
	hints := func(bindings ...key.Binding) []string {
		return padLines(currentModel.hintLines(bindings, width-2, 2), " ")
	}
	keys := currentModel.keys
	switch {
	case currentModel.prompt != nil:
		return []string{" " + theme.dim.Render(currentModel.prompt.question) + "  " + currentModel.prompt.input.View()}
	case currentModel.confirming != nil:
		return []string{" " + theme.bold.Render(currentModel.confirming.question) + "  " + theme.dim.Render(currentModel.confirming.detail), hints(keys.confirmYes, keys.confirmNo)[0]}
	case currentModel.picking:
		return []string{" " + currentModel.pickerLine(width-2), hints(relabeled(keys.open, "start"), relabeled(keys.back, "cancel"))[0]}
	case currentModel.organize.picker != nil:
		return hints(relabeled(keys.down, "folder"), relabeled(keys.open, "move here"), relabeled(keys.back, "cancel"))
	case currentModel.organize.filtering:
		return hints(relabeled(keys.open, "keep filter"), relabeled(keys.back, "clear"))
	case currentModel.statusMessage != "":
		style := theme.dim
		if currentModel.statusIsError {
			style = theme.errorText
		}
		return []string{" " + style.Render(truncate(currentModel.statusMessage, width-2)), " " + currentModel.hintLinesEndingWith(currentModel.organizeHints(), keys.help, width-2, 1)[0]}
	}
	return padLines(currentModel.hintLinesEndingWith(currentModel.organizeHints(), keys.help, width-2, 2), " ")
}

func (currentModel model) organizeHints() []key.Binding {
	keys := currentModel.keys
	var bindings []key.Binding
	if currentModel.organize.focus == foldersColumn {
		entry := currentModel.selectedEntry()
		switch entry.kind {
		case reviewEntry:
			bindings = append(bindings, relabeled(keys.open, "review one by one"))
		case repositoryEntry:
			bindings = append(bindings, relabeled(keys.right, "files"), relabeled(keys.editContext, "repo context"))
		case folderEntry:
			bindings = append(bindings, relabeled(keys.right, "sessions"), relabeled(keys.newSession, "new here"), keys.editContext, keys.rename, relabeled(keys.newFolder, "subfolder"), keys.delete, relabeled(keys.collapse, "fold"))
		default:
			bindings = append(bindings, relabeled(keys.right, "sessions"), keys.newSession, keys.newFolder)
		}
	} else if item, exists := currentModel.selectedItem(); exists {
		switch item.kind {
		case sessionItem:
			count := len(currentModel.actedOnSessions())
			bindings = append(bindings, keys.selectRow)
			acceptLabel := "accept"
			if count > 1 {
				acceptLabel = fmt.Sprintf("accept %d", count)
			}
			bindings = append(bindings, relabeled(keys.accept, acceptLabel), keys.move, keys.archive, keys.delete, keys.rename)
			enterAction := "resume"
			if item.open != nil {
				enterAction = "focus"
			}
			bindings = append(bindings, relabeled(keys.open, enterAction), keys.editContext)
		case suggestionItem:
			bindings = append(bindings, keys.accept, relabeled(keys.dismiss, "reject"), relabeled(keys.open, "review"))
		case contextFileItem:
			bindings = append(bindings, relabeled(keys.editContext, "edit"))
		case newAgentItem:
			bindings = append(bindings, relabeled(keys.open, "focus"))
		}
		bindings = append(bindings, relabeled(keys.left, "folders"))
	}
	if !slices.ContainsFunc(bindings, func(binding key.Binding) bool { return binding.Help().Key == keys.newSession.Help().Key }) {
		bindings = append(bindings, keys.newSession)
	}
	bindings = append(bindings, keys.filter, keys.chat)
	if len(currentModel.repository.Suggestions) > 0 {
		bindings = append(bindings, keys.review)
	}
	if len(currentModel.openAgents) > 0 {
		bindings = append(bindings, keys.jump, relabeled(keys.organize, "back to agents"))
	}
	return append(bindings, keys.quit)
}

func (currentModel model) folderPickerBox() string {
	picker := currentModel.organize.picker
	theme := currentModel.theme
	const boxWidth = 36
	lines := []string{theme.bold.Render(truncate(picker.title, boxWidth)), ""}
	depthById := map[string]int{"": 0}
	for _, entry := range currentModel.allFolderEntries() {
		depthById[entry.folder.Id] = entry.depth
	}
	for index, folderId := range picker.folderIds {
		name := currentModel.repository.FolderName(folderId)
		prefix := strings.Repeat("  ", depthById[folderId])
		if folder := currentModel.repository.FindFolder(folderId); folder != nil && folder.Branch != "" {
			prefix += "⎇ "
		}
		left := []segment{{text: prefix + name, style: theme.text}}
		var right []segment
		if _, hasContext := currentModel.contextTokens[folderId]; hasContext && folderId != "" {
			right = []segment{{text: "◆", style: theme.accent}}
		}
		lines = append(lines, currentModel.layoutRow(left, right, boxWidth, index == picker.cursor))
	}
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

func padLines(lines []string, prefix string) []string {
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return lines
}
