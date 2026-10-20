package ui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/git"
	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/layout"
	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

const (
	maximumAgents      = 4
	PanelWidth         = 30
	PanelMarkerOption  = "@den-panel"
	windowMarkerOption = "@den"
	// observeEvery is how often den rereads transcripts and git to look for new suggestions.
	observeEvery = 20 * time.Second
)

// Each agent pane remembers what runs in it, so a restarted panel can pick the sessions up again,
// and the pane border can show it.
const (
	paneToolOption       = "@den-tool"
	paneSessionOption    = "@den-session-id"
	paneLaunchedAtOption = "@den-launched-at"
	paneTitleOption      = "@den-title"
	paneFolderOption     = "@den-folder-id"
	paneFolderNameOption = "@den-folder"
	paneNumberOption     = "@den-number"
	paneStatusOption     = "@den-status"
	// paneContextOption is "none" for a session started without context, "empty" when its folders have no notes yet,
	// and "notes" when it started with notes.
	paneContextOption = "@den-context"
	// paneReasonOption says why a waiting agent needs the user, set by its hook.
	paneReasonOption = "@den-reason"
	// paneSeenOption marks a waiting agent the user has focused since it started waiting.
	paneSeenOption = "@den-seen"
	// panelSuggestionsOption counts the suggestions waiting in the dock, so the panel's title can show them.
	panelSuggestionsOption = "@den-suggestions"
)

const (
	// inactiveDarkening is how much darker than the terminal's background the panes without focus get,
	// so the focused one stands out while the others stay readable.
	inactiveDarkening  = 0.3
	inactiveLightening = 0.06
)

// panelSuggestionsTag is the panel title's badge while suggestions wait, in the dock badge's colors.
const panelSuggestionsTag = "#{?#{" + panelSuggestionsOption + "},#[fg=colour234 bg=colour183 bold] ✦ #{" + panelSuggestionsOption + "} for you #[default],}"

// paneLabel reads "claude · ◆ Eval · session title"; before the agent is known, it shows the running command.
const paneLabel = "#{?#{@den-tool},#{@den-tool}#{?#{@den-folder}, · #{?#{==:#{@den-context},notes},◆ ,}#{@den-folder},}#{?#{@den-title}, · #{=50:@den-title},},#{pane_current_command}}"

// windowOptions style the den window. Panes without focus are dimmed, and once den knows the terminal's background,
// tinted a little darker too; the focused pane keeps the terminal's own colors. A waiting agent's border
// carries an amber tag with why it waits, which the dimming never touches.
var windowOptions = map[string]string{
	"window-style": "fg=colour245",
	// terminal, not default: in tmux a default here falls back to window-style, which would dim the focused pane too.
	"window-active-style":      "fg=terminal,bg=terminal",
	"pane-border-style":        "fg=colour238",
	"pane-active-border-style": "fg=colour75",
	"pane-border-lines":        "heavy",
	"pane-border-status":       "top",
	"pane-border-format": tmux.AsPaneTitle("#{?#{@den-panel},#{?pane_active,#[fg=colour75 bold] den #[fg=colour245 nobold]⌥0 back #[default], den #[fg=colour240]⌥0 #[default]}" + panelSuggestionsTag + ",#{?#{==:#{@den-status},waiting}," +
		"#[fg=colour234 bg=colour214 bold] ! ⌥#{@den-number} needs you #[fg=colour214 bg=default nobold]#{?#{@den-reason}, #{@den-reason} ·,} " + paneLabel + " #[default]," +
		"#{?pane_active,#[bg=colour75 fg=colour231 bold] ⌥#{@den-number} " + paneLabel + " #[default], ⌥#{@den-number} " + paneLabel + " }}}"),
	windowMarkerOption: "1",
}

type screen int

const (
	workScreen screen = iota
	welcomeScreen
	chatScreen
	helpScreen
)

type openAgent struct {
	paneId    string
	tool      string
	sessionId string
	folderId  string
	status    string
	// reason says why a waiting agent needs the user.
	reason string
	// seen tells whether the user has focused a waiting agent since it started waiting.
	seen       bool
	context    string
	number     int
	launchedAt time.Time
}

