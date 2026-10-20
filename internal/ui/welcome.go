package ui

import (
	"encoding/json"
	"fmt"
	"os"
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

const welcomeCardWidth = 78

type welcomeStep int

const (
	scanningStep welcomeStep = iota
	sessionsStep
	// keysStep teaches the keys and checks that the Option key reaches den, which its ⌥ keys need, offering to turn it on.
	keysStep
)

// welcomeState is the first run: den reads the repo's newest past sessions, says which look worth returning to,
// and the user picks which to bring in. Nothing else from the past is imported.
type welcomeState struct {
	step     welcomeStep
	spinner  spinner.Model
	arrivals []brain.Arrival
	accepted []bool
	// callId is the logged call that judged the arrivals, for the decision log.
	callId string
	cursor int
	// optionKeyProfile is Terminal.app's profile for new windows, when den runs in Terminal.app.
	optionKeyProfile string
	optionKeyEnabled bool
	// terminalProgram is the terminal den shows in, like Apple_Terminal, iTerm.app or ghostty.
	terminalProgram string
	// optionKeyTyped is what arrived when the user pressed ⌥L to check the Option key: empty until they did,
	// "alt+l" when it reached den, or the character the terminal typed instead.
	optionKeyTyped string
}

type pastSessionsMessage struct {
	arrivals         []brain.Arrival
	optionKeyProfile string
	optionKeyEnabled bool
	// modelError says why den's model couldn't judge the sessions, so the local rule did.
	modelError string
	// call is den's model call that judged them, for the call log, and system its instructions.
	call   *store.Call
	system string
}

func newWelcomeState() *welcomeState {
	return &welcomeState{spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)), terminalProgram: currentTerminalProgram()}
}

// scanPastSessions reads the newest sessions the installed agents recorded in this repo's worktrees,
// leaving out the ones den already tracks, and asks den's model which are worth bringing in.
func (currentModel model) scanPastSessions() tea.Cmd {
	repository := currentModel.repository
	installedAgents := currentModel.installedAgents
	helperCaller := currentModel.helperCaller
	isAppleTerminal := currentModel.welcome.terminalProgram == "Apple_Terminal"
	tracked := map[string]bool{}
	for _, session := range repository.Sessions {
		tracked[session.SessionId] = true
	}
	return func() tea.Msg {
		var digests []agent.Digest
		adaptersBySessionId := map[string]agent.Adapter{}
		for _, adapter := range installedAgents {
			pastSessions, _ := adapter.PastSessions(git.Worktrees(repository.RepositoryPath))
			for _, digest := range pastSessions {
				if !tracked[digest.SessionId] && digest.PromptCount > 0 {
					digests = append(digests, digest)
					adaptersBySessionId[digest.SessionId] = adapter
				}
			}
		}
		newest := brain.NewestSessions(digests)
		scanned := pastSessionsMessage{arrivals: brain.ArrivalsWithoutModel(newest)}
		if helperCaller != nil && len(newest) > 0 {
			promptsBySessionId := map[string][]string{}
			for _, digest := range newest {
				if digest.PromptCount < brain.LongSessionPrompts {
					continue
				}
				turns, _ := adaptersBySessionId[digest.SessionId].Turns(digest.SessionId)
				for _, turn := range turns {
					promptsBySessionId[digest.SessionId] = append(promptsBySessionId[digest.SessionId], turn.User)
				}
			}
			var answer brain.FirstRunAnswer
			request := brain.FirstRunRequest(newest, promptsBySessionId, &repository, time.Now())
			usage, err := helperCaller.Call(request, &answer)
			scanned.system = request.System
			scanned.call = &store.Call{Job: "first run", Model: usage.Model, PromptVersion: promptVersion(request.System), Prompt: request.Prompt,
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CostDollars: usage.CostDollars, Milliseconds: usage.Duration.Milliseconds()}
			if err != nil {
				scanned.modelError, scanned.call.Error = err.Error(), err.Error()
			} else {
				scanned.arrivals = brain.ArrivalsFromModel(answer, newest, &repository)
				scanned.call.Answer, _ = json.Marshal(answer)
			}
		}
		if isAppleTerminal {
			scanned.optionKeyProfile, scanned.optionKeyEnabled, _ = terminal.OptionKey()
		}
		return scanned
	}
}

