package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	// maximumDockShare keeps the dock to part of the panel, so the tree stays visible above it.
	maximumDockShare = 2
	// dockNotesPreviewLines is how much of rewritten notes the dock shows before enter saves them.
	dockNotesPreviewLines = 8
)

// dockItem is one decision in the dock: a suggestion, or one session of a cleanup, since each session is archived on its own.
type dockItem struct {
	suggestionId string
	sessionId    string
}

// dockState is the suggestion dock at the bottom of the panel. Focused with tab, enter says yes, backspace says no,
// and anything typed is told to den, which answers with what it would do instead.
type dockState struct {
	focused bool
	index   int
	input   textinput.Model
	// asked is what the user told den about item; reply is den's answer, with the changes it would make.
	asked    string
	reply    *brain.ChatMessage
	thinking bool
	item     dockItem
}

func newDockState() dockState {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "↵ yes  ⌫ no  or tell den…"
	input.CharLimit = 500
	return dockState{input: input}
}

// dockItems lists every decision waiting, in the order of the tree, so walking them walks down the tree.
func (currentModel model) dockItems() []dockItem {
	var items []dockItem
	for _, suggestion := range currentModel.repository.Suggestions {
		if suggestion.Kind == store.SuggestCleanup {
			for _, sessionId := range suggestion.SessionIds {
				items = append(items, dockItem{suggestionId: suggestion.Id, sessionId: sessionId})
			}
			continue
		}
		items = append(items, dockItem{suggestionId: suggestion.Id})
	}
	rows := currentModel.treeRows(true)
	position := func(item dockItem) int {
		if rowIndex := slices.IndexFunc(rows, currentModel.itemRow(item)); rowIndex >= 0 {
			return rowIndex
		}
		return len(rows)
	}
	slices.SortStableFunc(items, func(first dockItem, second dockItem) int {
		return position(first) - position(second)
	})
	return items
}

// itemRow matches the tree row an item is about: its session, its folder, or the repo row for the rest.
func (currentModel model) itemRow(item dockItem) func(listRow) bool {
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	sessionId := item.sessionId
	if suggestion != nil && sessionId == "" && len(suggestion.SessionIds) > 0 {
		sessionId = suggestion.SessionIds[0]
	}
	switch {
	case sessionId != "":
		return func(row listRow) bool { return row.kind == sessionRow && row.session.SessionId == sessionId }
	case suggestion != nil && suggestion.FolderId != "" && (suggestion.Kind == store.SuggestContext || suggestion.Kind == store.SuggestLink):
		folderId := suggestion.FolderId
		return func(row listRow) bool { return row.kind == folderRow && row.folder.Id == folderId }
	}
	return func(row listRow) bool { return row.kind == repositoryRow }
}

// currentDockItem is the item the dock shows: the one being walked when focused,
// otherwise the selected row's, or else the first waiting.
func (currentModel model) currentDockItem() (dockItem, bool) {
	items := currentModel.dockItems()
	if len(items) == 0 {
		return dockItem{}, false
	}
	if currentModel.dock.focused {
		return items[min(currentModel.dock.index, len(items)-1)], true
	}
	if selected, exists := currentModel.selectedRow(); exists {
		for _, item := range items {
			if currentModel.itemRow(item)(selected) {
				return item, true
			}
		}
	}
	return items[0], true
}

// focusDock starts walking the suggestions at the one the dock shows.
func (currentModel *model) focusDock() tea.Cmd {
	item, exists := currentModel.currentDockItem()
	if !exists {
		currentModel.setStatus("nothing waiting · den suggests only when it's sure")
		return nil
	}
	currentModel.dock.focused = true
	currentModel.dock.index = max(slices.Index(currentModel.dockItems(), item), 0)
	currentModel.clearDockReply()
	currentModel.followDockItem()
	return currentModel.dock.input.Focus()
}

func (currentModel *model) blurDock() {
	currentModel.dock.focused = false
	currentModel.dock.input.Blur()
	currentModel.dock.input.SetValue("")
	currentModel.clearDockReply()
}

func (currentModel *model) clearDockReply() {
	currentModel.dock.asked, currentModel.dock.reply, currentModel.dock.thinking = "", nil, false
}