type tickMessage struct{}

// observedMessage carries what den read from transcripts in the background: the tracked sessions,
// and sessions started outside den in the repo's worktrees since den started watching it.
type observedMessage struct {
	digests    map[string]agent.Digest
	discovered []agent.Digest
}

// claimWindow is how close to a den pane's launch a session must start to be that pane's, which den tracks itself.
const claimWindow = 2 * time.Minute

// laptopFile is one of the user's own rule files, which the tool loads in every repo; den shows it beside the repo's context.
type laptopFile struct {
	agent.LaptopFile
	Text   string
	Exists bool
	// Installed tells whether the tool that reads it is installed, so the file may be created when it's missing.
	Installed bool
}

// confirmation is a yes/no question shown in the footer while the list stays visible;
// detail says what saying yes does.
type confirmation struct {
	question string
	detail   string
	accept   func(currentModel *model) tea.Cmd
}

type model struct {
	panelPaneId      string
	windowId         string
	workingDirectory string
	executable       string
	installedAgents  []agent.Adapter
	openAgents       []openAgent
	repository       store.Repository
	theme            theme
	isDark           bool
	colorProfile     colorprofile.Profile
	keys             keyMap
	width            int
	height           int

	// screens is the stack of full-window screens over the work view; the work view shows when it is empty.
	screens []screen
	// zoomedForScreen is set when den zoomed the panel to show a screen, so closing the last one zooms back out.
	zoomedForScreen bool

	listCursor int
	// listOffset is the first tree line on screen, counting the lines that explain a proposal.
	listOffset    int
	previewOffset int
	filter        textinput.Model
	filtering     bool
	picker        *folderPicker
	// repositoryExpanded, inboxCollapsed and archiveExpanded fold the built-in sections of the tree.
	repositoryExpanded bool
	inboxCollapsed     bool
	archiveExpanded    bool
	// newSessionFolderId is the folder the next new session goes into, chosen when n was pressed.
	newSessionFolderId       string
	newSessionWithoutContext bool
	picking                  bool
	pickerCursor             int
	confirming               *confirmation
	prompt                   *textPrompt
	statusMessage            string
	statusIsError            bool
	// newSessionHandoff is the brief the next new session starts with, when the chat proposed picking up earlier work.
	newSessionHandoff string
	// dragging matches the session held with the mouse, dropTargetRow the row under the mouse, or -1.
	dragging      func(listRow) bool
	dropTargetRow int

	dock        dockState
	welcome     *welcomeState
	chat        chatState
	contextPane contextPaneState

	digests map[string]agent.Digest
	// contextTokens holds the size of each folder's context, the top level's under "", with no entry when empty.
	contextTokens map[string]int
	laptopFiles   []laptopFile
	lastObserved  time.Time
	// retitles animates the sessions den just renamed, by session id.
	retitles  map[string]retitleAnimation
	animating bool
	// shownSuggestionCount is the count the panel's title shows, -1 before the first, so tmux is only told when it changes.
	shownSuggestionCount int

	// helperCaller makes den's model calls, or is nil when no agent CLI can, and den uses its local rules only.
	helperCaller   *helper.Caller
	jobQueue       []job
	runningJob     *job
	helperFailures int
	// helperUsage and helperCalls add up what this run's model calls cost.
	helperUsage helper.Usage
	helperCalls int
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
	if err := tmux.HideStatusLineOfDenSession(windowId); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	relayoutCommand := fmt.Sprintf("run-shell -b \"'%s' layout %s >/dev/null 2>&1\"", executable, windowId)
	layoutCommand := func(action string) string {
		return fmt.Sprintf("run-shell -b \"'%s' layout '#{window_id}' %s >/dev/null 2>&1\"", executable, action)
	}
	for _, hook := range []string{"window-layout-changed", "window-resized"} {
		if err := tmux.SetWindowHook(windowId, hook, relayoutCommand); err != nil {
			return err
		}
	}
	if err := tmux.BindPaneDrags(windowMarkerOption, PanelMarkerOption, layout.ResizingOption, layoutCommand("record"), layoutCommand("reset")); err != nil {
		return err
	}
	if err := tmux.BindDenKeys(windowMarkerOption, PanelMarkerOption, layoutCommand("reset")); err != nil {
		return err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	repository, err := store.Load(git.RepositoryRoot(workingDirectory))
	if err != nil {
		return err
	}
	forgotten := repository.ForgetExpiredArchive(time.Now())
	isFirstWatch := repository.TrackedSince.IsZero()
	if isFirstWatch {
		repository.TrackedSince = time.Now()
	}
	if forgotten > 0 || isFirstWatch {
		if err := repository.Save(); err != nil {
			return err
		}
	}
	colorProfile := colorprofile.Detect(os.Stdout, os.Environ())
	var helperCaller *helper.Caller
	if caller, found := helper.Find(); found {
		helperCaller = &caller
	}
	initialModel := model{
		panelPaneId:      panelPaneId,
		windowId:         windowId,
		workingDirectory: workingDirectory,
		executable:       executable,
		installedAgents:  agent.Installed(),
		repository:       repository,
		theme:            newTheme(true, colorProfile),
		isDark:           true,
		colorProfile:     colorProfile,
		keys:             newKeyMap(),
		dropTargetRow:    -1,
		listCursor:       1,
		digests:          map[string]agent.Digest{},
		contextTokens:    map[string]int{},
		retitles:         map[string]retitleAnimation{},
		helperCaller:     helperCaller,
		filter:           newFilterInput(),
		dock:             newDockState(),
		chat:             newChatState(),
		contextPane:      newContextPaneState(),
	}
	initialModel.shownSuggestionCount = -1
	if err := initialModel.refreshAgents(); err != nil {
		return err
	}
	if !repository.Welcomed {
		initialModel.welcome = newWelcomeState()
		initialModel.openScreen(welcomeScreen)
	}
	initialModel.refreshContextTokens()
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
	commands := []tea.Cmd{tick(), tea.RequestBackgroundColor, currentModel.observe()}
	if currentModel.welcome != nil {
		commands = append(commands, currentModel.welcome.spinner.Tick, currentModel.scanPastSessions())
	}
	return tea.Batch(commands...)
}

func (currentModel model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.WindowSizeMsg:
		currentModel.width, currentModel.height = typedMessage.Width, typedMessage.Height
		currentModel.chat.resize(currentModel.width)
		currentModel.scrollToCursor()
		return currentModel, nil
	case tea.BackgroundColorMsg:
		currentModel.isDark = typedMessage.IsDark()
		currentModel.theme = newTheme(currentModel.isDark, currentModel.colorProfile)
		currentModel.reportError(tmux.SetWindowOption(currentModel.windowId, "window-style", inactivePaneStyle(typedMessage.Color, currentModel.isDark)))
		return currentModel, nil
	case tea.ColorProfileMsg:
		currentModel.colorProfile = typedMessage.Profile
		currentModel.theme = newTheme(currentModel.isDark, currentModel.colorProfile)
		return currentModel, nil
	case tickMessage:
		currentModel.keepingSelection(func() {
			currentModel.reportError(currentModel.refreshAgents())
			currentModel.reportError(currentModel.trackNewSessions())
		})
		currentModel.refreshContextTokens()
		currentModel.showSuggestionCount()
		commands := []tea.Cmd{tick(), currentModel.startNextJob()}
		if time.Since(currentModel.lastObserved) > observeEvery {
			currentModel.lastObserved = time.Now()
			commands = append(commands, currentModel.observe())
		}
		return currentModel, tea.Batch(commands...)
	case observedMessage:
		currentModel.keepingSelection(func() { currentModel.applyObservation(typedMessage) })
		return currentModel, currentModel.animateRetitles()
	case animationTickMessage:
		return currentModel, currentModel.stepRetitles()
	case jobDoneMessage:
		return currentModel, currentModel.finishJob(typedMessage)
	case editorClosedMessage:
		return currentModel.afterEditorClosed(typedMessage)
	case pastSessionsMessage:
		return currentModel.updateWelcomeScan(typedMessage)
	}
	switch currentModel.currentScreen() {
	case welcomeScreen:
		return currentModel.updateWelcome(message)
	case chatScreen:
		return currentModel.updateChat(message)
	case helpScreen:
		return currentModel.updateHelp(message)
	}
	return currentModel.updateWork(message)
}

