package layout

import (
	"fmt"
	"strings"

	"github.com/IgorAmi52/den/internal/tmux"
)

const agentsPerRow = 2

// ResizingOption is set on the window while a pane border is being dragged, so the layout hooks leave the panes alone.
const ResizingOption = "@den-resizing"

// sizesOption keeps the sizes the user dragged the panes to, as "<panel width> <agent count> <top row share> <left shares>",
// shares in thousandths of the length, 0 for an even split. The agent shares only hold while the agent count stays the same.
const sizesOption = "@den-sizes"

const (
	minimumPanelWidth = 20
	maximumPanelWidth = 60
	minimumShare      = 150
	maximumShare      = 850
)

type area struct {
	width, height, left, top int
}

type sizes struct {
	panelWidth  int
	agentCount  int
	topRowShare int
	leftShares  [2]int
}

// Apply arranges the window as the panel on the left plus agents in a grid,
// filled row by row in pane order, at the sizes the user dragged to or evenly. It does nothing when every pane already
// sits in its cell, so running it from tmux's layout hooks cannot loop. A zoomed window is left alone, or the zoom would
// end at once, and so is one whose border is being dragged.
func Apply(windowId string, panelMarkerOption string, defaultPanelWidth int) error {
	if tmux.IsZoomed(windowId) || tmux.WindowOption(windowId, ResizingOption) != "" {
		return nil
	}
	panelPane, agentPanes, windowPanes, err := readPanes(windowId, panelMarkerOption)
	if err != nil || panelPane == nil {
		return err
	}
	windowWidth, windowHeight, err := tmux.WindowSize(windowId)
	if err != nil {
		return err
	}
	stored := readSizes(windowId, defaultPanelWidth, len(agentPanes))
	if stored.agentCount != len(agentPanes) {
		stored = sizes{panelWidth: stored.panelWidth, agentCount: len(agentPanes)}
		if err := saveSizes(windowId, stored); err != nil {
			return err
		}
	}
	var agentPaneIds []string
	for _, pane := range agentPanes {
		agentPaneIds = append(agentPaneIds, pane.Id)
	}
	cells := map[string]area{}
	desiredLayout := build(area{width: windowWidth, height: windowHeight}, stored, panelPane.Id, agentPaneIds, cells)
	if allPanesInTheirCells(windowPanes, cells) {
		return nil
	}
	return tmux.SelectLayout(windowId, desiredLayout)
}

// Record keeps the sizes a finished border drag left the panes at, then lays the window out with them.
func Record(windowId string, panelMarkerOption string, defaultPanelWidth int) error {
	panelPane, agentPanes, _, err := readPanes(windowId, panelMarkerOption)
	if err != nil || panelPane == nil {
		return err
	}
	recorded := sizes{panelWidth: clamp(panelPane.Width, minimumPanelWidth, maximumPanelWidth), agentCount: len(agentPanes)}
	rows := inRows(agentPanes)
	if len(rows) == 2 {
		// The top row gives its first line to the border status line, so its cell is one line taller than its pane.
		recorded.topRowShare = share(rows[0][0].Height+1, rows[1][0].Height)
	}
	for rowIndex, rowPanes := range rows {
		if len(rowPanes) == 2 {
			recorded.leftShares[rowIndex] = share(rowPanes[0].Width, rowPanes[1].Width)
		}
	}
	if err := saveSizes(windowId, recorded); err != nil {
		return err
	}
	if err := tmux.UnsetWindowOption(windowId, ResizingOption); err != nil {
		return err
	}
	return Apply(windowId, panelMarkerOption, defaultPanelWidth)
}

// Reset forgets the dragged sizes, so the panel and the agents go back to their default sizes.
func Reset(windowId string, panelMarkerOption string, defaultPanelWidth int) error {
	for _, option := range []string{sizesOption, ResizingOption} {
		if err := tmux.UnsetWindowOption(windowId, option); err != nil {
			return err
		}
	}
	return Apply(windowId, panelMarkerOption, defaultPanelWidth)
}

// readPanes returns the panel's pane, or nil when the window has none, the agents' panes, and all panes, in pane order.
func readPanes(windowId string, panelMarkerOption string) (*tmux.Pane, []tmux.Pane, []tmux.Pane, error) {
	windowPanes, err := tmux.ListPanes(windowId, panelMarkerOption)
	if err != nil {
		return nil, nil, nil, err
	}
	var panelPane *tmux.Pane
	var agentPanes []tmux.Pane
	for index, pane := range windowPanes {
		if pane.Options[panelMarkerOption] != "" && panelPane == nil {
			panelPane = &windowPanes[index]
		} else {
			agentPanes = append(agentPanes, pane)
		}
	}
	return panelPane, agentPanes, windowPanes, nil
}