// followDockItem selects the item's row in the tree, opening its folder, so it's clear what the suggestion is about.
func (currentModel *model) followDockItem() {
	item, exists := currentModel.currentDockItem()
	if !exists {
		return
	}
	if suggestion := currentModel.repository.FindSuggestion(item.suggestionId); suggestion != nil {
		sessionId := item.sessionId
		if sessionId == "" && len(suggestion.SessionIds) > 0 {
			sessionId = suggestion.SessionIds[0]
		}
		if session := currentModel.repository.FindSession(sessionId); session != nil {
			currentModel.expandFolder(session.FolderId)
			if session.Archived {
				currentModel.archiveExpanded = true
			}
		}
	}
	currentModel.cursorTo(currentModel.itemRow(item))
	currentModel.previewOffset = 0
}

func (currentModel model) updateDock(keyPress tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	state := &currentModel.dock
	keys := currentModel.keys
	isEmpty := state.input.Value() == ""
	switch {
	case key.Matches(keyPress, keys.back), keyPress.String() == "tab":
		currentModel.blurDock()
		return currentModel, nil
	case (keyPress.String() == "right" || keyPress.String() == "left") && isEmpty:
		currentModel.toggleWide()
		return currentModel, nil
	case keyPress.String() == "up", keyPress.String() == "down":
		step := 1
		if keyPress.String() == "up" {
			step = -1
		}
		state.index = max(min(state.index+step, len(currentModel.dockItems())-1), 0)
		currentModel.clearDockReply()
		currentModel.followDockItem()
		return currentModel, nil
	case key.Matches(keyPress, keys.yes) && !isEmpty && !state.thinking:
		state.asked = strings.TrimSpace(state.input.Value())
		state.item, _ = currentModel.currentDockItem()
		state.input.SetValue("")
		state.reply, state.thinking = nil, true
		currentModel.queueJob(job{kind: reviseJob, suggestionId: state.item.suggestionId, sessionId: state.item.sessionId})
		return currentModel, currentModel.startNextJob()
	case key.Matches(keyPress, keys.yes) && isEmpty && !state.thinking:
		item, exists := currentModel.currentDockItem()
		if !exists {
			return currentModel, nil
		}
		if state.reply != nil && len(state.reply.Actions) > 0 {
			currentModel.applyDockReply(item, state.reply.Actions)
		} else {
			currentModel.answerDockItem(item, true)
		}
		currentModel.afterDockAnswer()
		return currentModel, nil
	case key.Matches(keyPress, keys.no) && isEmpty:
		if state.reply != nil || state.thinking {
			currentModel.clearDockReply()
			return currentModel, nil
		}
		if item, exists := currentModel.currentDockItem(); exists {
			currentModel.answerDockItem(item, false)
			currentModel.afterDockAnswer()
		}
		return currentModel, nil
	}
	var command tea.Cmd
	state.input, command = state.input.Update(keyPress)
	return currentModel, command
}

// answerDockItem says yes or no to an item; for a cleanup, only to its one session.
func (currentModel *model) answerDockItem(item dockItem, accepted bool) {
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	if suggestion == nil {
		return
	}
	isCleanupSession := suggestion.Kind == store.SuggestCleanup && item.sessionId != ""
	switch {
	case accepted && isCleanupSession:
		if currentModel.reportError(currentModel.repository.AcceptFor(suggestion.Id, item.sessionId)) {
			return
		}
		currentModel.saveRepository()
		currentModel.setStatus(fmt.Sprintf("archived, still resumable for %d days", archiveKeepDays()))
	case accepted:
		currentModel.acceptSuggestion(suggestion.Id)
	case isCleanupSession:
		currentModel.repository.RejectFor(suggestion.Id, item.sessionId)
		currentModel.saveRepository()
		currentModel.setStatus("kept · den won't offer that again")
	default:
		currentModel.rejectSuggestion(suggestion.Id)
	}
	outcome := "rejected"
	if accepted {
		outcome = "accepted"
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "dock", SessionId: item.sessionId, FolderId: suggestion.FolderId, Action: string(suggestion.Kind), Outcome: outcome}))
}