// keepingSelection runs a background change that can add, drop or reorder sessions,
// and keeps the same row selected.
func (currentModel *model) keepingSelection(change func()) {
	var matchesRow func(listRow) bool
	if selected, exists := currentModel.selectedRow(); exists {
		matchesRow = sameItemAs(selected)
	}
	change()
	if matchesRow != nil {
		currentModel.cursorTo(matchesRow)
	}
}

func newFilterInput() textinput.Model {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "filter sessions"
	filter.CharLimit = 80
	return filter
}

func (currentModel model) currentScreen() screen {
	if len(currentModel.screens) == 0 {
		return workScreen
	}
	return currentModel.screens[len(currentModel.screens)-1]
}

// openScreen shows a full-window screen, zooming the panel first when it is the narrow column.
func (currentModel *model) openScreen(next screen) {
	if !currentModel.isWide() && len(currentModel.openAgents) > 0 && !tmux.IsZoomed(currentModel.windowId) {
		if currentModel.reportError(tmux.ToggleZoom(currentModel.panelPaneId)) {
			return
		}
		currentModel.zoomedForScreen = true
	}
	currentModel.screens = append(currentModel.screens, next)
}

func (currentModel *model) closeScreen() {
	if len(currentModel.screens) > 0 {
		currentModel.screens = currentModel.screens[:len(currentModel.screens)-1]
	}
	if len(currentModel.screens) == 0 && currentModel.zoomedForScreen {
		currentModel.zoomedForScreen = false
		if tmux.IsZoomed(currentModel.windowId) {
			currentModel.reportError(tmux.ToggleZoom(currentModel.panelPaneId))
		}
	}
}

