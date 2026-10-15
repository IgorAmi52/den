package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/layout"
	"github.com/IgorAmi52/den/internal/tmux"
)

const (
	maximumAgents      = 4
	PanelWidth         = 30
	PanelMarkerOption  = "@den-panel"
	windowMarkerOption = "@den"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	hintStyle     = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
)

var windowOptions = map[string]string{
	"window-style":             "fg=colour247",
	"window-active-style":      "fg=default",
	"pane-border-style":        "fg=colour238",
	"pane-active-border-style": "fg=colour75",
	"pane-border-lines":        "heavy",
	"pane-border-status":       "top",
	"pane-border-format":       "#{?pane_active,#[bg=colour75 fg=colour231 bold] #{pane_current_command} #[default], #{pane_current_command} }",
	windowMarkerOption:         "1",
}

type openAgent struct {
	paneId    string
	agentName string
}

type tickMessage struct{}

type model struct {
	panelPaneId      string
	windowId         string
	workingDirectory string
	installedAgents  []agent.Adapter
	openAgents       []openAgent
	picking          bool
	pickerCursor     int
	confirmingQuit   bool
	statusMessage    string
}

func Run() error {
	panelPaneId := tmux.CurrentPane()
	windowId, err := tmux.WindowOf(panelPaneId)
	if err != nil {
		return err
	}
	for option, value := range windowOptions {
		if err := tmux.SetWindowOption(windowId, option, value); err != nil {
			return err
		}
	}
	if err := tmux.SetPaneOption(panelPaneId, PanelMarkerOption, "1"); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	relayoutCommand := fmt.Sprintf("run-shell -b \"'%s' layout %s >/dev/null 2>&1\"", executable, windowId)
	for _, hook := range []string{"window-layout-changed", "window-resized"} {
		if err := tmux.SetWindowHook(windowId, hook, relayoutCommand); err != nil {
			return err
		}
	}
	if err := tmux.BindTitleDragToSwapPanes(windowMarkerOption, PanelMarkerOption); err != nil {
		return err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	initialModel := model{
		panelPaneId:      panelPaneId,
		windowId:         windowId,
		workingDirectory: workingDirectory,
		installedAgents:  agent.Installed(),
	}
	if err := initialModel.refreshAgents(); err != nil {
		return err
	}
	if err := layout.Apply(windowId, PanelMarkerOption, PanelWidth); err != nil {
		return err
	}
	_, err = tea.NewProgram(initialModel).Run()
	return err
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return tickMessage{}
	})
}

func (currentModel model) Init() tea.Cmd {
	return tick()
}

func (currentModel model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tickMessage:
		currentModel.reportError(currentModel.refreshAgents())
		return currentModel, tick()
	case tea.KeyPressMsg:
		if currentModel.confirmingQuit {
			return currentModel.updateQuitConfirmation(typedMessage.String())
		}
		if currentModel.picking {
			return currentModel.updatePicker(typedMessage.String())
		}
		return currentModel.updateMain(typedMessage.String())
	}
	return currentModel, nil
}

func (currentModel model) updateMain(key string) (tea.Model, tea.Cmd) {
	currentModel.statusMessage = ""
	switch key {
	case "q", "ctrl+c":
		currentModel.confirmingQuit = true
	case "n":
		if len(currentModel.openAgents) >= maximumAgents {
			currentModel.statusMessage = fmt.Sprintf("max %d agents", maximumAgents)
		} else if len(currentModel.installedAgents) == 1 {
			currentModel.openAgent(currentModel.installedAgents[0])
		} else {
			currentModel.picking = true
			currentModel.pickerCursor = 0
		}
	case "1", "2", "3", "4":
		agentIndex := int(key[0] - '1')
		if agentIndex < len(currentModel.openAgents) {
			currentModel.reportError(tmux.SelectPane(currentModel.openAgents[agentIndex].paneId))
		}
	}
	return currentModel, nil
}

func (currentModel model) updateQuitConfirmation(key string) (tea.Model, tea.Cmd) {
	currentModel.confirmingQuit = false
	if key != "y" {
		return currentModel, nil
	}
	if err := tmux.KillWindow(currentModel.windowId); err != nil {
		currentModel.reportError(err)
		return currentModel, nil
	}
	return currentModel, tea.Quit
}

func (currentModel model) updatePicker(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		currentModel.pickerCursor = max(currentModel.pickerCursor-1, 0)
	case "down", "j":
		currentModel.pickerCursor = min(currentModel.pickerCursor+1, len(currentModel.installedAgents)-1)
	case "enter":
		currentModel.picking = false
		currentModel.openAgent(currentModel.installedAgents[currentModel.pickerCursor])
	case "esc", "q":
		currentModel.picking = false
	}
	return currentModel, nil
}

// openAgent splits the last pane so the new agent comes last in pane order, then lets the layout place it.
func (currentModel *model) openAgent(adapter agent.Adapter) {
	targetPaneId := currentModel.panelPaneId
	if len(currentModel.openAgents) > 0 {
		targetPaneId = currentModel.openAgents[len(currentModel.openAgents)-1].paneId
	}
	if _, err := tmux.SplitPane(targetPaneId, true, currentModel.workingDirectory, adapter.NewSessionCommand()); err != nil {
		currentModel.reportError(err)
		return
	}
	currentModel.reportError(currentModel.refreshAgents())
	currentModel.reportError(layout.Apply(currentModel.windowId, PanelMarkerOption, PanelWidth))
}

// refreshAgents reads the agents from tmux in pane order, so numbering follows the screen after swaps.
func (currentModel *model) refreshAgents() error {
	agentPanes, err := currentModel.agentPanes()
	if err != nil {
		return err
	}
	currentModel.openAgents = nil
	for _, pane := range agentPanes {
		currentModel.openAgents = append(currentModel.openAgents, openAgent{paneId: pane.Id, agentName: pane.Command})
	}
	return nil
}

func (currentModel *model) agentPanes() ([]tmux.Pane, error) {
	windowPanes, err := tmux.ListPanes(currentModel.windowId)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(windowPanes, func(pane tmux.Pane) bool {
		return pane.Id == currentModel.panelPaneId
	}), nil
}

func (currentModel *model) reportError(err error) {
	if err != nil {
		currentModel.statusMessage = err.Error()
	}
}

func (currentModel model) View() tea.View {
	var content strings.Builder
	content.WriteString(titleStyle.Render("den") + "\n\n")
	if currentModel.confirmingQuit {
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
	} else {
		for agentIndex, open := range currentModel.openAgents {
			content.WriteString(fmt.Sprintf("%d %s\n", agentIndex+1, open.agentName))
		}
		if len(currentModel.openAgents) == 0 {
			content.WriteString(hintStyle.Render("no agents yet") + "\n")
		}
		content.WriteString("\n" + hintStyle.Render("n new · q quit\n1-4 focus agent\nclick a pane to focus\ndrag a title to swap") + "\n")
	}
	if currentModel.statusMessage != "" {
		content.WriteString("\n" + currentModel.statusMessage + "\n")
	}
	return tea.NewView(content.String())
}
