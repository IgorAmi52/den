package ui

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/layout"
	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

const (
	maximumAgents      = 4
	PanelWidth         = 30
	PanelMarkerOption  = "@den-panel"
	windowMarkerOption = "@den"
)

// Each agent pane remembers what runs in it, so a restarted panel can pick the sessions up again.
const (
	paneToolOption       = "@den-tool"
	paneSessionOption    = "@den-session-id"
	paneLaunchedAtOption = "@den-launched-at"
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
	paneId     string
	tool       string
	sessionId  string
	launchedAt time.Time
}

type tickMessage struct{}

type model struct {
	panelPaneId      string
	windowId         string
	workingDirectory string
	installedAgents  []agent.Adapter
	openAgents       []openAgent
	repository       store.Repository
	sessionCursor    int
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
	repository, err := store.Load(workingDirectory)
	if err != nil {
		return err
	}
	initialModel := model{
		panelPaneId:      panelPaneId,
		windowId:         windowId,
		workingDirectory: workingDirectory,
		installedAgents:  agent.Installed(),
		repository:       repository,
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
		currentModel.reportError(currentModel.trackNewSessions())
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
	case "up", "k":
		currentModel.sessionCursor = max(currentModel.sessionCursor-1, 0)
	case "down", "j":
		currentModel.sessionCursor = max(min(currentModel.sessionCursor+1, len(currentModel.repository.Sessions)-1), 0)
	case "enter":
		if currentModel.sessionCursor < len(currentModel.repository.Sessions) {
			currentModel.resumeSession(currentModel.repository.Sessions[currentModel.sessionCursor].SessionId)
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

func (currentModel *model) openAgent(adapter agent.Adapter) {
	command, sessionId := adapter.NewSessionCommand()
	currentModel.launchInPane(adapter.Name(), sessionId, command)
}

func (currentModel *model) resumeSession(sessionId string) {
	for _, open := range currentModel.openAgents {
		if open.sessionId == sessionId {
			currentModel.reportError(tmux.SelectPane(open.paneId))
			return
		}
	}
	if len(currentModel.openAgents) >= maximumAgents {
		currentModel.statusMessage = fmt.Sprintf("max %d agents, close one first", maximumAgents)
		return
	}
	session := currentModel.repository.FindSession(sessionId)
	adapter, known := agent.ByName(session.Tool)
	if !known {
		currentModel.statusMessage = "unknown tool " + session.Tool
		return
	}
	currentModel.launchInPane(session.Tool, sessionId, adapter.ResumeCommand(sessionId))
	session.LastActivityAt = time.Now()
	currentModel.saveSessions()
	currentModel.sessionCursor = slices.IndexFunc(currentModel.repository.Sessions, func(saved store.Session) bool {
		return saved.SessionId == sessionId
	})
}

// launchInPane splits the last pane so the new agent comes last in pane order, then lets the layout place it.
func (currentModel *model) launchInPane(tool string, sessionId string, command []string) {
	targetPaneId := currentModel.panelPaneId
	if len(currentModel.openAgents) > 0 {
		targetPaneId = currentModel.openAgents[len(currentModel.openAgents)-1].paneId
	}
	launchedAt := time.Now()
	paneId, err := tmux.SplitPane(targetPaneId, true, currentModel.workingDirectory, command)
	if err != nil {
		currentModel.reportError(err)
		return
	}
	currentModel.reportError(tmux.SetPaneOption(paneId, paneToolOption, tool))
	currentModel.reportError(tmux.SetPaneOption(paneId, paneLaunchedAtOption, strconv.FormatInt(launchedAt.UnixMilli(), 10)))
	if sessionId != "" {
		currentModel.reportError(tmux.SetPaneOption(paneId, paneSessionOption, sessionId))
	}
	currentModel.reportError(currentModel.refreshAgents())
	currentModel.reportError(layout.Apply(currentModel.windowId, PanelMarkerOption, PanelWidth))
}

// refreshAgents reads the agents from tmux in pane order, so numbering follows the screen after swaps.
func (currentModel *model) refreshAgents() error {
	windowPanes, err := tmux.ListPanes(currentModel.windowId, paneToolOption, paneSessionOption, paneLaunchedAtOption)
	if err != nil {
		return err
	}
	currentModel.openAgents = nil
	for _, pane := range windowPanes {
		if pane.Id == currentModel.panelPaneId {
			continue
		}
		launchedAtMilliseconds, _ := strconv.ParseInt(pane.Options[paneLaunchedAtOption], 10, 64)
		currentModel.openAgents = append(currentModel.openAgents, openAgent{
			paneId:     pane.Id,
			tool:       pane.Options[paneToolOption],
			sessionId:  pane.Options[paneSessionOption],
			launchedAt: time.UnixMilli(launchedAtMilliseconds),
		})
	}
	return nil
}

// trackNewSessions saves sessions of open panes once their first prompt exists; before that there is nothing to resume.
func (currentModel *model) trackNewSessions() error {
	for agentIndex, open := range currentModel.openAgents {
		adapter, known := agent.ByName(open.tool)
		if !known || (open.sessionId != "" && currentModel.repository.FindSession(open.sessionId) != nil) {
			continue
		}
		foundSession, found, err := adapter.FindSession(open.sessionId, currentModel.workingDirectory, open.launchedAt)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if open.sessionId == "" {
			if err := tmux.SetPaneOption(open.paneId, paneSessionOption, foundSession.SessionId); err != nil {
				return err
			}
			currentModel.openAgents[agentIndex].sessionId = foundSession.SessionId
		}
		now := time.Now()
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, store.Session{
			Tool:           open.tool,
			SessionId:      foundSession.SessionId,
			Title:          strings.Join(strings.Fields(foundSession.FirstPrompt), " "),
			CreatedAt:      now,
			LastActivityAt: now,
		})
		currentModel.saveSessions()
	}
	return nil
}

// saveSessions keeps the list ordered by last activity, newest first, and writes it to disk.
func (currentModel *model) saveSessions() {
	slices.SortStableFunc(currentModel.repository.Sessions, func(first store.Session, second store.Session) int {
		return second.LastActivityAt.Compare(first.LastActivityAt)
	})
	currentModel.reportError(currentModel.repository.Save())
}

func (currentModel model) openAgentFor(sessionId string) bool {
	return slices.ContainsFunc(currentModel.openAgents, func(open openAgent) bool {
		return open.sessionId == sessionId
	})
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
		currentModel.writeOpenAgents(&content)
		content.WriteString("\n" + titleStyle.Render("sessions") + "\n")
		currentModel.writeSessions(&content)
		content.WriteString("\n" + hintStyle.Render("n new · enter resume · q quit\nclick focus · drag title swap") + "\n")
	}
	if currentModel.statusMessage != "" {
		content.WriteString("\n" + currentModel.statusMessage + "\n")
	}
	return tea.NewView(content.String())
}

func (currentModel model) writeOpenAgents(content *strings.Builder) {
	if len(currentModel.openAgents) == 0 {
		content.WriteString(hintStyle.Render("no agents open") + "\n")
	}
	for agentIndex, open := range currentModel.openAgents {
		label := hintStyle.Render("new")
		if session := currentModel.repository.FindSession(open.sessionId); session != nil {
			label = truncate(session.Title, PanelWidth-10)
		}
		content.WriteString(fmt.Sprintf("%d %-7s %s\n", agentIndex+1, open.tool, label))
	}
}

func (currentModel model) writeSessions(content *strings.Builder) {
	if len(currentModel.repository.Sessions) == 0 {
		content.WriteString(hintStyle.Render("none yet") + "\n")
	}
	for sessionIndex, session := range currentModel.repository.Sessions {
		openMarker := " "
		if currentModel.openAgentFor(session.SessionId) {
			openMarker = "●"
		}
		age := relativeAge(session.LastActivityAt)
		line := fmt.Sprintf("%s %-*s %s", openMarker, PanelWidth-8, truncate(session.Title, PanelWidth-8), age)
		if sessionIndex == currentModel.sessionCursor {
			content.WriteString(selectedStyle.Render(">"+line) + "\n")
		} else {
			content.WriteString(" " + line + "\n")
		}
	}
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