// toggleWide zooms the panel to the full window, where the preview shows beside the tree,
// or back to the narrow column beside the agents.
func (currentModel *model) toggleWide() {
	if len(currentModel.openAgents) == 0 {
		currentModel.setStatus("no agents open, so the panel already fills the window")
		return
	}
	currentModel.zoomedForScreen = false
	currentModel.reportError(tmux.ToggleZoom(currentModel.panelPaneId))
}

// jumpToAgent focuses an agent pane; tmux ends the zoom when another pane is selected.
func (currentModel *model) jumpToAgent(key string) {
	agentIndex := int(key[0] - '1')
	if agentIndex >= len(currentModel.openAgents) {
		currentModel.setStatus(fmt.Sprintf("no agent %d open", agentIndex+1))
		return
	}
	currentModel.zoomedForScreen = false
	currentModel.reportError(tmux.SelectPane(currentModel.openAgents[agentIndex].paneId))
}

func (currentModel *model) openAgent(adapter agent.Adapter) {
	folderId := currentModel.newSessionFolderId
	launch, contextState := currentModel.launchFor(folderId, !currentModel.newSessionWithoutContext)
	if handoff := strings.TrimSpace(currentModel.newSessionHandoff); handoff != "" {
		launch.Context = strings.TrimSpace(launch.Context + "\n\n# Handoff from earlier work\n\n" + handoff)
		contextState = "notes"
	}
	command, sessionId := adapter.NewSessionCommand(launch)
	paneId := currentModel.launchInPane(adapter.Name(), sessionId, "", folderId, contextState, currentModel.workingDirectory, command)
	if contextState == "none" {
		currentModel.setStatus("started without context")
	}
	currentModel.cursorTo(func(row listRow) bool {
		return row.open != nil && row.open.paneId == paneId
	})
}

// launchFor builds what den adds to an agent's command line: the merged context, unless left out, and the status hook.
func (currentModel *model) launchFor(folderId string, withContext bool) (agent.Launch, string) {
	launch := agent.Launch{StatusCommand: []string{currentModel.executable, "hook", "status"}}
	if !withContext {
		return launch, "none"
	}
	text, writableDirectories, hasNotes := currentModel.repository.MergedContext(folderId)
	launch.Context, launch.WritableDirectories = text, writableDirectories
	if hasNotes {
		return launch, "notes"
	}
	return launch, "empty"
}

