package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/git"
	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/terminal"
	"github.com/IgorAmi52/den/internal/tmux"
)

const (
	welcomeCardWidth   = 78
	recentCommitsCount = 40
	draftPreviewLines  = 9
)

type welcomeStep int

const (
	scanningStep welcomeStep = iota
	foldersStep
	contextsStep
	// keysStep teaches the keys and offers to turn on Terminal.app's Option key, which den's ⌥ keys need.
	keysStep
)

// welcomeState is the first-run review: den reads the repo's past sessions and commits,
// then proposes folders and their contexts, and the user picks what to keep.
type welcomeState struct {
	step     welcomeStep
	spinner  spinner.Model
	proposal brain.Welcome
	cursor   int
	// folderNames and contextTexts start as den's drafts and change when the user edits them.
	folderAccepted  []bool
	folderNames     []string
	contextAccepted []bool
	contextTexts    []string
	// optionKeyProfile is Terminal.app's profile for new windows, when den runs in Terminal.app.
	optionKeyProfile string
	optionKeyEnabled bool
}

type pastSessionsMessage struct {
	proposal         brain.Welcome
	optionKeyProfile string
	optionKeyEnabled bool
	// modelError says why den's model couldn't group the sessions, so the local rules did.
	modelError string
}

func newWelcomeState() *welcomeState {
	return &welcomeState{spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

// scanPastSessions digests every session the installed agents recorded in this repo's worktrees,
// leaving out the ones den already tracks.
func (currentModel model) scanPastSessions() tea.Cmd {
	repositoryPath := currentModel.repository.RepositoryPath
	installedAgents := currentModel.installedAgents
	helperCaller := currentModel.helperCaller
	tracked := map[string]bool{}
	for _, session := range currentModel.repository.Sessions {
		tracked[session.SessionId] = true
	}
	return func() tea.Msg {
		worktrees := git.Worktrees(repositoryPath)
		var digests []agent.Digest
		for _, adapter := range installedAgents {
			pastSessions, _ := adapter.PastSessions(worktrees)
			for _, digest := range pastSessions {
				if !tracked[digest.SessionId] {
					digests = append(digests, digest)
				}
			}
		}
		commitSubjects := git.RecentCommitSubjects(repositoryPath, recentCommitsCount)
		scanned := pastSessionsMessage{proposal: brain.FirstRun(digests, commitSubjects, repositoryPath)}
		if helperCaller != nil && len(digests) > 0 {
			var answer brain.FirstRunAnswer
			if _, err := helperCaller.Call(brain.FirstRunRequest(digests, repositoryPath), &answer); err != nil {
				scanned.modelError = err.Error()
			} else {
				scanned.proposal = brain.FirstRunFromModel(answer, digests, commitSubjects, repositoryPath)
			}
		}
		if isAppleTerminal() {
			scanned.optionKeyProfile, scanned.optionKeyEnabled, _ = terminal.OptionKey()
		}
		return scanned
	}
}

// isAppleTerminal tells whether den shows in Terminal.app. Inside tmux, TERM_PROGRAM names tmux,
// so the environment the tmux server started with, which came from the terminal, is asked instead.
func isAppleTerminal() bool {
	terminalProgram := os.Getenv("TERM_PROGRAM")
	if terminalProgram == "tmux" {
		terminalProgram = tmux.GlobalEnvironment("TERM_PROGRAM")
	}
	return terminalProgram == "Apple_Terminal"
}

func (currentModel model) updateWelcomeScan(scanned pastSessionsMessage) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
	if state == nil {
		return currentModel, nil
	}
	state.proposal = scanned.proposal
	state.optionKeyProfile, state.optionKeyEnabled = scanned.optionKeyProfile, scanned.optionKeyEnabled
	if scanned.modelError != "" {
		currentModel.setError("den's model couldn't group them (" + scanned.modelError + "), so these come from local rules")
	}
	state.step = foldersStep
	for _, folder := range scanned.proposal.Folders {
		state.folderAccepted = append(state.folderAccepted, true)
		state.folderNames = append(state.folderNames, folder.Name)
		state.contextAccepted = append(state.contextAccepted, true)
		state.contextTexts = append(state.contextTexts, folder.Context)
	}
	return currentModel, nil
}

func (currentModel model) updateWelcome(message tea.Msg) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
	if currentModel.prompt != nil {
		return currentModel.updatePrompt(message)
	}
	if state.step == scanningStep {
		var command tea.Cmd
		state.spinner, command = state.spinner.Update(message)
		return currentModel, command
	}
	keyPress, isKeyPress := message.(tea.KeyPressMsg)
	if !isKeyPress {
		return currentModel, nil
	}
	currentModel.clearStatus()
	keys := currentModel.keys
	if state.step == keysStep {
		return currentModel.updateWelcomeKeysStep(keyPress)
	}
	indexes := currentModel.welcomeIndexes()
	switch {
	case key.Matches(keyPress, keys.up):
		state.cursor = max(state.cursor-1, 0)
	case key.Matches(keyPress, keys.down):
		state.cursor = min(state.cursor+1, max(len(indexes)-1, 0))
	case key.Matches(keyPress, keys.toggle) && len(indexes) > 0:
		accepted := currentModel.welcomeAccepted()
		accepted[indexes[state.cursor]] = !accepted[indexes[state.cursor]]
	case key.Matches(keyPress, keys.edit) && len(indexes) > 0:
		return currentModel, currentModel.editWelcomeItem(indexes[state.cursor])
	case key.Matches(keyPress, keys.yes):
		currentModel.advanceWelcome()
	case key.Matches(keyPress, keys.no):
		if state.step == foldersStep {
			for index := range state.folderAccepted {
				state.folderAccepted[index] = false
			}
		} else {
			accepted := currentModel.welcomeAccepted()
			for index := range accepted {
				accepted[index] = false
			}
		}
		currentModel.advanceWelcome()
	case key.Matches(keyPress, keys.back):
		currentModel.finishWelcome(false)
	}
	return currentModel, nil
}

