package ui

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
	paneFolderOption     = "@den-folder-id"
)

// paneLabel shows "tool · session title" once a session has a title, and the running command before that.
const paneLabel = "#{?#{@den-title},#{@den-tool} · #{=60:@den-title},#{pane_current_command}}"

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
	folderId   string
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
	listCursor       int
	// newSessionFolderId is the folder the next new session goes into, chosen when n was pressed.
	newSessionFolderId string
	picking            bool
	pickerCursor       int
	confirmingQuit     bool
	deleting           *listRow
	prompt             *textPrompt
	statusMessage      string
	// grabbed matches the session being carried with the arrows; grabbedFromFolderId restores it on esc.
	grabbed             func(listRow) bool
	grabbedFromFolderId string
	// dragging matches the session held with the mouse, dropTargetRow the row under the mouse, or -1.
	dragging      func(listRow) bool
	dropTargetRow int
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
		dropTargetRow:    -1,
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
		if currentModel.prompt != nil {
			return currentModel.updatePrompt(typedMessage)
		}
		if currentModel.confirmingQuit {
			return currentModel.updateQuitConfirmation(typedMessage.String())
		}
		if currentModel.deleting != nil {
			return currentModel.updateDeleteConfirmation(typedMessage.String())
		}
		if currentModel.picking {
			return currentModel.updatePicker(typedMessage.String())
		}
		if currentModel.grabbed != nil {
			return currentModel.updateGrab(typedMessage.String())
		}
		return currentModel.updateMain(typedMessage.String())
	case tea.MouseMsg:
		if currentModel.prompt == nil && currentModel.deleting == nil && !currentModel.picking && !currentModel.confirmingQuit {
			return currentModel.updateMouse(typedMessage)
		}
	}
	if currentModel.prompt != nil {
		return currentModel.updatePrompt(message)
	}
	return currentModel, nil
}

func (currentModel model) updateMain(key string) (tea.Model, tea.Cmd) {
	currentModel.statusMessage = ""
	switch key {
	case "q", "ctrl+c":
		currentModel.confirmingQuit = true
	case "n":
		currentModel.newSessionFolderId = currentModel.folderForNewItems()
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
			if selected.folder != nil {
				currentModel.toggleFolder(selected.folder.Id)
			} else if selected.open != nil {
				currentModel.reportError(tmux.SelectPane(selected.open.paneId))
			} else {
				currentModel.resumeSession(selected.session.SessionId)
			}
		}
	case "space", "m":
		currentModel.startGrab()
	// Shortcuts for terminals that send Shift+arrows; Terminal.app sends them as plain arrows, so grab mode is the main way.
	case "shift+down", "J":
		currentModel.moveSelected(1)
	case "shift+up", "K":
		currentModel.moveSelected(-1)
	case "f":
		return currentModel, currentModel.askForNewFolder()
	case "r":
		return currentModel, currentModel.askForRename()
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
	if deleting.folder != nil {
		currentModel.repository.DeleteFolder(deleting.folder.Id)
		currentModel.saveRepository()
	}
	if deleting.open != nil {
		currentModel.reportError(tmux.KillPane(deleting.open.paneId))
	}
	if deleting.session != nil {
		currentModel.repository.Sessions = slices.DeleteFunc(currentModel.repository.Sessions, func(saved store.Session) bool {
			return saved.SessionId == deleting.session.SessionId
		})
		currentModel.saveRepository()
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
	paneId := currentModel.launchInPane(adapter.Name(), sessionId, "", currentModel.newSessionFolderId, command)
	currentModel.cursorTo(func(row listRow) bool {
		return row.open != nil && row.open.paneId == paneId
	})
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
	currentModel.launchInPane(session.Tool, sessionId, session.Title, session.FolderId, adapter.ResumeCommand(sessionId))
	session.LastActivityAt = time.Now()
	currentModel.saveRepository()
	currentModel.cursorTo(func(row listRow) bool {
		return row.session != nil && row.session.SessionId == sessionId
	})
}

// launchInPane splits the last pane so the new agent comes last in pane order, then lets the layout place it.
func (currentModel *model) launchInPane(tool string, sessionId string, title string, folderId string, command []string) string {
	targetPaneId := currentModel.panelPaneId
	if len(currentModel.openAgents) > 0 {
		targetPaneId = currentModel.openAgents[len(currentModel.openAgents)-1].paneId
	}
	launchedAt := time.Now()
	paneId, err := tmux.SplitPane(targetPaneId, true, currentModel.workingDirectory, command)
	if err != nil {
		currentModel.reportError(err)
		return ""
	}
	currentModel.reportError(tmux.SetPaneOption(paneId, paneToolOption, tool))
	currentModel.reportError(tmux.SetPaneOption(paneId, paneLaunchedAtOption, strconv.FormatInt(launchedAt.UnixMilli(), 10)))
	if sessionId != "" {
		currentModel.reportError(tmux.SetPaneOption(paneId, paneSessionOption, sessionId))
	}
	if title != "" {
		currentModel.reportError(tmux.SetPaneOption(paneId, paneTitleOption, title))
	}
	if folderId != "" {
		currentModel.reportError(tmux.SetPaneOption(paneId, paneFolderOption, folderId))
	}
	currentModel.reportError(currentModel.refreshAgents())
	currentModel.reportError(layout.Apply(currentModel.windowId, PanelMarkerOption, PanelWidth))
	return paneId
}

// refreshAgents reads the agents from tmux in pane order, so numbering follows the screen after swaps.
func (currentModel *model) refreshAgents() error {
	windowPanes, err := tmux.ListPanes(currentModel.windowId, paneToolOption, paneSessionOption, paneLaunchedAtOption, paneFolderOption)
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
			folderId:   pane.Options[paneFolderOption],
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
			FolderId:       open.folderId,
			Title:          title,
			CreatedAt:      now,
			LastActivityAt: now,
		})
		currentModel.saveRepository()
		if err := tmux.SetPaneOption(open.paneId, paneTitleOption, title); err != nil {
			return err
		}
	}
	return nil
}

// saveRepository keeps sessions ordered by last activity, newest first, and writes folders and sessions to disk.
func (currentModel *model) saveRepository() {
	slices.SortStableFunc(currentModel.repository.Sessions, func(first store.Session, second store.Session) int {
		return second.LastActivityAt.Compare(first.LastActivityAt)
	})
	currentModel.reportError(currentModel.repository.Save())
}

func (currentModel *model) reportError(err error) {
	if err != nil {
		currentModel.statusMessage = err.Error()
	}
}