func (currentModel *model) resumeSession(sessionId string) {
	if open := currentModel.openAgentFor(sessionId); open != nil {
		currentModel.focusPane(open.paneId)
		return
	}
	if len(currentModel.openAgents) >= maximumAgents {
		currentModel.setError(fmt.Sprintf("%d agents open, close one first", maximumAgents))
		return
	}
	session := currentModel.repository.FindSession(sessionId)
	adapter, known := agent.ByName(session.Tool)
	if !known {
		currentModel.setError("unknown tool " + session.Tool)
		return
	}
	workingDirectory := session.WorkingDirectory
	if information, err := os.Stat(workingDirectory); err != nil || !information.IsDir() {
		workingDirectory = currentModel.workingDirectory
	}
	launch, contextState := currentModel.launchFor(session.FolderId, !session.WithoutContext)
	currentModel.launchInPane(session.Tool, sessionId, session.Title, session.FolderId, contextState, workingDirectory, adapter.ResumeCommand(sessionId, launch))
	session.LastActivityAt = time.Now()
	if session.Archived {
		currentModel.repository.SetArchived(sessionId, false)
	}
	currentModel.saveRepository()
	currentModel.cursorTo(func(row listRow) bool {
		return row.session != nil && row.session.SessionId == sessionId
	})
}

// focusPane selects an agent pane, leaving the zoomed panel first.
func (currentModel *model) focusPane(paneId string) {
	currentModel.zoomedForScreen = false
	currentModel.reportError(tmux.SelectPane(paneId))
}

// launchInPane splits the last pane so the new agent comes last in pane order, then lets the layout place it.
func (currentModel *model) launchInPane(tool string, sessionId string, title string, folderId string, contextState string, workingDirectory string, command []string) string {
	targetPaneId := currentModel.panelPaneId
	if len(currentModel.openAgents) > 0 {
		targetPaneId = currentModel.openAgents[len(currentModel.openAgents)-1].paneId
	}
	launchedAt := time.Now()
	paneId, err := tmux.SplitPane(targetPaneId, true, workingDirectory, command)
	if currentModel.reportError(err) {
		return ""
	}
	paneOptions := map[string]string{
		paneToolOption:       tool,
		paneLaunchedAtOption: strconv.FormatInt(launchedAt.UnixMilli(), 10),
		paneSessionOption:    sessionId,
		paneTitleOption:      title,
		paneFolderOption:     folderId,
		paneFolderNameOption: currentModel.repository.FolderName(folderId),
		paneContextOption:    contextState,
	}
	for option, value := range paneOptions {
		if value != "" {
			currentModel.reportError(tmux.SetPaneOption(paneId, option, value))
		}
	}
	currentModel.reportError(currentModel.refreshAgents())
	currentModel.reportError(layout.Apply(currentModel.windowId, PanelMarkerOption, PanelWidth))
	return paneId
}

// refreshAgents reads the agents from tmux in pane order, so numbering follows the screen after swaps.
func (currentModel *model) refreshAgents() error {
	windowPanes, err := tmux.ListPanes(currentModel.windowId, paneToolOption, paneSessionOption, paneLaunchedAtOption,
		paneFolderOption, paneStatusOption, paneReasonOption, paneContextOption, paneNumberOption, paneSeenOption, "pane_active")
	if err != nil {
		return err
	}
	currentModel.openAgents = nil
	for _, pane := range windowPanes {
		if pane.Id == currentModel.panelPaneId {
			continue
		}
		number := len(currentModel.openAgents) + 1
		if pane.Options[paneNumberOption] != strconv.Itoa(number) {
			if err := tmux.SetPaneOption(pane.Id, paneNumberOption, strconv.Itoa(number)); err != nil {
				return err
			}
		}
		if err := markSeen(pane); err != nil {
			return err
		}
		launchedAtMilliseconds, _ := strconv.ParseInt(pane.Options[paneLaunchedAtOption], 10, 64)
		currentModel.openAgents = append(currentModel.openAgents, openAgent{
			paneId:     pane.Id,
			tool:       pane.Options[paneToolOption],
			sessionId:  pane.Options[paneSessionOption],
			folderId:   pane.Options[paneFolderOption],
			status:     pane.Options[paneStatusOption],
			reason:     pane.Options[paneReasonOption],
			seen:       pane.Options[paneSeenOption] != "",
			context:    pane.Options[paneContextOption],
			number:     number,
			launchedAt: time.UnixMilli(launchedAtMilliseconds),
		})
	}
	return nil
}

