package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	// wideMinimumWidth is the panel width from which it shows the preview beside the tree.
	wideMinimumWidth = 80
	// footerHeight is the rule plus two lines of keys.
	footerHeight = 3
	ghostWidth   = 12
)

// treeLine is one screen line of the tree: a row, or with rowIndex -1, a line explaining the selected row's proposal.
type treeLine struct {
	text     string
	rowIndex int
}

func (currentModel model) View() tea.View {
	var content string
	switch currentModel.currentScreen() {
	case welcomeScreen:
		content = currentModel.welcomeView()
	case reviewScreen:
		content = currentModel.reviewView()
	case chatScreen:
		content = currentModel.chatView()
	case helpScreen:
		content = currentModel.helpView()
	default:
		content = currentModel.treeView()
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

// isWide is true when the panel is zoomed, or alone in the window because no agent is open.
func (currentModel model) isWide() bool {
	return currentModel.width >= wideMinimumWidth
}

func (currentModel model) panelWidth() int {
	if currentModel.width > 0 {
		return currentModel.width
	}
	return PanelWidth
}

// treeWidth is the tree's column: the whole panel when narrow, or the left part beside the preview when wide.
func (currentModel model) treeWidth() int {
	if !currentModel.isWide() {
		return currentModel.panelWidth()
	}
	return max(min(currentModel.width*2/5, 64), 40)
}

// treeTop is the screen line of the tree's first line: below the header and a blank line,
// or when wide, below the top bar, the waiting alert and a rule.
func (currentModel model) treeTop() int {
	if currentModel.isWide() && len(currentModel.waitingAgents()) > 0 {
		return 3
	}
	return 2
}

func (currentModel model) treeHeight() int {
	return max(currentModel.height-currentModel.treeTop()-footerHeight, 1)
}

func (currentModel model) treeView() string {
	width := currentModel.panelWidth()
	theme := currentModel.theme
	var lines []string
	if currentModel.isWide() {
		lines = append(lines, currentModel.wideTopBar(width))
		if waiting := currentModel.waitingAgents(); len(waiting) > 0 {
			lines = append(lines, theme.waitingBar.Render(padRight(" "+currentModel.waitingAlert(waiting), width)))
		}
		lines = append(lines, theme.border.Render(strings.Repeat("─", width)))
		treeWidth, height := currentModel.treeWidth(), currentModel.treeHeight()
		separator := strings.TrimSuffix(strings.Repeat(theme.border.Render("│")+"\n", height), "\n")
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			strings.Join(currentModel.treeLines(treeWidth, height), "\n"),
			separator,
			strings.Join(currentModel.previewColumnLines(width-treeWidth-1, height), "\n")))
	} else {
		lines = append(lines, currentModel.panelHeader(width), "")
		lines = append(lines, currentModel.treeLines(width, currentModel.treeHeight())...)
	}
	lines = append(lines, theme.border.Render(strings.Repeat("─", width)))
	lines = append(lines, padLines(currentModel.footer(width), currentModel.footerMargin())...)
	content := strings.Join(lines, "\n")
	if currentModel.picker != nil {
		content = currentModel.withOverlay(content, currentModel.folderPickerBox())
	}
	return content
}

func (currentModel model) footerMargin() string {
	if currentModel.isWide() {
		return " "
	}
	return ""
}

// panelHeader reads "den · repo" with the waiting-suggestions badge, or the active filter, on the right.
func (currentModel model) panelHeader(width int) string {
	left := []segment{{text: "den", style: currentModel.theme.accentBold}, {text: " · " + currentModel.repositoryName(), style: currentModel.theme.dim}}
	return currentModel.layoutRow(left, currentModel.headerBadge(), width, false)
}

func (currentModel model) headerBadge() []segment {
	if filterText := currentModel.filter.Value(); filterText != "" && !currentModel.filtering {
		return []segment{{text: "/" + truncate(filterText, 10), style: currentModel.theme.accent}}
	}
	if pending := len(currentModel.repository.Suggestions); pending > 0 {
		return []segment{{text: fmt.Sprintf("✦ %d", pending), style: currentModel.theme.suggestion}}
	}
	return nil
}