// applyDockReply makes the changes den proposed after the user told it something about a suggestion.
// Unless den kept the suggestion itself, the user changed it, which counts as a no to the original.
func (currentModel *model) applyDockReply(item dockItem, actions []brain.ChatAction) {
	keepsOriginal := slices.ContainsFunc(actions, func(action brain.ChatAction) bool { return action.Kind == "acceptSuggestion" })
	others := slices.DeleteFunc(slices.Clone(actions), func(action brain.ChatAction) bool { return action.Kind == "acceptSuggestion" })
	answered := currentModel.repository.FindSuggestion(item.suggestionId) != nil
	if answered {
		currentModel.answerDockItem(item, keepsOriginal)
	}
	done := currentModel.doChatActions(others)
	if keepsOriginal {
		done = append([]string{"did what den suggested"}, done...)
	}
	currentModel.setStatus("done: " + strings.Join(done, "; "))
}

// reviseInput is what den's model gets when the user told it something about a suggestion in the dock:
// the suggestion spelled out, and the repo, as for a chat about it.
func (currentModel model) reviseInput() (brain.ChatInput, bool) {
	state := currentModel.dock
	suggestion := currentModel.repository.FindSuggestion(state.item.suggestionId)
	if suggestion == nil || state.asked == "" {
		return brain.ChatInput{}, false
	}
	tag, subject, details, why := currentModel.describeDockItem(state.item)
	about := tag + ": " + ansi.Strip(subject)
	for _, detail := range details {
		about += "\n" + ansi.Strip(detail)
	}
	if why != "" {
		about += "\nwhy: " + why
	}
	if suggestion.Kind == store.SuggestContext {
		about += "\nthe whole proposed text:\n" + suggestion.ProposedText
	}
	input := brain.ChatInput{
		Conversation:    []brain.ChatMessage{{Text: state.asked}},
		Repository:      &currentModel.repository,
		Digests:         currentModel.digests,
		About:           about,
		RepositoryNotes: currentModel.repository.ReadContext(""),
		SubjectSession:  state.item.sessionId,
	}
	if suggestion.FolderId != "" {
		input.SubjectFolder = suggestion.FolderId
		input.SubjectNotes = currentModel.folderNotesText(suggestion.FolderId)
	}
	if input.SubjectSession == "" && len(suggestion.SessionIds) == 1 {
		input.SubjectSession = suggestion.SessionIds[0]
	}
	return input, true
}

// applyReviseAnswer shows den's answer in the dock, with the changes it would make instead, waiting for enter.
func (currentModel *model) applyReviseAnswer(done job, answer brain.ChatAnswer, input brain.ChatInput) {
	state := &currentModel.dock
	state.thinking = false
	if state.item.suggestionId != done.suggestionId || state.item.sessionId != done.sessionId {
		return
	}
	actions := brain.ResolveChatActions(answer, input, currentModel.workingDirectory)
	text := replyText(answer.Reply, actions)
	for _, action := range actions {
		text += "\n· " + action.Description
		if action.Kind == "editNotes" {
			noteLines := strings.Split(strings.TrimSpace(action.Text), "\n")
			for _, line := range noteLines[:min(len(noteLines), dockNotesPreviewLines)] {
				text += "\n    " + line
			}
			if len(noteLines) > dockNotesPreviewLines {
				text += fmt.Sprintf("\n    … %d more lines", len(noteLines)-dockNotesPreviewLines)
			}
		}
	}
	state.reply = &brain.ChatMessage{FromDen: true, Text: text, Actions: actions}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "revise", SessionId: done.sessionId, Action: "reply",
		Detail: state.asked, Outcome: fmt.Sprintf("%d actions", len(actions))}))
}

// afterDockAnswer moves on to the next item in the same place, or leaves the dock when nothing is left.
func (currentModel *model) afterDockAnswer() {
	currentModel.dock.input.SetValue("")
	currentModel.clearDockReply()
	items := currentModel.dockItems()
	if len(items) == 0 {
		currentModel.blurDock()
		return
	}
	currentModel.dock.index = min(currentModel.dock.index, len(items)-1)
	currentModel.followDockItem()
}