// markSeen notes that the user focused a waiting agent. A closing question needs nothing more, so it turns done;
// a permission keeps the agent waiting, only quieter, until the user answers it.
func markSeen(pane tmux.Pane) error {
	if pane.Options[paneStatusOption] != agent.StateWaiting || pane.Options[paneSeenOption] != "" || pane.Options["pane_active"] != "1" {
		return nil
	}
	if strings.HasPrefix(pane.Options[paneReasonOption], agent.QuestionReasonPrefix) {
		pane.Options[paneStatusOption] = agent.StateDone
		return tmux.SetPaneStatus(pane.Id, agent.StateDone, "")
	}
	pane.Options[paneSeenOption] = "1"
	return tmux.SetPaneOption(pane.Id, paneSeenOption, "1")
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
		// Two Codex panes started close together can match the same transcript; only the first gets it.
		if !found || (open.sessionId == "" && currentModel.repository.FindSession(foundSession.SessionId) != nil) {
			continue
		}
		folderId := open.folderId
		if currentModel.repository.FindFolder(folderId) == nil {
			folderId = ""
		}
		if open.sessionId == "" {
			if err := tmux.SetPaneOption(open.paneId, paneSessionOption, foundSession.SessionId); err != nil {
				return err
			}
			currentModel.openAgents[agentIndex].sessionId = foundSession.SessionId
		}
		now := time.Now()
		title := truncate(oneLine(foundSession.FirstPrompt), 120)
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, store.Session{
			Tool:             open.tool,
			SessionId:        foundSession.SessionId,
			FolderId:         folderId,
			WorkingDirectory: currentModel.workingDirectory,
			Branch:           git.CurrentBranch(currentModel.workingDirectory),
			Title:            title,
			WithoutContext:   open.context == "none",
			CreatedAt:        now,
			LastActivityAt:   now,
		})
		currentModel.saveRepository()
		if err := tmux.SetPaneOption(open.paneId, paneTitleOption, title); err != nil {
			return err
		}
	}
	return nil
}

// observe reads every session's transcript in the background, and finds sessions started outside den in the repo's
// worktrees since den started watching it, leaving out the ones the user deleted and the ones den's own panes are starting.
func (currentModel model) observe() tea.Cmd {
	type sessionReference struct{ tool, sessionId string }
	var references []sessionReference
	known := map[string]bool{}
	for _, session := range currentModel.repository.Sessions {
		references = append(references, sessionReference{tool: session.Tool, sessionId: session.SessionId})
		known[session.SessionId] = true
	}
	for _, sessionId := range currentModel.repository.IgnoredSessionIds {
		known[sessionId] = true
	}
	var unclaimedPanes []openAgent
	for _, open := range currentModel.openAgents {
		known[open.sessionId] = true
		if open.sessionId == "" || currentModel.repository.FindSession(open.sessionId) == nil {
			unclaimedPanes = append(unclaimedPanes, open)
		}
	}
	repositoryPath, trackedSince, installedAgents := currentModel.repository.RepositoryPath, currentModel.repository.TrackedSince, currentModel.installedAgents
	return func() tea.Msg {
		observed := observedMessage{digests: map[string]agent.Digest{}}
		for _, reference := range references {
			if adapter, found := agent.ByName(reference.tool); found {
				if digest, found, err := adapter.Digest(reference.sessionId); err == nil && found {
					observed.digests[reference.sessionId] = digest
				}
			}
		}
		worktrees := git.Worktrees(repositoryPath)
		for _, adapter := range installedAgents {
			pastSessions, _ := adapter.PastSessions(worktrees)
			for _, digest := range pastSessions {
				isClaimed := slices.ContainsFunc(unclaimedPanes, func(open openAgent) bool {
					return open.tool == digest.Tool && digest.StartedAt.Sub(open.launchedAt).Abs() < claimWindow
				})
				if !known[digest.SessionId] && !isClaimed && digest.PromptCount > 0 && digest.StartedAt.After(trackedSince) {
					observed.discovered = append(observed.discovered, digest)
				}
			}
		}
		return observed
	}
}

