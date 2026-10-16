package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// listTopLine is the screen line of the first list row, below the "den" title and a blank line.
const listTopLine = 2

var (
	grabbedStyle  = lipgloss.NewStyle().Bold(true).Reverse(true)
	titleStyle    = lipgloss.NewStyle().Bold(true)
	hintStyle     = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
)

func (currentModel model) View() tea.View {
	var content strings.Builder
	content.WriteString(titleStyle.Render("den") + "\n\n")
	if currentModel.prompt != nil {
		content.WriteString(currentModel.prompt.question + "\n" + currentModel.prompt.input.View() + "\n\n")
		content.WriteString(hintStyle.Render("enter save · esc cancel") + "\n")
	} else if currentModel.confirmingQuit {
		content.WriteString("Quit den and close all agents?\n\n" + hintStyle.Render("y yes · any other key no") + "\n")
	} else if currentModel.picking {
		content.WriteString("Start which agent?\n")
		for adapterIndex, adapter := range currentModel.installedAgents {
			if adapterIndex == currentModel.pickerCursor {
				content.WriteString(selectedStyle.Render("> "+adapter.Name()) + "\n")
			} else {
				content.WriteString("  " + adapter.Name() + "\n")
			}
		}
		content.WriteString("\n" + hintStyle.Render("enter start · esc cancel") + "\n")
	} else if currentModel.deleting != nil {
		currentModel.writeDeleteConfirmation(&content)
	} else {
		currentModel.writeList(&content)
		content.WriteString("\n" + hintStyle.Render(currentModel.footer()) + "\n")
	}
	if currentModel.statusMessage != "" {
		content.WriteString("\n" + currentModel.statusMessage + "\n")
	}
	view := tea.NewView(content.String())
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (currentModel model) writeList(content *strings.Builder) {
	rows := currentModel.rows()
	if len(rows) == 0 {
		content.WriteString(hintStyle.Render("no sessions yet") + "\n")
	}
	for rowIndex, row := range rows {
		openMarker := " "
		if row.open != nil {
			openMarker = "●"
		}
		line := openMarker + " " + strings.Repeat("  ", row.depth) + currentModel.rowContent(row)
		isCarried := (currentModel.grabbed != nil && currentModel.grabbed(row)) || (currentModel.dragging != nil && currentModel.dragging(row))
		if isCarried {
			content.WriteString(grabbedStyle.Render(">"+line) + "\n")
		} else if rowIndex == currentModel.dropTargetRow {
			content.WriteString(selectedStyle.Render("→"+line) + "\n")
		} else if rowIndex == currentModel.listCursor {
			content.WriteString(selectedStyle.Render(">"+line) + "\n")
		} else {
			content.WriteString(" " + line + "\n")
		}
	}
}

func (currentModel model) rowContent(row listRow) string {
	availableWidth := PanelWidth - 3 - 2*row.depth
	if row.folder != nil {
		if row.folder.Collapsed {
			label := fmt.Sprintf("▸ %s (%d)", row.folder.Name, currentModel.repository.SessionCount(row.folder.Id))
			return titleStyle.Render(truncate(label, availableWidth))
		}
		return titleStyle.Render(truncate("▾ "+row.folder.Name, availableWidth))
	}
	if row.session == nil {
		return hintStyle.Render("new " + row.open.tool)
	}
	titleWidth := max(availableWidth-5, 5)
	return fmt.Sprintf("%-*s %s", titleWidth, truncate(row.session.Title, titleWidth), relativeAge(row.session.LastActivityAt))
}

// footer only offers the keys that do something for the selected row.
func (currentModel model) footer() string {
	const everywhere = "n new · f folder · q quit\nclick focus · drag title swap"
	if currentModel.grabbed != nil {
		return "↑↓ move to folder\nenter drop · esc cancel"
	}
	selected, exists := currentModel.selectedRow()
	if !exists {
		return everywhere
	}
	if selected.folder != nil {
		return "enter open/close\nr rename · d delete\n" + everywhere
	}
	if selected.session == nil {
		return "enter focus · space move\nd delete\n" + everywhere
	}
	enterAction := "resume"
	if selected.open != nil {
		enterAction = "focus"
	}
	return "enter " + enterAction + " · space move\nr rename · d delete\n" + everywhere
}

func (currentModel model) writeDeleteConfirmation(content *strings.Builder) {
	deleting := currentModel.deleting
	if deleting.folder != nil {
		destination := "the top level"
		if parent := currentModel.repository.FindFolder(deleting.folder.ParentId); parent != nil {
			destination = parent.Name
		}
		content.WriteString("Delete this folder?\n" + selectedStyle.Render(truncate(deleting.folder.Name, PanelWidth)) + "\n\n")
		content.WriteString("What's inside moves to\n" + truncate(destination, PanelWidth) + ".\n")
		content.WriteString("\n" + hintStyle.Render("y yes · any other key no") + "\n")
		return
	}
	content.WriteString("Delete this session?\n")
	if deleting.session != nil {
		content.WriteString(selectedStyle.Render(truncate(deleting.session.Title, PanelWidth)) + "\n")
	}
	content.WriteString("\n")
	if deleting.open != nil {
		content.WriteString("Its pane will close.\n")
	}
	if deleting.session != nil {
		content.WriteString(hintStyle.Render("It stays in "+deleting.session.Tool+"'s history.") + "\n")
	}
	content.WriteString("\n" + hintStyle.Render("y yes · any other key no") + "\n")
}

func truncate(text string, maximumLength int) string {
	characters := []rune(text)
	if len(characters) <= maximumLength {
		return text
	}
	return string(characters[:maximumLength-1]) + "…"
}

func relativeAge(moment time.Time) string {
	elapsed := time.Since(moment)
	if elapsed < time.Minute {
		return "now"
	} else if elapsed < time.Hour {
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	} else if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	}
	return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
}