// currentTerminalProgram names the terminal den shows in. Inside tmux, TERM_PROGRAM names tmux,
// so the environment the tmux server started with, which came from the terminal, is asked instead.
func currentTerminalProgram() string {
	terminalProgram := os.Getenv("TERM_PROGRAM")
	if terminalProgram == "tmux" {
		terminalProgram = tmux.GlobalEnvironment("TERM_PROGRAM")
	}
	return terminalProgram
}

func (currentModel model) updateWelcomeScan(scanned pastSessionsMessage) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
	if state == nil {
		return currentModel, nil
	}
	if scanned.call != nil {
		currentModel.reportError(currentModel.repository.KeepSystemPrompt(scanned.call.PromptVersion, scanned.system))
		callId, err := currentModel.repository.LogCall(*scanned.call)
		currentModel.reportError(err)
		state.callId = callId
	}
	state.arrivals = scanned.arrivals
	state.optionKeyProfile, state.optionKeyEnabled = scanned.optionKeyProfile, scanned.optionKeyEnabled
	if scanned.modelError != "" {
		currentModel.setError("den's model couldn't judge them (" + scanned.modelError + "), so these come from a local rule")
	}
	for _, arrival := range scanned.arrivals {
		state.accepted = append(state.accepted, arrival.Verdict != brain.VerdictDone)
	}
	state.step = sessionsStep
	return currentModel, nil
}

func (currentModel model) updateWelcome(message tea.Msg) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
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
	switch {
	case key.Matches(keyPress, keys.up):
		state.cursor = max(state.cursor-1, 0)
	case key.Matches(keyPress, keys.down):
		state.cursor = min(state.cursor+1, max(len(state.arrivals)-1, 0))
	case key.Matches(keyPress, keys.toggle) && len(state.arrivals) > 0:
		state.accepted[state.cursor] = !state.accepted[state.cursor]
	case key.Matches(keyPress, keys.yes):
		state.step, state.cursor = keysStep, 0
	case key.Matches(keyPress, keys.no):
		for index := range state.accepted {
			state.accepted[index] = false
		}
		state.step, state.cursor = keysStep, 0
	case key.Matches(keyPress, keys.back):
		currentModel.finishWelcome(false)
	}
	return currentModel, nil
}

// updateWelcomeKeysStep checks the Option key with ⌥L and finishes the setup; enter also turns the Option key on
// in Terminal.app or Ghostty when it types characters.
func (currentModel model) updateWelcomeKeysStep(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
	keys := currentModel.keys
	switch {
	case keyPress.String() == "alt+l":
		state.optionKeyTyped = "alt+l"
	case key.Matches(keyPress, keys.yes):
		profile, needsOptionKey := state.optionKeyProfile, currentModel.needsOptionKey()
		needsGhosttyOptionKey := currentModel.optionKeyTypesCharacters() && state.terminalProgram == "ghostty"
		currentModel.finishWelcome(true)
		if needsOptionKey && !currentModel.reportError(terminal.EnableOptionKey(profile)) {
			currentModel.setStatus("Option keys on for new Terminal windows · open one to use ⌥")
		}
		if needsGhosttyOptionKey && !currentModel.reportError(terminal.EnableGhosttyOptionKey()) {
			currentModel.setStatus("Option keys on in Ghostty's config · reload it with ⌘⇧, to use ⌥")
		}
	case key.Matches(keyPress, keys.no), key.Matches(keyPress, keys.back):
		currentModel.finishWelcome(true)
	case keyPress.Text != "" && state.optionKeyProfile == "":
		state.optionKeyTyped = keyPress.Text
	}
	return currentModel, nil
}

// needsOptionKey is true in Terminal.app when its profile still types characters with Option.
func (currentModel model) needsOptionKey() bool {
	return currentModel.welcome.optionKeyProfile != "" && !currentModel.welcome.optionKeyEnabled
}

// optionKeyTypesCharacters is true once ⌥L typed a character instead of reaching den.
func (currentModel model) optionKeyTypesCharacters() bool {
	typed := currentModel.welcome.optionKeyTyped
	return typed != "" && typed != "alt+l"
}

// finishWelcome brings in the sessions the user kept, in the Inbox, and offers what den found worth keeping in them.
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
		currentModel.setStatus("den's all set · n starts a session")
	}
}