// trackDiscovered adds sessions started outside den to the Inbox, so den reviews them like its own. It reports how many it added.
func (currentModel *model) trackDiscovered(discovered []agent.Digest) int {
	added := 0
	for _, digest := range discovered {
		if currentModel.repository.FindSession(digest.SessionId) != nil {
			continue
		}
		title := digest.AgentTitle
		if title == "" {
			title = truncate(oneLine(digest.FirstPrompt), 120)
		}
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, store.Session{
			Tool:             digest.Tool,
			SessionId:        digest.SessionId,
			WorkingDirectory: digest.WorkingDirectory,
			Branch:           digest.Branch,
			Title:            title,
			CreatedAt:        time.Now(),
			LastActivityAt:   digest.LastActivityAt,
		})
		currentModel.digests[digest.SessionId] = digest
		added++
	}
	return added
}

// applyObservation keeps session details current from their transcripts, reviews sessions when due,
// archives the ones left untouched, and runs the suggestion rules.
func (currentModel *model) applyObservation(observed observedMessage) {
	currentModel.digests = observed.digests
	changed := false
	if added := currentModel.trackDiscovered(observed.discovered); added > 0 {
		changed = true
		currentModel.setStatus(fmt.Sprintf("picked up %s started outside den · in the Inbox", countedSessions(added)))
	}
	for index := range currentModel.repository.Sessions {
		session := &currentModel.repository.Sessions[index]
		digest, found := observed.digests[session.SessionId]
		if !found {
			continue
		}
		if digest.Branch != "" && digest.Branch != session.Branch {
			session.Branch, changed = digest.Branch, true
		}
		if digest.WorkingDirectory != "" && digest.WorkingDirectory != session.WorkingDirectory {
			session.WorkingDirectory, changed = digest.WorkingDirectory, true
		}
		if digest.LastActivityAt.After(session.LastActivityAt) {
			session.LastActivityAt, changed = digest.LastActivityAt, true
		}
		if currentModel.helperCaller != nil {
			changed = currentModel.queueReviewIfDue(session, digest) || changed
		} else if title, due := brain.Retitle(*session, digest); due {
			session.TitledAtPrompt, changed = digest.PromptCount, true
			if title != "" {
				currentModel.retitle(session, title)
			}
		}
	}
	now := time.Now()
	if archived := currentModel.repository.ArchiveIdle(now, currentModel.openSessionIds()); archived > 0 {
		changed = true
		currentModel.setStatus(fmt.Sprintf("archived %s untouched for %d days · still resumable", countedSessions(archived), archiveAfterDays()))
	}
	if forgotten := currentModel.repository.ForgetExpiredArchive(now); forgotten > 0 {
		changed = true
		currentModel.setStatus(fmt.Sprintf("forgot %s archived over %d days ago", countedSessions(forgotten), archiveKeepDays()))
	}
	if currentModel.runRules() || changed {
		currentModel.saveRepository()
	}
}

func (currentModel model) openSessionIds() map[string]bool {
	openSessionIds := map[string]bool{}
	for _, open := range currentModel.openAgents {
		openSessionIds[open.sessionId] = true
	}
	return openSessionIds
}

// runRules offers what the local rules find in the current state; it reads nothing from disk, so it runs after every change.
// It reports whether the queue changed.
func (currentModel *model) runRules() bool {
	observation := brain.Observation{
		Repository:    &currentModel.repository,
		Digests:       currentModel.digests,
		ContextTokens: currentModel.contextTokens,
	}
	changed := false
	for _, suggestion := range brain.Suggest(observation) {
		if currentModel.repository.Offer(suggestion) {
			changed = true
		}
	}
	return changed
}