// wideTopBar shows den, every agent's status with the key that jumps to it, and the way back to the agents.
func (currentModel model) wideTopBar(width int) string {
	theme := currentModel.theme
	left := []segment{{text: " den", style: theme.accentBold}, {text: " · " + currentModel.repositoryName(), style: theme.dim}}
	if badge := currentModel.headerBadge(); len(badge) > 0 {
		left = append(append(left, segment{text: "   ", style: theme.text}), badge...)
	}
	var right []segment
	if len(currentModel.openAgents) == 0 {
		right = append(right, segment{text: "no agents open · n starts one ", style: theme.faint})
	} else {
		for _, open := range currentModel.openAgents {
			right = append(right, segment{text: fmt.Sprintf("⌥%d", open.number), style: theme.accentBold}, currentModel.statusGlyph(open.status), segment{text: "  ", style: theme.text})
		}
		right = append(right, segment{text: "z", style: theme.key}, segment{text: " back to agents ", style: theme.action})
	}
	return currentModel.layoutRow(left, right, width, false)
}

func (currentModel model) waitingAgents() []openAgent {
	var waiting []openAgent
	for _, open := range currentModel.openAgents {
		if open.status == "waiting" {
			waiting = append(waiting, open)
		}
	}
	return waiting
}

func (currentModel model) waitingAlert(waiting []openAgent) string {
	if len(waiting) > 1 {
		var numbers []string
		for _, open := range waiting {
			numbers = append(numbers, fmt.Sprintf("⌥%d", open.number))
		}
		return "! agents are waiting for you · " + strings.Join(numbers, " ") + " jumps to one"
	}
	title := "new " + waiting[0].tool
	if session := currentModel.repository.FindSession(waiting[0].sessionId); session != nil {
		title = session.Title
	}
	return fmt.Sprintf("! %s is waiting for you · ⌥%d jumps there", truncate(title, 50), waiting[0].number)
}

// treeLayout lays out every row, with the selected row's proposal explained under it.
func (currentModel model) treeLayout(width int) []treeLine {
	rows := currentModel.rows()
	isAnswering := currentModel.prompt != nil || currentModel.confirming != nil
	var layout []treeLine
	for rowIndex, row := range rows {
		// A blank line before each top-level group keeps folders apart from the sessions above them.
		if row.depth == 0 && rowIndex > 0 {
			layout = append(layout, treeLine{rowIndex: -1})
		}
		layout = append(layout, treeLine{text: currentModel.treeRowLine(row, rowIndex, width), rowIndex: rowIndex})
		if rowIndex == currentModel.listCursor && !isAnswering {
			for _, line := range currentModel.proposalLines(row, width) {
				layout = append(layout, treeLine{text: line, rowIndex: -1})
			}
		}
		if row.kind == inboxRow && len(currentModel.repository.Sessions) == 0 && len(currentModel.openAgents) == 0 {
			layout = append(layout, treeLine{text: currentModel.theme.faint.Render(truncate("    no sessions yet · n starts one", width)), rowIndex: -1})
		}
	}
	if len(rows) == 0 {
		layout = append(layout, treeLine{text: currentModel.theme.faint.Render(truncate("   no session matches", width)), rowIndex: -1})
	}
	return layout
}