// applyWelcome adds the kept sessions. Each gets its days before the archive from now, so it shows long enough to be seen.
func (currentModel *model) applyWelcome() {
	state := currentModel.welcome
	now := time.Now()
	for index, arrival := range state.arrivals {
		digest := arrival.Digest
		if currentModel.repository.FindSession(digest.SessionId) != nil {
			continue
		}
		decision := store.Decision{Job: "first run", SessionId: digest.SessionId, Action: "verdict " + arrival.Verdict,
			Detail: arrival.Title + " · " + arrival.Why, Outcome: "brought in", CallId: state.callId}
		if !state.accepted[index] {
			decision.Outcome = "left out"
			currentModel.reportError(currentModel.repository.LogDecision(decision))
			continue
		}
		session := store.Session{
			Tool:             digest.Tool,
			SessionId:        digest.SessionId,
			WorkingDirectory: digest.WorkingDirectory,
			Branch:           digest.Branch,
			Title:            arrival.Title,
			TitledAtPrompt:   digest.PromptCount,
			ReviewedAtPrompt: digest.PromptCount,
			CreatedAt:        now,
			LastActivityAt:   digest.LastActivityAt,
		}
		// The first run sees only a session's start and end, so one worth keeping gets the card review,
		// and one worth keeping or unfinished gets the progress review of its latest prompt, which writes where it stands.
		// The rest wait for new prompts.
		checkedUpTo(&session, digest)
		if arrival.Verdict == brain.VerdictKeep {
			session.CardReviewDue, session.CardReviewedAtPrompt = true, 0
		}
		if arrival.Verdict != brain.VerdictDone {
			session.ReviewedAtPrompt = digest.PromptCount - 1
		}
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, session)
		currentModel.reportError(currentModel.repository.LogDecision(decision))
	}
}

// folderNamed finds a top-level folder by name, ignoring case.
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
			state.spinner.View() + theme.dim.Render(fmt.Sprintf(" reading your last %d sessions in %s…", brain.FirstRunSessions, currentModel.repositoryName())),
			"",
			theme.faint.Render(currentModel.scanningNote()),
		}
	case sessionsStep:
		body = currentModel.welcomeSessionsBody(innerWidth)
	case keysStep:
		body = currentModel.welcomeKeysBody(innerWidth)
	}
	lines = append(lines, centered(theme.card.Width(cardWidth).Render(strings.Join(body, "\n")), width)...)
	if currentModel.statusMessage != "" {
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

func (currentModel model) welcomeCardHeader(title string, step int, width int) string {
	return currentModel.layoutRow([]segment{{text: "✦ " + title, style: currentModel.theme.suggestionBold}},
		[]segment{{text: fmt.Sprintf("%d of 2", step), style: currentModel.theme.dim}}, width, false)
}

// welcomeSessionsBody lists the newest past sessions with den's verdict on each, checked when worth bringing in.
func (currentModel model) welcomeSessionsBody(width int) []string {
	theme := currentModel.theme
	state := currentModel.welcome
	lines := []string{currentModel.welcomeCardHeader("Welcome to den · "+currentModel.repositoryName(), 1, width), ""}
	if len(state.arrivals) == 0 {
		lines = append(lines, wrapAll([]string{
			theme.text.Render("No past sessions here yet, so there's nothing to bring in."),
			"",
			theme.dim.Render("Start sessions in folders, and each starts with its folder's context. When a session finds something worth keeping, den offers to save it. It only suggests: you confirm every change."),
		}, width)...)
		return append(lines, "", strings.Join(currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.yes, "continue"), relabeled(currentModel.keys.back, "skip setup")}, width, 1), ""))
	}
	lines = append(lines, wrapAll([]string{theme.text.Render(fmt.Sprintf("These are your %d newest sessions here. den archives a session %d days after you last use it, so bring in only the ones worth returning to.",
		len(state.arrivals), archiveAfterDays()))}, width)...)
	lines = append(lines, "")
	offered := 0
	for index, arrival := range state.arrivals {
		checkbox := segment{text: "[x] ", style: theme.accent}
		if !state.accepted[index] {
			checkbox = segment{text: "[ ] ", style: theme.faint}
		}
		verdict := map[string]segment{
			brain.VerdictContinue: {text: "unfinished", style: theme.waiting},
			brain.VerdictKeep:     {text: "worth keeping", style: theme.suggestion},
			brain.VerdictDone:     {text: "done", style: theme.faint},
		}[arrival.Verdict]
		right := []segment{verdict, {text: fmt.Sprintf("%5s", relativeAge(arrival.Digest.LastActivityAt)), style: theme.dim}}
		lines = append(lines, currentModel.layoutRow([]segment{checkbox, {text: arrival.Title, style: theme.text}}, right, width, index == state.cursor))
		if arrival.Why != "" && arrival.Verdict != brain.VerdictDone {
			lines = append(lines, theme.faint.Render(truncate("    "+arrival.Why, width)))
		}
		if arrival.Verdict == brain.VerdictKeep && state.accepted[index] {
			offered++
		}
	}
	if offered > 0 {
		lines = append(lines, "", theme.suggestion.Render(fmt.Sprintf("✦ den reads all of %s next, and suggests what's worth keeping.", countedSessions(offered))))
	}
	lines = append(lines, "")
	lines = append(lines, currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.yes, "bring them in"), currentModel.keys.toggle,
		relabeled(currentModel.keys.no, "none of them")}, width, 2)...)
	return lines
}