// welcomeIndexes are the rows of the current step: every proposed folder, or the drafts of the accepted ones.
func (currentModel model) welcomeIndexes() []int {
	state := currentModel.welcome
	var indexes []int
	switch state.step {
	case foldersStep:
		for index := range state.folderNames {
			indexes = append(indexes, index)
		}
	case contextsStep:
		for index, accepted := range state.folderAccepted {
			if accepted {
				indexes = append(indexes, index)
			}
		}
	}
	return indexes
}

func (currentModel model) welcomeAccepted() []bool {
	if currentModel.welcome.step == foldersStep {
		return currentModel.welcome.folderAccepted
	}
	return currentModel.welcome.contextAccepted
}

func (currentModel *model) advanceWelcome() {
	state := currentModel.welcome
	state.cursor = 0
	switch state.step {
	case foldersStep:
		state.step = keysStep
		if slices.Contains(state.folderAccepted, true) {
			state.step = contextsStep
		}
	case contextsStep:
		state.step = keysStep
	}
}

// updateWelcomeKeysStep finishes the setup; enter also turns on Terminal.app's Option key when it is off.
func (currentModel model) updateWelcomeKeysStep(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
	keys := currentModel.keys
	switch {
	case key.Matches(keyPress, keys.yes):
		profile, needsOptionKey := state.optionKeyProfile, currentModel.needsOptionKey()
		currentModel.finishWelcome(true)
		if needsOptionKey && !currentModel.reportError(terminal.EnableOptionKey(profile)) {
			currentModel.setStatus("Option keys on for new Terminal windows · open one to use ⌥")
		}
	case key.Matches(keyPress, keys.no), key.Matches(keyPress, keys.back):
		currentModel.finishWelcome(true)
	}
	return currentModel, nil
}

// needsOptionKey is true in Terminal.app when its profile still types characters with Option.
func (currentModel model) needsOptionKey() bool {
	return currentModel.welcome.optionKeyProfile != "" && !currentModel.welcome.optionKeyEnabled
}

func (currentModel *model) editWelcomeItem(index int) tea.Cmd {
	state := currentModel.welcome
	switch state.step {
	case foldersStep:
		return currentModel.askForText("Folder name", state.folderNames[index], func(currentModel *model, name string) {
			currentModel.welcome.folderNames[index] = name
		})
	}
	return currentModel.editDraft("◆ "+state.folderNames[index]+" · draft context", state.contextTexts[index], func(currentModel *model, text string) {
		currentModel.welcome.contextTexts[index] = text
		currentModel.welcome.contextAccepted[index] = true
		currentModel.setStatus("draft updated")
	})
}

// finishWelcome saves what was accepted: the folders with the past sessions filed in them,
// recent leftovers in the Inbox and older ones in the archive, and the contexts.
// Without apply, nothing is imported, and the welcome still won't show again.
func (currentModel *model) finishWelcome(apply bool) {
	state := currentModel.welcome
	if apply && state.step != scanningStep {
		currentModel.applyWelcome()
	}
	currentModel.repository.Welcomed = true
	currentModel.welcome = nil
	currentModel.saveRepository()
	currentModel.refreshContextTokens()
	currentModel.closeScreen()
	if apply {
		currentModel.setStatus("den is set up · n starts a session")
	}
}

