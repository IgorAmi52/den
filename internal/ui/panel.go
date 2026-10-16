package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	// listTopLine is the screen line of the first tree row, below the header and a blank line.
	listTopLine = 2
	// footerHeight is the rule plus two lines of keys.
	footerHeight = 3
	ghostWidth   = 12
)

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
		if currentModel.isOrganizeLayout() {
			content = currentModel.organizeView()
		} else {
			content = currentModel.panelView()
		}
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (currentModel model) panelWidth() int {
	if currentModel.width > 0 {
		return currentModel.width
	}
	return PanelWidth
}

func (currentModel model) listHeight() int {
	return max(currentModel.height-listTopLine-footerHeight, 1)
}

func (currentModel model) panelView() string {
	width := currentModel.panelWidth()
	lines := []string{currentModel.panelHeader(width), ""}
	rows := currentModel.rows()
	for rowIndex := currentModel.listOffset; rowIndex < len(rows) && rowIndex < currentModel.listOffset+currentModel.listHeight(); rowIndex++ {
		lines = append(lines, currentModel.panelRowLine(rows[rowIndex], rowIndex, width))
	}
	if len(rows) == 1 && len(currentModel.openAgents) == 0 {
		lines = append(lines, currentModel.theme.faint.Render(truncate("  no sessions yet · n to start one", width)))
	}
	for len(lines) < currentModel.height-footerHeight {
		lines = append(lines, "")
	}
	lines = append(lines, currentModel.theme.border.Render(strings.Repeat("─", width)))
	lines = append(lines, currentModel.panelFooter(width)...)
	return strings.Join(lines, "\n")
}

// panelHeader reads "den · repo" with the waiting-suggestions badge on the right.
func (currentModel model) panelHeader(width int) string {
	left := []segment{{text: "den", style: currentModel.theme.accentBold}, {text: " · " + currentModel.repositoryName(), style: currentModel.theme.dim}}
	var right []segment
	if pending := len(currentModel.repository.Suggestions); pending > 0 {
		right = append(right, segment{text: fmt.Sprintf("✦ %d", pending), style: currentModel.theme.suggestion})
	}
	return currentModel.layoutRow(left, right, width, false)
}

func (currentModel model) panelRowLine(row listRow, rowIndex int, width int) string {
	isCarried := (currentModel.grabbed != nil && currentModel.grabbed(row)) || (currentModel.dragging != nil && currentModel.dragging(row))
	isSelected := rowIndex == currentModel.listCursor || isCarried
	if currentModel.prompt != nil && currentModel.prompt.inlineRow != nil && currentModel.prompt.inlineRow(row) {
		indent := strings.Repeat("  ", row.depth)
		input := currentModel.prompt.input
		input.SetWidth(width - len(indent) - len(input.Prompt) - 1)
		return padRight(indent+input.View(), width)
	}
	left := []segment{{text: strings.Repeat("  ", row.depth), style: currentModel.theme.text}}
	if rowIndex == currentModel.dropTargetRow && !isCarried {
		left[0] = segment{text: "→" + strings.Repeat(" ", max(row.depth*2-1, 1)), style: currentModel.theme.dropTarget}
	}
	if isCarried {
		left[0] = segment{text: "▸" + strings.Repeat(" ", max(row.depth*2-1, 1)), style: currentModel.theme.accentBold}
	}
	rowLeft, right := currentModel.rowSegments(row, width)
	return currentModel.layoutRow(append(left, rowLeft...), right, width, isSelected)
}

// rowSegments builds a tree row without its indent: the folder arrow or the session's pane number and status,
// the name, and on the right the age, the count or a suggestion as ghost text.
func (currentModel model) rowSegments(row listRow, width int) (left []segment, right []segment) {
	switch row.kind {
	case inboxRow:
		left = []segment{{text: folderArrow(currentModel.inboxCollapsed) + " Inbox", style: currentModel.theme.bold}}
		if count := len(currentModel.sessionsIn("")); count > 0 {
			right = []segment{{text: fmt.Sprint(count), style: currentModel.theme.dim}}
		}
	case folderRow:
		left = []segment{{text: folderArrow(row.folder.Collapsed) + " ", style: currentModel.theme.bold}}
		if row.folder.Branch != "" {
			left = append(left, segment{text: "⎇ ", style: currentModel.theme.dim}, segment{text: row.folder.Name, style: currentModel.theme.text})
		} else {
			left = append(left, segment{text: row.folder.Name, style: currentModel.theme.bold})
		}
		if _, hasContext := currentModel.contextTokens[row.folder.Id]; hasContext {
			right = append(right, segment{text: "◆", style: currentModel.theme.accent})
		}
		if count := currentModel.repository.SessionCount(row.folder.Id); row.folder.Collapsed && count > 0 {
			if len(right) > 0 {
				right = append(right, segment{text: " ", style: currentModel.theme.text})
			}
			right = append(right, segment{text: fmt.Sprint(count), style: currentModel.theme.dim})
		}
	case archiveRow:
		left = []segment{{text: folderArrow(!currentModel.archiveExpanded) + " Archive", style: currentModel.theme.dim}}
		right = []segment{{text: fmt.Sprint(currentModel.repository.ArchivedCount()), style: currentModel.theme.dim}}
	case newAgentRow:
		left = append(currentModel.paneMarker(row.open), segment{text: " new " + row.open.tool, style: currentModel.theme.dim})
		right = []segment{{text: relativeAge(row.open.launchedAt), style: currentModel.theme.dim}}
	case sessionRow:
		left = append(currentModel.paneMarker(row.open), segment{text: " " + row.session.Title, style: currentModel.theme.text})
		if row.session.Archived {
			left[len(left)-1].style = currentModel.theme.dim
		}
		if ghost := currentModel.ghostText(row.session.SessionId); ghost != "" {
			right = []segment{{text: truncate(ghost, min(ghostWidth, width/2)), style: currentModel.theme.ghost}}
		} else {
			right = []segment{{text: relativeAge(row.session.LastActivityAt), style: currentModel.theme.dim}}
		}
	}
	return left, right
}