// refreshContextTokens notes which folders have context, linked or den's notes, and how big each is,
// and rereads the laptop files.
func (currentModel *model) refreshContextTokens() {
	contextTokens := map[string]int{}
	folderIds := []string{""}
	for _, folder := range currentModel.repository.Folders {
		folderIds = append(folderIds, folder.Id)
	}
	for _, folderId := range folderIds {
		filePath, err := currentModel.repository.FolderContextPath(folderId)
		if err != nil {
			continue
		}
		if information, err := os.Stat(filePath); err == nil && information.Size() > 1 {
			contextTokens[folderId] = int(information.Size()+3) / 4
		}
	}
	currentModel.contextTokens = contextTokens
	installed := map[string]bool{}
	for _, adapter := range currentModel.installedAgents {
		installed[adapter.Name()] = true
	}
	currentModel.laptopFiles = nil
	for _, file := range agent.LaptopFiles() {
		_, err := os.Stat(file.Path)
		currentModel.laptopFiles = append(currentModel.laptopFiles, laptopFile{LaptopFile: file, Text: store.ReadText(file.Path), Exists: err == nil, Installed: installed[file.Tool]})
	}
}

// saveRepository keeps sessions ordered by last activity, newest first, and writes folders and sessions to disk.
// The rules run first, so suggestions follow every change right away.
func (currentModel *model) saveRepository() {
	currentModel.runRules()
	slices.SortStableFunc(currentModel.repository.Sessions, func(first store.Session, second store.Session) int {
		return second.LastActivityAt.Compare(first.LastActivityAt)
	})
	currentModel.reportError(currentModel.repository.Save())
}

func (currentModel *model) openAgentFor(sessionId string) *openAgent {
	for agentIndex := range currentModel.openAgents {
		if currentModel.openAgents[agentIndex].sessionId == sessionId {
			return &currentModel.openAgents[agentIndex]
		}
	}
	return nil
}

// reportError shows the error in the footer and tells whether there was one.
func (currentModel *model) reportError(err error) bool {
	if err != nil {
		currentModel.setError(err.Error())
	}
	return err != nil
}

func (currentModel *model) setError(message string) {
	currentModel.statusMessage, currentModel.statusIsError = message, true
}

func (currentModel *model) setStatus(message string) {
	currentModel.statusMessage, currentModel.statusIsError = message, false
}

func (currentModel *model) clearStatus() {
	currentModel.statusMessage, currentModel.statusIsError = "", false
}

// inactivePaneStyle dims the panes without focus: dimmer text on the terminal's own background, a little darker
// on a dark terminal and a little grayer on a light one, so the focused pane stands out in any theme.
func inactivePaneStyle(background color.Color, isDark bool) string {
	red, green, blue, _ := background.RGBA()
	shade := func(channel uint32) uint32 {
		value := float64(channel >> 8)
		if isDark {
			return uint32(value * (1 - inactiveDarkening))
		}
		return uint32(value * (1 - inactiveLightening))
	}
	foreground := "colour245"
	if !isDark {
		foreground = "colour243"
	}
	return fmt.Sprintf("fg=%s,bg=#%02x%02x%02x", foreground, shade(red), shade(green), shade(blue))
}

func (currentModel model) repositoryName() string {
	return filepath.Base(currentModel.repository.RepositoryPath)
}

// showSuggestionCount puts the number of waiting suggestions in the panel's title, or takes it away when none wait.
func (currentModel *model) showSuggestionCount() {
	count := len(currentModel.dockItems())
	if count == currentModel.shownSuggestionCount {
		return
	}
	currentModel.shownSuggestionCount = count
	if count == 0 {
		currentModel.reportError(tmux.UnsetPaneOption(currentModel.panelPaneId, panelSuggestionsOption))
		return
	}
	currentModel.reportError(tmux.SetPaneOption(currentModel.panelPaneId, panelSuggestionsOption, strconv.Itoa(count)))
}