// dockLines draw the dock: one line when the tree has focus, the whole card when the dock has it.
func (currentModel model) dockLines(width int) []string {
	item, exists := currentModel.currentDockItem()
	if !exists {
		return nil
	}
	theme := currentModel.theme
	tag, subject, details, why := currentModel.describeDockItem(item)
	lines := []string{theme.border.Render(strings.Repeat("─", width))}
	badge := theme.badge.Render(" " + tag + " ")
	if !currentModel.dock.focused {
		return append(lines, truncateStyled(badge+" "+subject, width))
	}
	items := currentModel.dockItems()
	position := fmt.Sprintf("%d of %d", min(currentModel.dock.index, len(items)-1)+1, len(items))
	lines = append(lines, currentModel.layoutRow([]segment{{text: " " + tag + " ", style: theme.badge}}, []segment{{text: position, style: theme.dim}}, width, false))
	lines = append(lines, wrapAll([]string{subject}, width)...)
	lines = append(lines, details...)
	if why != "" {
		lines = append(lines, wrapAll([]string{theme.dim.Render(why)}, width)...)
	}
	state := currentModel.dock
	if state.asked != "" {
		lines = append(lines, wrapAll([]string{theme.youLabel.Render("you ") + theme.text.Render(state.asked)}, width)...)
	}
	switch {
	case state.thinking:
		lines = append(lines, theme.denLabel.Render("den ")+theme.faint.Render("thinking…"))
	case state.reply != nil:
		for index, replyLine := range strings.Split(state.reply.Text, "\n") {
			label := "    "
			if index == 0 {
				label = theme.denLabel.Render("den ")
			}
			lines = append(lines, wrapAll([]string{label + theme.text.Render(replyLine)}, width)...)
		}
		if len(state.reply.Actions) > 0 {
			lines = append(lines, theme.key.Render("↵")+theme.action.Render(" do it  ")+theme.key.Render("⌫")+theme.action.Render(" back"))
		}
	}
	input := state.input
	input.SetWidth(width - 3)
	if state.reply != nil || state.thinking {
		input.Placeholder = "or tell den more…"
	}
	lines = append(lines, input.View())
	return lines[:min(len(lines), max(currentModel.height/maximumDockShare, 4))]
}

// describeDockItem says what a suggestion does in a fixed shape: a type tag, the things it changes in the tree's
// own colors, details, and why den suggests it.
func (currentModel model) describeDockItem(item dockItem) (tag string, subject string, details []string, why string) {
	theme := currentModel.theme
	repository := &currentModel.repository
	suggestion := repository.FindSuggestion(item.suggestionId)
	if suggestion == nil {
		return "", "", nil, ""
	}
	sessionName := func(sessionId string) string {
		if session := repository.FindSession(sessionId); session != nil {
			return theme.bold.Render("\"" + session.Title + "\"")
		}
		return theme.dim.Render("a session")
	}
	folderName := func(folderId string) string {
		if folderId == "" {
			return theme.folder.Render("Inbox")
		}
		return theme.folder.Render(repository.FolderName(folderId))
	}
	arrow := theme.dim.Render(" → ")
	listed := func(sessionIds []string) []string {
		var lines []string
		for _, sessionId := range sessionIds[:min(len(sessionIds), 3)] {
			lines = append(lines, theme.dim.Render("  · ")+sessionName(sessionId))
		}
		if len(sessionIds) > 3 {
			lines = append(lines, theme.faint.Render(fmt.Sprintf("  and %d more", len(sessionIds)-3)))
		}
		return lines
	}
	why = suggestion.Reason
	switch suggestion.Kind {
	case store.SuggestMove:
		tag = "MOVE"
		if len(suggestion.SessionIds) == 1 {
			subject = sessionName(suggestion.SessionIds[0]) + arrow + folderName(suggestion.FolderId)
		} else {
			subject = theme.bold.Render(countedSessions(len(suggestion.SessionIds))) + arrow + folderName(suggestion.FolderId)
			details = listed(suggestion.SessionIds)
		}
	case store.SuggestSubfolder:
		tag = "GROUP"
		newFolder := theme.folder.Render(subfolderLabel(*suggestion)) + theme.dim.Render(" (new, in ") + folderName(suggestion.ParentFolderId) + theme.dim.Render(")")
		subject = theme.bold.Render(countedSessions(len(suggestion.SessionIds))) + arrow + newFolder
		details = listed(suggestion.SessionIds)
		if suggestion.LinkedPath != "" {
			details = append(details, theme.dim.Render("  with ")+theme.link.Render(filepath.Base(suggestion.LinkedPath))+theme.dim.Render(" as its context"))
		}
	case store.SuggestCleanup:
		tag = "ARCHIVE"
		subject = sessionName(item.sessionId) + arrow + theme.archive.Render("Archive")
		if session := repository.FindSession(item.sessionId); session != nil {
			why = currentModel.proposalReason(*suggestion, listRow{kind: sessionRow, session: session})
		}
	case store.SuggestLink:
		tag = "LINK"
		subject = theme.link.Render(currentModel.relativeToRepository(suggestion.LinkedPath, "")) + arrow + theme.dim.Render("context of ") + folderName(suggestion.FolderId)
	case store.SuggestLaptopRule:
		tag = "RULE"
		subject = theme.bold.Render("\""+suggestion.Rule+"\"") + arrow + theme.link.Render(laptopFileNames(suggestion.FilePaths))
		why = strings.TrimPrefix(suggestion.Reason, "\""+suggestion.Rule+"\" ")
	case store.SuggestQuestion:
		tag = "ASK"
		subject = theme.bold.Render(suggestion.Question)
		if suggestion.Rule != "" {
			details = []string{theme.dim.Render("  yes adds: ") + theme.text.Render(suggestion.Rule)}
		}
	case store.SuggestContext:
		tag, subject, details = currentModel.describeContextChange(*suggestion)
	}
	return tag, subject, details, why
}

