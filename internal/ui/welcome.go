package ui

import (
	"fmt"
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
	guidesStep
)

// welcomeState is the first-run review: den reads the repo's past sessions and commits,
// then proposes folders, their contexts and the guides, and the user picks what to keep.
type welcomeState struct {
	step     welcomeStep
	spinner  spinner.Model
	proposal brain.Welcome
	cursor   int
	// folderNames, contextTexts and guideTexts start as den's drafts and change when the user edits them.
	folderAccepted  []bool
	folderNames     []string
	contextAccepted []bool
	contextTexts    []string
	guideAccepted   []bool
	guideTexts      []string
}

type pastSessionsMessage struct {
	proposal brain.Welcome
}

func newWelcomeState() *welcomeState {
	return &welcomeState{spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot))}
}

// scanPastSessions digests every session the installed agents recorded in this repo's worktrees,
// leaving out the ones den already tracks.
func (currentModel model) scanPastSessions() tea.Cmd {
	repositoryPath := currentModel.repository.RepositoryPath
	installedAgents := currentModel.installedAgents
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
		return pastSessionsMessage{proposal: brain.FirstRun(digests, commitSubjects, repositoryPath)}
	}
}

func (currentModel model) updateWelcomeScan(scanned pastSessionsMessage) (tea.Model, tea.Cmd) {
	state := currentModel.welcome
	if state == nil {
		return currentModel, nil
	}
	state.proposal = scanned.proposal
	state.step = foldersStep
	for _, folder := range scanned.proposal.Folders {
		state.folderAccepted = append(state.folderAccepted, true)
		state.folderNames = append(state.folderNames, folder.Name)
		state.contextAccepted = append(state.contextAccepted, true)
		state.contextTexts = append(state.contextTexts, folder.Context)
	}
	for _, guide := range scanned.proposal.Guides {
		state.guideAccepted = append(state.guideAccepted, currentModel.repository.ReadGuide(guide.Name) == "")
		state.guideTexts = append(state.guideTexts, guide.Text)
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
	case key.Matches(keyPress, keys.yes), key.Matches(keyPress, keys.next):
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

// welcomeIndexes are the rows of the current step: every proposed folder, the drafts of the accepted ones, or the guides.
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
	case guidesStep:
		for index := range state.guideTexts {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (currentModel model) welcomeAccepted() []bool {
	switch currentModel.welcome.step {
	case foldersStep:
		return currentModel.welcome.folderAccepted
	case contextsStep:
		return currentModel.welcome.contextAccepted
	}
	return currentModel.welcome.guideAccepted
}

func (currentModel *model) advanceWelcome() {
	state := currentModel.welcome
	state.cursor = 0
	switch state.step {
	case foldersStep:
		state.step = guidesStep
		if slices.Contains(state.folderAccepted, true) {
			state.step = contextsStep
		}
	case contextsStep:
		state.step = guidesStep
	case guidesStep:
		currentModel.finishWelcome(true)
	}
}

func (currentModel *model) editWelcomeItem(index int) tea.Cmd {
	state := currentModel.welcome
	switch state.step {
	case foldersStep:
		return currentModel.askForText("Folder name", state.folderNames[index], func(currentModel *model, name string) {
			currentModel.welcome.folderNames[index] = name
		})
	case contextsStep:
		return currentModel.editDraft("◆ "+state.folderNames[index]+" · draft context", state.contextTexts[index], func(currentModel *model, text string) {
			currentModel.welcome.contextTexts[index] = text
			currentModel.welcome.contextAccepted[index] = true
			currentModel.setStatus("draft updated")
		})
	}
	guide := state.proposal.Guides[index]
	return currentModel.editDraft(guide.Name+".md · draft", state.guideTexts[index], func(currentModel *model, text string) {
		currentModel.welcome.guideTexts[index] = text
		currentModel.welcome.guideAccepted[index] = true
		currentModel.setStatus("draft updated")
	})
}

// finishWelcome saves what was accepted: the folders with the past sessions filed in them,
// recent leftovers in the Inbox and older ones in the archive, the contexts and the guides.
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
		currentModel.repository.Sessions = append(currentModel.repository.Sessions, store.Session{
			Tool:             digest.Tool,
			SessionId:        digest.SessionId,
			FolderId:         folderId,
			WorkingDirectory: digest.WorkingDirectory,
			Branch:           digest.Branch,
			Title:            truncate(digest.FirstPrompt, 120),
			Archived:         !isFiled && !brain.IsRecent(digest, now),
			CreatedAt:        digest.StartedAt,
			LastActivityAt:   digest.LastActivityAt,
		})
	}
	for index, guide := range state.proposal.Guides {
		if state.guideAccepted[index] && currentModel.repository.ReadGuide(guide.Name) == "" {
			currentModel.reportError(currentModel.repository.WriteGuide(guide.Name, state.guideTexts[index]))
		}
	}
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
			theme.faint.Render("Only branch names, edited files and first prompts, read on this Mac."),
		}
	case foldersStep:
		body = currentModel.welcomeFoldersBody(innerWidth)
	case contextsStep:
		body = currentModel.welcomeContextsBody(innerWidth)
	case guidesStep:
		body = currentModel.welcomeGuidesBody(innerWidth)
	}
	lines = append(lines, centered(theme.card.Width(cardWidth).Render(strings.Join(body, "\n")), width)...)
	if currentModel.prompt != nil {
		lines = append(append(lines, ""), centered(theme.dim.Render(currentModel.prompt.question+"  ")+currentModel.prompt.input.View(), width)...)
	} else if currentModel.statusMessage != "" {
		lines = append(lines, centered(theme.dim.Render(currentModel.statusMessage), width)...)
	} else if state.step != scanningStep {
		lines = append(lines, centered(theme.faint.Render("esc skips the setup · nothing changes until you press y"), width)...)
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
	lines = append(lines, wrapAll([]string{theme.text.Render(fmt.Sprintf("I read %s in this repo: their branches, the files they edited and their first prompts. No model call, nothing left this Mac.",
		countedSessions(proposal.PastSessionCount)))}, width)...)
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
		left := []segment{checkbox, {text: padRight(truncate(state.folderNames[index], 18), 19), style: theme.bold},
			{text: fmt.Sprintf("%4d  ", len(folder.Sessions)), style: theme.dim}, {text: strings.Join(folder.Keywords, ", "), style: theme.dim}}
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
		relabeled(currentModel.keys.edit, "rename"), relabeled(currentModel.keys.no, "start empty")}, width, 2)...)
	return lines
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