func (currentModel *model) applyWelcome() {
	state := currentModel.welcome
	now := time.Now()
	filedIn := map[string]string{}
	for index, folder := range state.proposal.Folders {
		if !state.folderAccepted[index] {
			continue
		}
		folderId := currentModel.folderNamed(state.folderNames[index])
		if folderId == "" {
			folderId = currentModel.repository.AddFolder(state.folderNames[index], "").Id
		}
		for _, digest := range folder.Sessions {
			filedIn[digest.SessionId] = folderId
		}
		if state.contextAccepted[index] && currentModel.repository.ReadContext(folderId) == "" {
			currentModel.reportError(currentModel.repository.WriteContext(folderId, state.contextTexts[index]))
		}
	}
	var pastSessions []agent.Digest
	for _, folder := range state.proposal.Folders {
		pastSessions = append(pastSessions, folder.Sessions...)
	}
	for _, digest := range append(pastSessions, state.proposal.Unfiled...) {
		if currentModel.repository.FindSession(digest.SessionId) != nil {
			continue
		}
		folderId, isFiled := filedIn[digest.SessionId]
		title := state.proposal.Titles[digest.SessionId]
		if title == "" {
			title = digest.AgentTitle
		}
		if title == "" {
			title = truncate(digest.FirstPrompt, 120)
		}
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, store.Session{
			Tool:             digest.Tool,
			SessionId:        digest.SessionId,
			FolderId:         folderId,
			WorkingDirectory: digest.WorkingDirectory,
			Branch:           digest.Branch,
			Title:            title,
			TitledAtPrompt:   digest.PromptCount,
			ReviewedAtPrompt: digest.PromptCount,
			Archived:         !isFiled && !brain.IsRecent(digest, now),
			ArchivedAt:       archivedAt(!isFiled && !brain.IsRecent(digest, now), now),
			CreatedAt:        digest.StartedAt,
			LastActivityAt:   digest.LastActivityAt,
		})
	}
}

func archivedAt(archived bool, now time.Time) time.Time {
	if archived {
		return now
	}
	return time.Time{}
}

// folderNamed finds a top-level folder by name, ignoring case, so the welcome adds to folders that already exist.
func (currentModel model) folderNamed(name string) string {
	for _, folder := range currentModel.repository.Folders {
		if folder.ParentId == "" && strings.EqualFold(folder.Name, name) {
			return folder.Id
		}
	}
	return ""
}

func (currentModel model) welcomeView() string {
	theme := currentModel.theme
	width := currentModel.width
	state := currentModel.welcome
	cardWidth := min(welcomeCardWidth, width-4)
	innerWidth := cardWidth - 6
	lines := []string{currentModel.welcomeTopBar(width), theme.border.Render(strings.Repeat("─", width)), ""}
	var body []string
	switch state.step {
	case scanningStep:
		body = []string{
			theme.suggestionBold.Render("✦ Welcome to den"),
			"",
			state.spinner.View() + theme.dim.Render(" reading your past sessions in "+currentModel.repositoryName()+"…"),
			"",
			theme.faint.Render(currentModel.scanningNote()),
		}
	case foldersStep:
		body = currentModel.welcomeFoldersBody(innerWidth)
	case contextsStep:
		body = currentModel.welcomeContextsBody(innerWidth)
	case keysStep:
		body = currentModel.welcomeKeysBody(innerWidth)
	}
	lines = append(lines, centered(theme.card.Width(cardWidth).Render(strings.Join(body, "\n")), width)...)
	if currentModel.prompt != nil {
		lines = append(append(lines, ""), centered(theme.dim.Render(currentModel.prompt.question+"  ")+currentModel.prompt.input.View(), width)...)
	} else if currentModel.statusMessage != "" {
		lines = append(lines, centered(theme.dim.Render(currentModel.statusMessage), width)...)
	} else if state.step != scanningStep {
		lines = append(lines, centered(theme.faint.Render("esc skips the setup · nothing changes until you press enter"), width)...)
	}
	return strings.Join(lines, "\n")
}

func (currentModel model) welcomeTopBar(width int) string {
	theme := currentModel.theme
	left := []segment{{text: " den", style: theme.accentBold}, {text: " · " + currentModel.repositoryName() + " · first run", style: theme.dim}}
	return currentModel.layoutRow(left, nil, width, false)
}

// welcomeStepCount is three steps, or two when no folder is proposed or kept, since there are no contexts to draft then.
func (currentModel model) welcomeStepCount() int {
	if slices.Contains(currentModel.welcome.folderAccepted, true) {
		return 3
	}
	return 2
}