func (currentModel model) treeLines(width int, height int) []string {
	layout := currentModel.treeLayout(width)
	var lines []string
	for lineIndex := currentModel.listOffset; lineIndex < len(layout) && len(lines) < height; lineIndex++ {
		lines = append(lines, padRight(layout[lineIndex].text, width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines
}

// scrollToCursor moves the visible part of the tree so the selected row and its explanation stay on screen.
func (currentModel *model) scrollToCursor() {
	layout := currentModel.treeLayout(currentModel.treeWidth())
	height := currentModel.treeHeight()
	firstLine, lastLine := -1, -1
	for lineIndex, line := range layout {
		if line.rowIndex == currentModel.listCursor {
			firstLine, lastLine = lineIndex, lineIndex
		} else if firstLine >= 0 && line.rowIndex == -1 && lastLine == lineIndex-1 {
			lastLine = lineIndex
		}
	}
	if firstLine < 0 {
		return
	}
	if firstLine < currentModel.listOffset {
		currentModel.listOffset = firstLine
	}
	if lastLine >= currentModel.listOffset+height {
		currentModel.listOffset = min(lastLine-height+1, firstLine)
	}
	currentModel.listOffset = max(min(currentModel.listOffset, len(layout)-height), 0)
}

// rowAtLine finds the row shown on a screen line of the tree, if any.
func (currentModel model) rowAtLine(screenLine int) (int, bool) {
	if screenLine < currentModel.treeTop() || screenLine >= currentModel.treeTop()+currentModel.treeHeight() {
		return 0, false
	}
	lineIndex := screenLine - currentModel.treeTop() + currentModel.listOffset
	layout := currentModel.treeLayout(currentModel.treeWidth())
	if lineIndex >= len(layout) {
		return 0, false
	}
	return layout[lineIndex].rowIndex, layout[lineIndex].rowIndex >= 0
}

func (currentModel model) treeRowLine(row listRow, rowIndex int, width int) string {
	theme := currentModel.theme
	isDragged := currentModel.dragging != nil && currentModel.dragging(row)
	isSelected := rowIndex == currentModel.listCursor || isDragged
	indent := strings.Repeat("  ", row.depth)
	if currentModel.prompt != nil && currentModel.prompt.inlineRow != nil && currentModel.prompt.inlineRow(row) {
		input := currentModel.prompt.input
		input.SetWidth(width - 1 - len(indent) - len(input.Prompt) - 1)
		return padRight(theme.accentBold.Render("▌")+indent+input.View(), width)
	}
	gutter := segment{text: " ", style: theme.text}
	switch {
	case rowIndex == currentModel.dropTargetRow && !isDragged:
		gutter = segment{text: "→", style: theme.dropTarget}
	case isSelected:
		gutter = segment{text: "▌", style: theme.accentBold}
	}
	left, right := currentModel.rowSegments(row, width)
	return currentModel.layoutRow(append([]segment{gutter, {text: indent, style: theme.text}}, left...), right, width, isSelected)
}

// rowSegments builds a tree row without its indent: the folder arrow or the session's pane number and status,
// the name, and on the right the age, the count, or what den proposes for it.
func (currentModel model) rowSegments(row listRow, width int) (left []segment, right []segment) {
	theme := currentModel.theme
	proposalMark := func() []segment {
		if currentModel.proposalFor(row) != nil {
			return []segment{{text: "✦ ", style: theme.suggestion}}
		}
		return nil
	}
	switch row.kind {
	case repositoryRow:
		left = []segment{{text: "◆ ", style: theme.accent}, {text: "context & guides", style: theme.accent}}
		right = append(proposalMark(), segment{text: folderArrow(!currentModel.repositoryExpanded), style: theme.dim})
	case contextFileRow:
		name, _, tokens := currentModel.contextFileDetails(row.guide)
		left = []segment{{text: name, style: theme.dim}}
		right = proposalMark()
		if tokens > 0 {
			right = append(right, segment{text: fmt.Sprintf("~%d", tokens), style: theme.dim})
		} else {
			right = append(right, segment{text: "empty", style: theme.faint})
		}
	case inboxRow:
		left = []segment{{text: folderArrow(currentModel.inboxCollapsed) + " ", style: theme.faint}, {text: "Inbox", style: theme.folder}}
		right = []segment{{text: fmt.Sprint(len(currentModel.sessionsIn(""))), style: theme.dim}}
	case folderRow:
		left = []segment{{text: folderArrow(row.folder.Collapsed) + " ", style: theme.faint}}
		if row.folder.Branch != "" {
			left = append(left, segment{text: "⎇ ", style: theme.folder})
		}
		left = append(left, segment{text: row.folder.Name, style: theme.folder})
		right = proposalMark()
		if _, hasContext := currentModel.contextTokens[row.folder.Id]; hasContext {
			right = append(right, segment{text: "◆ ", style: theme.accent})
		}
		right = append(right, segment{text: fmt.Sprint(currentModel.repository.SessionCount(row.folder.Id)), style: theme.dim})
	case archiveRow:
		left = []segment{{text: folderArrow(!currentModel.archiveExpanded) + " ", style: theme.faint}, {text: "Archive", style: theme.archive}}
		right = []segment{{text: fmt.Sprint(currentModel.repository.ArchivedCount()), style: theme.dim}}
	case newAgentRow:
		left = append(currentModel.paneMarker(row.open), segment{text: " new " + row.open.tool, style: theme.dim})
		right = []segment{{text: relativeAge(row.open.launchedAt), style: theme.dim}}
	case sessionRow:
		left = append(currentModel.paneMarker(row.open), segment{text: " " + row.session.Title, style: theme.text})
		if row.session.Archived {
			left[len(left)-1].style = theme.dim
		}
		switch ghost := currentModel.ghostText(row.session.SessionId); {
		case ghost != "":
			right = []segment{{text: truncate(ghost, min(ghostWidth, width/2)), style: theme.ghost}}
		case row.session.Archived:
			daysLeft := int(row.session.ArchiveTimeLeft(time.Now()).Hours()/24) + 1
			right = []segment{{text: fmt.Sprintf("%dd left", daysLeft), style: theme.faint}}
		default:
			right = []segment{{text: relativeAge(row.session.LastActivityAt), style: theme.dim}}
		}
	}
	return left, right
}

// paneMarker is two cells: the pane number of an open agent and its status glyph, or blank when not open.
func (currentModel model) paneMarker(open *openAgent) []segment {
	if open == nil {
		return []segment{{text: "  ", style: currentModel.theme.text}}
	}
	return []segment{{text: fmt.Sprint(open.number), style: currentModel.theme.accentBold}, currentModel.statusGlyph(open.status)}
}

func (currentModel model) statusGlyph(status string) segment {
	switch status {
	case "running":
		return segment{text: "●", style: currentModel.theme.running}
	case "waiting":
		return segment{text: "!", style: currentModel.theme.waiting}
	}
	return segment{text: "●", style: currentModel.theme.dim}
}

func (currentModel model) footer(width int) []string {
	theme := currentModel.theme
	keys := currentModel.keys
	width -= len(currentModel.footerMargin())
	hints := func(bindings ...key.Binding) []string {
		return currentModel.hintLines(bindings, width, 2)
	}
	var lines []string
	switch {
	case currentModel.filtering:
		lines = append([]string{currentModel.filter.View()}, hints(relabeled(keys.open, "keep"), relabeled(keys.back, "clear"))...)
	case currentModel.prompt != nil && currentModel.prompt.inlineRow == nil:
		lines = []string{theme.dim.Render(truncate(currentModel.prompt.question, width)), currentModel.prompt.input.View()}
	case currentModel.prompt != nil:
		lines = hints(keys.save, relabeled(keys.back, "cancel"))
	case currentModel.confirming != nil:
		lines = []string{currentModel.confirmationLine(width), theme.dim.Render(truncate(currentModel.confirming.detail, width))}
	case currentModel.picking:
		lines = append([]string{currentModel.pickerLine(width)}, hints(relabeled(keys.open, "start"), relabeled(keys.back, "cancel"))...)
	case currentModel.picker != nil:
		lines = hints(relabeled(keys.down, "folder"), relabeled(keys.open, "move here"), relabeled(keys.back, "cancel"))
	case currentModel.statusMessage != "":
		style := theme.dim
		if currentModel.statusIsError {
			style = theme.errorText
		}
		lines = append([]string{style.Render(truncate(currentModel.statusMessage, width))}, currentModel.hintLinesEndingWith(currentModel.rowHints(), keys.help, width, 1)...)
	default:
		lines = currentModel.hintLinesEndingWith(currentModel.rowHints(), keys.help, width, 2)
	}
	for len(lines) < 2 {
		lines = append(lines, "")
	}
	return lines[:2]
}

// rowHints are the keys that do something for the selected row, most useful first.
func (currentModel model) rowHints() []key.Binding {
	keys := currentModel.keys
	var bindings []key.Binding
	selected, exists := currentModel.selectedRow()
	if exists {
		switch selected.kind {
		case sessionRow:
			enterAction := "resume"
			if selected.open != nil {
				enterAction = "focus"
			}
			bindings = append(bindings, relabeled(keys.open, enterAction), keys.move, keys.rename)
			if selected.session.Archived {
				bindings = append(bindings, relabeled(keys.archive, "unarchive"))
			} else {
				bindings = append(bindings, keys.archive)
			}
			bindings = append(bindings, keys.delete)
		case newAgentRow:
			bindings = append(bindings, relabeled(keys.open, "focus"), keys.move, relabeled(keys.delete, "close"))
		case folderRow:
			bindings = append(bindings, relabeled(keys.open, "open"), relabeled(keys.newSession, "new here"), keys.move, keys.editContext, keys.rename, keys.newSubfolder, keys.delete)
		case repositoryRow:
			bindings = append(bindings, relabeled(keys.open, "show files"), relabeled(keys.editContext, "repo context"))
		case contextFileRow:
			bindings = append(bindings, relabeled(keys.open, "edit"))
		case inboxRow, archiveRow:
			bindings = append(bindings, relabeled(keys.open, "open"))
		}
	}
	if !exists || selected.kind != folderRow {
		bindings = append(bindings, keys.newSession)
	}
	bindings = append(bindings, keys.newFolder)
	if len(currentModel.repository.Suggestions) > 0 {
		bindings = append(bindings, keys.review)
	}
	if !currentModel.isWide() {
		return append(bindings, keys.wide)
	}
	bindings = append(bindings, keys.filter, keys.chat)
	if len(currentModel.openAgents) > 0 {
		bindings = append(bindings, relabeled(keys.wide, "back to agents"))
	}
	return append(bindings, keys.quit)
}

// confirmationLine is the question followed by its keys.
func (currentModel model) confirmationLine(width int) string {
	theme := currentModel.theme
	keyHints := currentModel.hintLines([]key.Binding{currentModel.keys.confirmYes, currentModel.keys.confirmNo}, width, 1)[0]
	return currentModel.layoutRow([]segment{{text: currentModel.confirming.question + " ", style: theme.bold}},
		[]segment{{text: keyHints, style: theme.text}}, width, false)
}

func (currentModel model) pickerLine(width int) string {
	segments := []segment{{text: "start ", style: currentModel.theme.dim}}
	for adapterIndex, adapter := range currentModel.installedAgents {
		if adapterIndex == currentModel.pickerCursor {
			segments = append(segments, segment{text: "▸" + adapter.Name() + " ", style: currentModel.theme.accentBold})
		} else {
			segments = append(segments, segment{text: " " + adapter.Name() + " ", style: currentModel.theme.text})
		}
	}
	return currentModel.layoutRow(segments, nil, width, false)
}

func (currentModel model) contextFileDetails(guide string) (name string, description string, tokens int) {
	if guide == "" {
		return "context.md", "every session", store.EstimatedTokens(currentModel.repository.ReadContext(""))
	}
	return guide + ".md", "guide", store.EstimatedTokens(currentModel.repository.ReadGuide(guide))
}

func padLines(lines []string, prefix string) []string {
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return lines
}

func folderArrow(collapsed bool) string {
	if collapsed {
		return "▸"
	}
	return "▾"
}

func relativeAge(moment time.Time) string {
	elapsed := time.Since(moment)
	if elapsed < time.Minute {
		return "now"
	} else if elapsed < time.Hour {
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	} else if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	} else if elapsed < 60*24*time.Hour {
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
	return fmt.Sprintf("%dmo", int(elapsed.Hours()/24/30))
}
