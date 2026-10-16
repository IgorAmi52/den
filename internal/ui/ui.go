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
	paneTitleOption      = "@den-title"
)

// paneLabel shows "tool · session title" once a session has a title, and the running command before that.
const paneLabel = "#{?#{@den-title},#{@den-tool} · #{=60:@den-title},#{pane_current_command}}"

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
	"pane-border-format":       "#{?pane_active,#[bg=colour75 fg=colour231 bold] " + paneLabel + " #[default], " + paneLabel + " }",
	windowMarkerOption:         "1",
}

type openAgent struct {
	paneId     string
	tool       string
	sessionId  string
	launchedAt time.Time
}

// listRow is one line of the panel list: a saved session, a just-opened agent without a first prompt yet, or both.
type listRow struct {
	session *store.Session
	open    *openAgent
}

type tickMessage struct{}

type model struct {
	panelPaneId      string
	windowId         string
	workingDirectory string
	installedAgents  []agent.Adapter
	openAgents       []openAgent
	repository       store.Repository
	listCursor       int
	picking          bool
	pickerCursor     int
	confirmingQuit   bool
	deleting         *listRow
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
		if currentModel.deleting != nil {
			return currentModel.updateDeleteConfirmation(typedMessage.String())
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
		currentModel.listCursor = max(currentModel.listCursor-1, 0)
	case "down", "j":
		currentModel.listCursor = min(currentModel.listCursor+1, len(currentModel.rows())-1)
	case "enter":
		if selected, exists := currentModel.selectedRow(); exists {
			if selected.open != nil {
				currentModel.reportError(tmux.SelectPane(selected.open.paneId))
			} else {
				currentModel.resumeSession(selected.session.SessionId)
			}
		}
	case "d":
		if selected, exists := currentModel.selectedRow(); exists {
			currentModel.deleting = &selected
		}
	}
	return currentModel, nil
}

// updateDeleteConfirmation forgets the session in den and closes its pane; the agent keeps its own history.
func (currentModel model) updateDeleteConfirmation(key string) (tea.Model, tea.Cmd) {
	deleting := currentModel.deleting
	currentModel.deleting = nil
	if key != "y" {
		return currentModel, nil
	}
	if deleting.open != nil {
		currentModel.reportError(tmux.KillPane(deleting.open.paneId))
	}
	if deleting.session != nil {
		currentModel.repository.Sessions = slices.DeleteFunc(currentModel.repository.Sessions, func(saved store.Session) bool {
			return saved.SessionId == deleting.session.SessionId
		})
		currentModel.saveSessions()
	}
	currentModel.reportError(currentModel.refreshAgents())
	currentModel.listCursor = max(min(currentModel.listCursor, len(currentModel.rows())-1), 0)
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
	currentModel.launchInPane(adapter.Name(), sessionId, "", command)
	currentModel.listCursor = 0
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
	currentModel.launchInPane(session.Tool, sessionId, session.Title, adapter.ResumeCommand(sessionId))
	session.LastActivityAt = time.Now()
	currentModel.saveSessions()
	currentModel.listCursor = slices.IndexFunc(currentModel.rows(), func(row listRow) bool {
		return row.session != nil && row.session.SessionId == sessionId
	})
}

// launchInPane splits the last pane so the new agent comes last in pane order, then lets the layout place it.
func (currentModel *model) launchInPane(tool string, sessionId string, title string, command []string) {
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
	if title != "" {
		currentModel.reportError(tmux.SetPaneOption(paneId, paneTitleOption, title))
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
		title := strings.Join(strings.Fields(foundSession.FirstPrompt), " ")
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, store.Session{
			Tool:           open.tool,
			SessionId:      foundSession.SessionId,
			Title:          title,
			CreatedAt:      now,
			LastActivityAt: now,
		})
		currentModel.saveSessions()
		if err := tmux.SetPaneOption(open.paneId, paneTitleOption, title); err != nil {
			return err
		}
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

// rows lists just-opened agents without a first prompt first, then saved sessions, newest first.
func (currentModel model) rows() []listRow {
	var rows []listRow
	for agentIndex := range currentModel.openAgents {
		open := &currentModel.openAgents[agentIndex]
		if open.sessionId == "" || currentModel.repository.FindSession(open.sessionId) == nil {
			rows = append(rows, listRow{open: open})
		}
	}
	for sessionIndex := range currentModel.repository.Sessions {
		session := &currentModel.repository.Sessions[sessionIndex]
		rows = append(rows, listRow{session: session, open: currentModel.openAgentFor(session.SessionId)})
	}
	return rows
}

func (currentModel model) selectedRow() (listRow, bool) {
	rows := currentModel.rows()
	if currentModel.listCursor >= len(rows) {
		return listRow{}, false
	}
	return rows[currentModel.listCursor], true
}

func (currentModel model) openAgentFor(sessionId string) *openAgent {
	for agentIndex := range currentModel.openAgents {
		if currentModel.openAgents[agentIndex].sessionId == sessionId {
			return &currentModel.openAgents[agentIndex]
		}
	}
	return nil
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
	} else if currentModel.deleting != nil {
		currentModel.writeDeleteConfirmation(&content)
	} else {
		currentModel.writeList(&content)
		content.WriteString("\n" + hintStyle.Render(currentModel.footer()) + "\n")
	}
	if currentModel.statusMessage != "" {
		content.WriteString("\n" + currentModel.statusMessage + "\n")
	}
	return tea.NewView(content.String())
}

func (currentModel model) writeList(content *strings.Builder) {
	rows := currentModel.rows()
	if len(rows) == 0 {
		content.WriteString(hintStyle.Render("no sessions yet") + "\n")
	}
	titleWidth := PanelWidth - 8
	for rowIndex, row := range rows {
		openMarker := " "
		if row.open != nil {
			openMarker = "●"
		}
		var line string
		if row.session == nil {
			line = fmt.Sprintf("%s %s", openMarker, hintStyle.Render("new "+row.open.tool))
		} else {
			line = fmt.Sprintf("%s %-*s %s", openMarker, titleWidth, truncate(row.session.Title, titleWidth), relativeAge(row.session.LastActivityAt))
		}
		if rowIndex == currentModel.listCursor {
			content.WriteString(selectedStyle.Render(">"+line) + "\n")
		} else {
			content.WriteString(" " + line + "\n")
		}
	}
}

// footer only offers the keys that do something for the selected row.
func (currentModel model) footer() string {
	selected, exists := currentModel.selectedRow()
	if !exists {
		return "n new · q quit\nclick focus · drag title swap"
	}
	enterAction := "resume"
	if selected.open != nil {
		enterAction = "focus"
	}
	return "enter " + enterAction + " · d delete\nn new · q quit\nclick focus · drag title swap"
}

func (currentModel model) writeDeleteConfirmation(content *strings.Builder) {
	deleting := currentModel.deleting
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