func (currentModel model) welcomeCardHeader(title string, step int, width int) string {
	return currentModel.layoutRow([]segment{{text: "✦ " + title, style: currentModel.theme.suggestionBold}},
		[]segment{{text: fmt.Sprintf("%d of %d", step, currentModel.welcomeStepCount()), style: currentModel.theme.dim}}, width, false)
}

func (currentModel model) welcomeFoldersBody(width int) []string {
	theme := currentModel.theme
	state := currentModel.welcome
	proposal := state.proposal
	lines := []string{currentModel.welcomeCardHeader("Welcome to den · "+currentModel.repositoryName(), 1, width), ""}
	if proposal.PastSessionCount == 0 {
		lines = append(lines, wrapAll([]string{
			theme.text.Render("No past sessions here yet, so there's nothing to file."),
			"",
			theme.dim.Render("den learns as you work. It files new sessions into folders, offers ⎇ subfolders when sessions share a branch, and keeps private notes per folder that every agent there starts with. It only suggests: you confirm every change."),
		}, width)...)
		lines = append(lines, "", strings.Join(currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.yes, "continue"), relabeled(currentModel.keys.back, "skip setup")}, width, 1), ""))
		return lines
	}
	readWith := "No model call, nothing left this Mac."
	if proposal.ByModel {
		readWith = "den's model grouped and named them, from those details only."
	}
	lines = append(lines, wrapAll([]string{theme.text.Render(fmt.Sprintf("I read %s in this repo: their branches, the files they edited and their first prompts. %s",
		countedSessions(proposal.PastSessionCount), readWith))}, width)...)
	lines = append(lines, "", theme.columnHeader.Render("PROPOSED FOLDERS"))
	if len(proposal.Folders) == 0 {
		lines = append(lines, theme.dim.Render("No clear topics yet; everything goes to the Inbox, and den files as it learns."))
	}
	leftovers := len(proposal.Unfiled)
	for index, folder := range proposal.Folders {
		if !state.folderAccepted[index] {
			leftovers += len(folder.Sessions)
		}
		checkbox := segment{text: "[x] ", style: theme.accent}
		if !state.folderAccepted[index] {
			checkbox = segment{text: "[ ] ", style: theme.faint}
		}
		covers := folder.Description
		if covers == "" {
			covers = strings.Join(folder.Keywords, ", ")
		}
		left := []segment{checkbox, {text: padRight(truncate(state.folderNames[index], 18), 19), style: theme.bold},
			{text: fmt.Sprintf("%4d  ", len(folder.Sessions)), style: theme.dim}, {text: covers, style: theme.dim}}
		lines = append(lines, currentModel.layoutRow(left, nil, width, index == state.cursor))
	}
	if leftovers > 0 {
		recent := 0
		now := time.Now()
		for _, digest := range currentModel.leftoverSessions() {
			if brain.IsRecent(digest, now) {
				recent++
			}
		}
		lines = append(lines, currentModel.layoutRow([]segment{{text: "    ", style: theme.text}, {text: padRight("Everything else", 19), style: theme.dim},
			{text: fmt.Sprintf("%4d  ", leftovers), style: theme.dim}, {text: fmt.Sprintf("%d recent → Inbox, %d older → Archive", recent, leftovers-recent), style: theme.faint}}, nil, width, false))
	}
	if len(proposal.Noticed) > 0 {
		lines = append(lines, "", theme.columnHeader.Render("NOTICED"))
		for _, noticed := range proposal.Noticed {
			lines = append(lines, theme.dim.Render("· "+noticed))
		}
	}
	lines = append(lines, "")
	lines = append(lines, currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.yes, "create"), currentModel.keys.toggle,
		relabeled(currentModel.keys.edit, "rename"), relabeled(currentModel.keys.no, "none of them")}, width, 2)...)
	return lines
}

// scanningNote says what the first run reads, and whether a model groups it.
func (currentModel model) scanningNote() string {
	if currentModel.helperCaller == nil {
		return "Only branch names, edited files and first prompts, read on this Mac."
	}
	return "Branch names, edited files and first prompts; " + currentModel.helperCaller.Name() + " groups and names them."
}

// leftoverSessions are the past sessions no accepted folder takes.
func (currentModel model) leftoverSessions() []agent.Digest {
	state := currentModel.welcome
	leftovers := append([]agent.Digest{}, state.proposal.Unfiled...)
	for index, folder := range state.proposal.Folders {
		if !state.folderAccepted[index] {
			leftovers = append(leftovers, folder.Sessions...)
		}
	}
	return leftovers
}