// scanningNote says what the first run reads, and whether a model judges it.
func (currentModel model) scanningNote() string {
	if currentModel.helperCaller == nil {
		return "Their prompts and edited files, read on this Mac; sessions with real work come in."
	}
	return "Their prompts, edited files and how they ended; den judges which are worth returning to."
}

func (currentModel model) welcomeKeysBody(width int) []string {
	theme := currentModel.theme
	state := currentModel.welcome
	keys := currentModel.keys
	keyLine := func(keyLabel string, description string) string {
		return theme.key.Render(padRight(keyLabel, 8)) + theme.text.Render(description)
	}
	lines := []string{currentModel.welcomeCardHeader("Getting around", 2, width), ""}
	lines = append(lines, wrapAll([]string{theme.text.Render("den is the column on the left, and your agents sit beside it. Every key in den is a plain key; these work from anywhere in the window:")}, width)...)
	lines = append(lines, "",
		keyLine("⌥1–⌥4", "jump to that agent"),
		keyLine("⌥0", "jump to den, and back"),
		keyLine("⌥⇥", "next pane, round to den"),
		keyLine("⌥N", "start a new agent"),
		keyLine("click", "focus any pane"),
		"", theme.columnHeader.Render("IN DEN"),
		keyLine("n", "new agent in the selected folder, starting with its context"),
		keyLine("c", "ask den where you left off, and pick it back up in a new agent"),
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
	case state.optionKeyTyped == "alt+l":
		lines = append(lines, theme.running.Render("✓ ")+theme.dim.Render("Option keys work: ⌥L reached den."))
		finishHints = []key.Binding{relabeled(keys.yes, "finish")}
	case currentModel.optionKeyTypesCharacters() && state.terminalProgram == "ghostty":
		lines = append(lines, wrapAll([]string{
			theme.suggestionBold.Render(fmt.Sprintf("✦ ⌥L typed \"%s\", so Ghostty types characters with Option and the ⌥ keys can't reach den.", state.optionKeyTyped)),
			theme.dim.Render("Enter adds macos-option-as-alt = true to Ghostty's config. Reload it with ⌘⇧, and Option stops typing characters like å."),
		}, width)...)
		finishHints = []key.Binding{relabeled(keys.yes, "turn it on and finish"), relabeled(keys.no, "leave it off")}
	case currentModel.optionKeyTypesCharacters() && state.terminalProgram == "iTerm.app":
		lines = append(lines, wrapAll([]string{
			theme.suggestionBold.Render(fmt.Sprintf("✦ ⌥L typed \"%s\", so iTerm2 types characters with Option and the ⌥ keys can't reach den.", state.optionKeyTyped)),
			theme.dim.Render("In iTerm2, open Settings → Profiles → Keys and set Left Option key to Esc+. Then press ⌥L again."),
		}, width)...)
		finishHints = []key.Binding{relabeled(keys.yes, "finish")}
	case currentModel.optionKeyTypesCharacters():
		lines = append(lines, wrapAll([]string{
			theme.suggestionBold.Render(fmt.Sprintf("✦ ⌥L typed \"%s\", so your terminal types characters with Option and the ⌥ keys can't reach den.", state.optionKeyTyped)),
			theme.dim.Render("Set your terminal's Option key to send Alt, Meta or Esc+. Then press ⌥L again."),
		}, width)...)
		finishHints = []key.Binding{relabeled(keys.yes, "finish")}
	default:
		lines = append(lines, wrapAll([]string{theme.text.Render("Press ⌥L to check that the ⌥ keys reach den.")}, width)...)
		finishHints = []key.Binding{relabeled(keys.yes, "finish")}
	}
	return append(lines, "", strings.Join(currentModel.hintLines(finishHints, width, 1), ""))
}
