package layout

import (
	"fmt"
	"strings"

	"github.com/IgorAmi52/den/internal/tmux"
)

const agentsPerRow = 2

type area struct {
	width, height, left, top int
}

// Apply arranges the window as the panel on the left plus agents in a grid,
// filled row by row in pane order. It does nothing when every pane already sits in its cell,
// so running it from tmux's layout hooks cannot loop. A zoomed window is left alone, or the zoom would end at once.
func Apply(windowId string, panelMarkerOption string, panelWidth int) error {
	if tmux.IsZoomed(windowId) {
		return nil
	}
	panelPaneIds, err := tmux.WindowPanesWithOption(windowId, panelMarkerOption)
	if err != nil || len(panelPaneIds) == 0 {
		return err
	}
	windowPanes, err := tmux.ListPanes(windowId)
	if err != nil {
		return err
	}
	var agentPaneIds []string
	for _, pane := range windowPanes {
		if pane.Id != panelPaneIds[0] {
			agentPaneIds = append(agentPaneIds, pane.Id)
		}
	}
	windowWidth, windowHeight, err := tmux.WindowSize(windowId)
	if err != nil {
		return err
	}
	cells := map[string]area{}
	desiredLayout := build(area{width: windowWidth, height: windowHeight}, panelWidth, panelPaneIds[0], agentPaneIds, cells)
	if allPanesInTheirCells(windowPanes, cells) {
		return nil
	}
	return tmux.SelectLayout(windowId, desiredLayout)
}

// allPanesInTheirCells compares real pane geometry with the planned cells.
// Panes on the window's top edge give their first row to the border status line;
// lower panes show it on the border line above them, so their size is unchanged.
func allPanesInTheirCells(windowPanes []tmux.Pane, cells map[string]area) bool {
	for _, pane := range windowPanes {
		expected := cells[pane.Id]
		if expected.top == 0 {
			expected.top, expected.height = 1, expected.height-1
		}
		if pane.Left != expected.left || pane.Width != expected.width || pane.Top != expected.top || pane.Height != expected.height {
			return false
		}
	}
	return true
}

func build(window area, panelWidth int, panelPaneId string, agentPaneIds []string, cells map[string]area) string {
	if len(agentPaneIds) == 0 {
		return withChecksum(leaf(window, panelPaneId, cells))
	}
	panelArea := area{width: panelWidth, height: window.height}
	agentsArea := area{width: window.width - panelWidth - 1, height: window.height, left: panelWidth + 1}
	return withChecksum(container(window, "{", "}", []string{leaf(panelArea, panelPaneId, cells), grid(agentsArea, agentPaneIds, cells)}))
}

func grid(agentsArea area, agentPaneIds []string, cells map[string]area) string {
	var rows [][]string
	for start := 0; start < len(agentPaneIds); start += agentsPerRow {
		rows = append(rows, agentPaneIds[start:min(start+agentsPerRow, len(agentPaneIds))])
	}
	if len(rows) == 1 {
		return row(agentsArea, rows[0], cells)
	}
	rowAreas := splitVertically(agentsArea, len(rows))
	var rowLayouts []string
	for rowIndex, rowPaneIds := range rows {
		rowLayouts = append(rowLayouts, row(rowAreas[rowIndex], rowPaneIds, cells))
	}
	return container(agentsArea, "[", "]", rowLayouts)
}

func row(rowArea area, paneIds []string, cells map[string]area) string {
	if len(paneIds) == 1 {
		return leaf(rowArea, paneIds[0], cells)
	}
	cellAreas := splitHorizontally(rowArea, len(paneIds))
	var cellLayouts []string
	for cellIndex, paneId := range paneIds {
		cellLayouts = append(cellLayouts, leaf(cellAreas[cellIndex], paneId, cells))
	}
	return container(rowArea, "{", "}", cellLayouts)
}

func splitHorizontally(parent area, count int) []area {
	var areas []area
	left := parent.left
	for _, width := range splitLength(parent.width, count) {
		areas = append(areas, area{width: width, height: parent.height, left: left, top: parent.top})
		left += width + 1
	}
	return areas
}

func splitVertically(parent area, count int) []area {
	var areas []area
	top := parent.top
	for _, height := range splitLength(parent.height, count) {
		areas = append(areas, area{width: parent.width, height: height, left: parent.left, top: top})
		top += height + 1
	}
	return areas
}

// splitLength divides a length into equal parts, leaving one cell between parts for the border.
func splitLength(total int, count int) []int {
	available := total - (count - 1)
	lengths := make([]int, count)
	for index := range lengths {
		lengths[index] = available / count
		if index < available%count {
			lengths[index]++
		}
	}
	return lengths
}

func leaf(cell area, paneId string, cells map[string]area) string {
	cells[paneId] = cell
	return fmt.Sprintf("%dx%d,%d,%d,%s", cell.width, cell.height, cell.left, cell.top, strings.TrimPrefix(paneId, "%"))
}

func container(cell area, open string, close string, children []string) string {
	return fmt.Sprintf("%dx%d,%d,%d%s%s%s", cell.width, cell.height, cell.left, cell.top, open, strings.Join(children, ","), close)
}

// withChecksum prefixes the layout with the 16-bit rotating checksum tmux requires.
func withChecksum(layout string) string {
	checksum := 0
	for _, character := range []byte(layout) {
		checksum = (checksum >> 1) + ((checksum & 1) << 15)
		checksum = (checksum + int(character)) & 0xffff
	}
	return fmt.Sprintf("%04x,%s", checksum, layout)
}