// paneMarker is two cells: the pane number of an open agent and its status glyph, or a faint dot when not open.
func (currentModel model) paneMarker(open *openAgent) []segment {
	if open == nil {
		return []segment{{text: " ·", style: currentModel.theme.faint}}
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

// ghostText names where the session's waiting filing suggestion would put it.
func (currentModel model) ghostText(sessionId string) string {
	suggestion := currentModel.repository.PendingFor(sessionId)
	if suggestion == nil {
		return ""
	}
	if suggestion.Kind == store.SuggestSubfolder {
		return "→ ⎇ " + suggestion.FolderName
	}
	return "→ " + currentModel.repository.FolderName(suggestion.FolderId)
}

func (currentModel model) panelFooter(width int) []string {
	theme := currentModel.theme
	hints := func(bindings ...key.Binding) []string {
		return currentModel.hintLines(bindings, width, 2)
	}
	var lines []string
	switch {
	case currentModel.prompt != nil && currentModel.prompt.inlineRow == nil:
		lines = []string{theme.dim.Render(truncate(currentModel.prompt.question, width)), currentModel.prompt.input.View()}
	case currentModel.prompt != nil:
		lines = hints(currentModel.keys.save, relabeled(currentModel.keys.back, "cancel"))
	case currentModel.confirming != nil:
		lines = []string{currentModel.confirmationLine(width), theme.dim.Render(truncate(currentModel.confirming.detail, width))}
	case currentModel.picking:
		lines = []string{currentModel.pickerLine(width)}
		lines = append(lines, hints(relabeled(currentModel.keys.open, "start"), relabeled(currentModel.keys.back, "cancel"))...)
	case currentModel.grabbed != nil:
		lines = append([]string{theme.dim.Render("carry it to a folder")}, hints(relabeled(currentModel.keys.down, "folder"), relabeled(currentModel.keys.open, "drop"), relabeled(currentModel.keys.back, "cancel"))...)
	case currentModel.statusMessage != "":
		style := theme.dim
		if currentModel.statusIsError {
			style = theme.errorText
		}
		lines = append([]string{style.Render(truncate(currentModel.statusMessage, width))}, currentModel.hintLinesEndingWith(currentModel.panelHints(), currentModel.keys.help, width, 1)...)
	default:
		lines = currentModel.hintLinesEndingWith(currentModel.panelHints(), currentModel.keys.help, width, 2)
	}
	for len(lines) < 2 {
		lines = append(lines, "")
	}
	return lines[:2]
}

// panelHints are the keys that do something for the selected row, most useful first.
func (currentModel model) panelHints() []key.Binding {
	keys := currentModel.keys
	var bindings []key.Binding
	selected, exists := currentModel.selectedRow()
	if exists {
		switch selected.kind {
		case sessionRow:
			if ghost := currentModel.ghostText(selected.session.SessionId); ghost != "" {
				bindings = append(bindings, relabeled(keys.accept, "file "+strings.TrimPrefix(ghost, "→ ")))
			}
			enterAction := "resume"
			if selected.open != nil {
				enterAction = "focus"
			}
			bindings = append(bindings, relabeled(keys.open, enterAction), keys.rename, keys.move)
			if selected.session.Archived {
				bindings = append(bindings, relabeled(keys.archive, "unarchive"))
			} else {
				bindings = append(bindings, keys.archive)
			}
			bindings = append(bindings, keys.delete)
		case newAgentRow:
			bindings = append(bindings, relabeled(keys.open, "focus"), keys.move, relabeled(keys.delete, "close"))
		case folderRow:
			bindings = append(bindings, relabeled(keys.open, "open"), relabeled(keys.newSession, "new here"), keys.editContext, keys.rename, relabeled(keys.newFolder, "subfolder"), keys.delete)
		case inboxRow, archiveRow:
			bindings = append(bindings, relabeled(keys.open, "open"))
		}
	}
	if !exists || selected.kind != folderRow {
		bindings = append(bindings, keys.newSession)
	}
	if len(currentModel.repository.Suggestions) > 0 {
		bindings = append(bindings, keys.review)
	}
	return append(bindings, keys.organize)
}

// confirmationLine is the question followed by its y/n keys.
func (currentModel model) confirmationLine(width int) string {
	theme := currentModel.theme
	return currentModel.layoutRow([]segment{{text: currentModel.confirming.question + " ", style: theme.bold}},
		[]segment{{text: "y", style: theme.key}, {text: "/", style: theme.dim}, {text: "n", style: theme.key}}, width, false)
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

// scrollToCursor moves the visible part of the tree so the cursor stays on screen.
func (currentModel *model) scrollToCursor() {
	listHeight := currentModel.listHeight()
	if currentModel.listCursor < currentModel.listOffset {
		currentModel.listOffset = currentModel.listCursor
	}
	if currentModel.listCursor >= currentModel.listOffset+listHeight {
		currentModel.listOffset = currentModel.listCursor - listHeight + 1
	}
	currentModel.listOffset = max(min(currentModel.listOffset, len(currentModel.rows())-listHeight), 0)
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