// describeContextChange tells a rule added to notes from other note changes, and a folder's first notes from an edit.
func (currentModel model) describeContextChange(suggestion store.Suggestion) (tag string, subject string, details []string) {
	theme := currentModel.theme
	target := theme.folder.Render("◆ " + currentModel.contextHeading(suggestion.FolderId))
	if suggestion.FolderId == "" {
		target = theme.folder.Render("◆ " + currentModel.repositoryName() + " context")
	}
	var added []string
	removedCount := 0
	for _, line := range lineDiff(currentModel.currentContextText(suggestion), suggestion.ProposedText) {
		switch {
		case line.kind == addedLine && strings.TrimSpace(line.text) != "":
			added = append(added, strings.TrimSpace(line.text))
		case line.kind == removedLine:
			removedCount++
		}
	}
	if suggestion.Starter != "" {
		source := "drafted from its sessions"
		if suggestion.Starter == store.StarterPlan {
			source = "the plan you approved"
		}
		seeThem := "shown above"
		if !currentModel.isWide() {
			seeThem = "→ shows them"
		}
		return "NOTES", theme.bold.Render("first notes") + arrowText(theme) + target, []string{theme.dim.Render(fmt.Sprintf("  %s · %d lines · %s", source, len(added), seeThem))}
	}
	if removedCount == 0 && len(added) <= 2 && sectionOf(suggestion.ProposedText, added) == "Rules" {
		rule := strings.TrimPrefix(added[len(added)-1], "- ")
		return "RULE", theme.bold.Render("\""+rule+"\"") + arrowText(theme) + target, nil
	}
	for _, line := range added[:min(len(added), 2)] {
		details = append(details, theme.diffAdded.Render("+ "+line))
	}
	return "NOTES", theme.bold.Render(fmt.Sprintf("+%d −%d lines", len(added), removedCount)) + arrowText(theme) + target, details
}

func arrowText(theme theme) string {
	return theme.dim.Render(" → ")
}

// sectionOf is the "## " heading the last added line sits under in the text.
func sectionOf(text string, added []string) string {
	if len(added) == 0 {
		return ""
	}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		if heading, isHeading := strings.CutPrefix(line, "## "); isHeading {
			section = strings.TrimSpace(heading)
		}
		if strings.TrimSpace(line) == added[len(added)-1] {
			return section
		}
	}
	return section
}

// dockHints are the footer keys while the dock has focus.
func (currentModel model) dockHints() []key.Binding {
	keys := currentModel.keys
	bindings := []key.Binding{relabeled(keys.up, "other suggestions")}
	switch {
	case len(currentModel.openAgents) == 0:
	case currentModel.isWide():
		bindings = append(bindings, key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "narrow again")))
	default:
		bindings = append(bindings, key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "see it whole")))
	}
	return append(bindings, relabeled(keys.back, "back to the tree"))
}