func (currentModel model) welcomeContextsBody(width int) []string {
	theme := currentModel.theme
	state := currentModel.welcome
	lines := []string{currentModel.welcomeCardHeader("Draft contexts", 2, width), ""}
	lines = append(lines, wrapAll([]string{theme.text.Render("A context.md per folder, drafted from where its sessions worked. Every agent you start in a folder gets its context first. They stay private, outside the repo.")}, width)...)
	lines = append(lines, "")
	indexes := currentModel.welcomeIndexes()
	for row, index := range indexes {
		checkbox := segment{text: "[x] ", style: theme.accent}
		if !state.contextAccepted[index] {
			checkbox = segment{text: "[ ] ", style: theme.faint}
		}
		left := []segment{checkbox, {text: "◆ ", style: theme.accent}, {text: state.folderNames[index], style: theme.bold}}
		right := []segment{{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(state.contextTexts[index])), style: theme.dim}}
		lines = append(lines, currentModel.layoutRow(left, right, width, row == state.cursor))
	}
	if len(indexes) > 0 {
		lines = append(lines, "")
		lines = append(lines, currentModel.draftPreview(state.contextTexts[indexes[min(state.cursor, len(indexes)-1)]], width)...)
	}
	lines = append(lines, "")
	lines = append(lines, currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.yes, "save drafts"), currentModel.keys.toggle,
		currentModel.keys.edit, relabeled(currentModel.keys.no, "skip")}, width, 2)...)
	return lines
}

// draftPreview shows the start of a draft behind a bar in the suggestion color.
func (currentModel model) draftPreview(text string, width int) []string {
	draftLines := wrapAll(currentModel.markdownLines(text), width-2)
	shown := draftLines[:min(draftPreviewLines, len(draftLines))]
	if len(draftLines) > len(shown) {
		shown = append(shown, currentModel.theme.faint.Render("…"))
	}
	return strings.Split(currentModel.theme.pinned.Render(strings.Join(shown, "\n")), "\n")
}

func (currentModel model) welcomeKeysBody(width int) []string {
	theme := currentModel.theme
	state := currentModel.welcome
	keys := currentModel.keys
	keyLine := func(keyLabel string, description string) string {
		return theme.key.Render(padRight(keyLabel, 8)) + theme.text.Render(description)
	}
	lines := []string{currentModel.welcomeCardHeader("Getting around", currentModel.welcomeStepCount(), width), ""}
	lines = append(lines, wrapAll([]string{theme.text.Render("den is the column on the left, and your agents sit beside it. Every key in den is a plain key; these work from anywhere in the window:")}, width)...)
	lines = append(lines, "",
		keyLine("⌥1–⌥4", "jump to that agent"),
		keyLine("⌥0", "jump to den, and back"),
		keyLine("⌥N", "start a new agent"),
		keyLine("click", "focus any pane"),
		"", theme.columnHeader.Render("IN DEN"),
		keyLine("⇥", "den's suggestions: ↵ yes, ⌫ no, or type to tell den"),
		keyLine("↵", "open, or yes when den asks"),
		keyLine("esc", "back out without changing anything"),
		keyLine("?", "every key"),
		"")
	var finishHints []key.Binding
	switch {
	case currentModel.needsOptionKey():
		lines = append(lines, wrapAll([]string{
			theme.suggestionBold.Render("✦ Terminal.app types characters with Option right now, so the ⌥ keys can't reach den."),
			theme.dim.Render(fmt.Sprintf("Enter turns on \"Use Option as Meta key\" for its profile \"%s\". New windows get it; Option stops typing characters like å there.", state.optionKeyProfile)),
		}, width)...)
		finishHints = []key.Binding{relabeled(keys.yes, "turn it on and finish"), relabeled(keys.no, "leave it off")}
	case state.optionKeyProfile != "":
		lines = append(lines, theme.running.Render("✓ ")+theme.dim.Render(fmt.Sprintf("Option keys work: \"Use Option as Meta key\" is on in Terminal.app (%s).", state.optionKeyProfile)))
		finishHints = []key.Binding{relabeled(keys.yes, "finish")}
	default:
		lines = append(lines, wrapAll([]string{theme.dim.Render("In iTerm2 set Option to send Esc+, in Ghostty set macos-option-as-alt, so the ⌥ keys reach den.")}, width)...)
		finishHints = []key.Binding{relabeled(keys.yes, "finish")}
	}
	return append(lines, "", strings.Join(currentModel.hintLines(finishHints, width, 1), ""))
}