func (currentModel model) welcomeGuidesBody(width int) []string {
	theme := currentModel.theme
	state := currentModel.welcome
	lines := []string{currentModel.welcomeCardHeader("Guides", currentModel.welcomeStepCount(), width), ""}
	lines = append(lines, wrapAll([]string{theme.text.Render("How you write commits, pull requests and issues in this repo. Agents read the matching guide before writing one, and den suggests edits as it learns your habits.")}, width)...)
	lines = append(lines, "")
	for index, guide := range state.proposal.Guides {
		checkbox := segment{text: "[x] ", style: theme.accent}
		if !state.guideAccepted[index] {
			checkbox = segment{text: "[ ] ", style: theme.faint}
		}
		reason := guide.Reason
		if currentModel.repository.ReadGuide(guide.Name) != "" {
			reason = "you have one already, kept as is"
		}
		left := []segment{checkbox, {text: padRight(guide.Name+".md", 18), style: theme.bold}, {text: reason, style: theme.dim}}
		lines = append(lines, currentModel.layoutRow(left, nil, width, index == state.cursor))
	}
	if len(state.guideTexts) > 0 {
		lines = append(lines, "")
		lines = append(lines, currentModel.draftPreview(state.guideTexts[min(state.cursor, len(state.guideTexts)-1)], width)...)
	}
	lines = append(lines, "")
	lines = append(lines, currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.yes, "save and finish"), currentModel.keys.toggle,
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