func readSizes(windowId string, defaultPanelWidth int, agentCount int) sizes {
	defaults := sizes{panelWidth: defaultPanelWidth, agentCount: agentCount}
	value := tmux.WindowOption(windowId, sizesOption)
	if value == "" {
		return defaults
	}
	var stored sizes
	scanned, _ := fmt.Sscanf(value, "%d %d %d %d %d", &stored.panelWidth, &stored.agentCount, &stored.topRowShare, &stored.leftShares[0], &stored.leftShares[1])
	if scanned != 5 {
		return defaults
	}
	return stored
}

func saveSizes(windowId string, stored sizes) error {
	value := fmt.Sprintf("%d %d %d %d %d", stored.panelWidth, stored.agentCount, stored.topRowShare, stored.leftShares[0], stored.leftShares[1])
	return tmux.SetWindowOption(windowId, sizesOption, value)
}

// share is how many thousandths of the two lengths the first takes, kept away from the edges so no pane vanishes.
func share(first int, second int) int {
	return clamp(first*1000/max(first+second, 1), minimumShare, maximumShare)
}

func clamp(value int, lowest int, highest int) int {
	return max(lowest, min(value, highest))
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

func build(window area, stored sizes, panelPaneId string, agentPaneIds []string, cells map[string]area) string {
	if len(agentPaneIds) == 0 {
		return withChecksum(leaf(window, panelPaneId, cells))
	}
	panelWidth := clamp(stored.panelWidth, minimumPanelWidth, maximumPanelWidth)
	panelArea := area{width: panelWidth, height: window.height}
	agentsArea := area{width: window.width - panelWidth - 1, height: window.height, left: panelWidth + 1}
	return withChecksum(container(window, "{", "}", []string{leaf(panelArea, panelPaneId, cells), grid(agentsArea, stored, agentPaneIds, cells)}))
}

func grid(agentsArea area, stored sizes, agentPaneIds []string, cells map[string]area) string {
	rows := inRows(agentPaneIds)
	if len(rows) == 1 {
		return row(agentsArea, rows[0], stored.leftShares[0], cells)
	}
	rowAreas := splitVertically(agentsArea, splitLengths(agentsArea.height, len(rows), stored.topRowShare))
	var rowLayouts []string
	for rowIndex, rowPaneIds := range rows {
		rowLayouts = append(rowLayouts, row(rowAreas[rowIndex], rowPaneIds, stored.leftShares[rowIndex], cells))
	}
	return container(agentsArea, "[", "]", rowLayouts)
}

func row(rowArea area, paneIds []string, leftShare int, cells map[string]area) string {
	if len(paneIds) == 1 {
		return leaf(rowArea, paneIds[0], cells)
	}
	cellAreas := splitHorizontally(rowArea, splitLengths(rowArea.width, len(paneIds), leftShare))
	var cellLayouts []string
	for cellIndex, paneId := range paneIds {
		cellLayouts = append(cellLayouts, leaf(cellAreas[cellIndex], paneId, cells))
	}
	return container(rowArea, "{", "}", cellLayouts)
}

// inRows groups the agents into the grid's rows, agentsPerRow to a row.
func inRows[Item any](items []Item) [][]Item {
	var rows [][]Item
	for start := 0; start < len(items); start += agentsPerRow {
		rows = append(rows, items[start:min(start+agentsPerRow, len(items))])
	}
	return rows
}

func splitHorizontally(parent area, widths []int) []area {
	var areas []area
	left := parent.left
	for _, width := range widths {
		areas = append(areas, area{width: width, height: parent.height, left: left, top: parent.top})
		left += width + 1
	}
	return areas
}

func splitVertically(parent area, heights []int) []area {
	var areas []area
	top := parent.top
	for _, height := range heights {
		areas = append(areas, area{width: parent.width, height: height, left: parent.left, top: top})
		top += height + 1
	}
	return areas
}

// splitLengths divides a length in two at the first part's share, or evenly when there is no share.
func splitLengths(total int, count int, firstShare int) []int {
	if count != 2 || firstShare == 0 {
		return splitLength(total, count)
	}
	available := total - 1
	first := clamp(available*firstShare/1000, 1, available-1)
	return []int{first, available - first}
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
