package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	// minimumTreeLinesBesideDock keeps a few tree lines visible above an open card, so it's clear what it's about.
	minimumTreeLinesBesideDock = 5
	// dockNotesPreviewLines is how much of rewritten notes the dock shows before enter saves them.
	dockNotesPreviewLines = 8
	dockScrollStep        = 5
	// reviseTextWidth lays a card out for den's model, which reads it without a screen.
	reviseTextWidth = 200
)

// dockItem is one decision in the dock: one suggestion.
type dockItem struct {
	suggestionId string
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
	// bodyOffset scrolls a card too long for the panel, so the whole proposed context can be read.
	bodyOffset int
	// discussion is the last exchange about the card discussedId, kept after the reply is cleared, for the lesson of a no.
	discussion  string
	discussedId string
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

// itemRow matches the tree row an item is about: the session a new folder takes in, the folder whose context changes,
// or the repo row for the repo's context.
func (currentModel model) itemRow(item dockItem) func(listRow) bool {
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	switch {
	case suggestion == nil:
	case suggestion.Kind == store.SuggestFolder && len(suggestion.SessionIds) > 0:
		sessionId := suggestion.SessionIds[0]
		return func(row listRow) bool { return row.kind == sessionRow && row.session.SessionId == sessionId }
	case suggestion.FolderId != "":
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
		currentModel.setStatus("all quiet · den speaks up only when it's sure")
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
	if currentModel.dock.asked != "" {
		currentModel.dock.discussion, currentModel.dock.discussedId = currentModel.dockDiscussion(), currentModel.dock.item.suggestionId
	}
	currentModel.dock.asked, currentModel.dock.reply, currentModel.dock.thinking = "", nil, false
	currentModel.dock.bodyOffset = 0
}

// followDockItem selects the item's row in the tree, opening its folder, so it's clear what the suggestion is about.
func (currentModel *model) followDockItem() {
	item, exists := currentModel.currentDockItem()
	if !exists {
		return
	}
	if suggestion := currentModel.repository.FindSuggestion(item.suggestionId); suggestion != nil && suggestion.Kind == store.SuggestFolder && len(suggestion.SessionIds) > 0 {
		if session := currentModel.repository.FindSession(suggestion.SessionIds[0]); session != nil {
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
	case keyPress.String() == "pgdown":
		state.bodyOffset += dockScrollStep
		return currentModel, nil
	case keyPress.String() == "pgup":
		state.bodyOffset = max(state.bodyOffset-dockScrollStep, 0)
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
		currentModel.queueJob(job{kind: reviseJob, suggestionId: state.item.suggestionId})
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

// answerDockItem says yes or no to an item.
func (currentModel *model) answerDockItem(item dockItem, accepted bool) {
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	if suggestion == nil {
		return
	}
	logged := store.Decision{Job: "dock", FolderId: suggestion.FolderId, Action: string(suggestion.Kind), Detail: suggestion.Reason, Outcome: "rejected"}
	if len(suggestion.SessionIds) > 0 {
		logged.SessionId = suggestion.SessionIds[0]
	}
	if accepted {
		logged.Outcome = "accepted"
		currentModel.acceptSuggestion(suggestion.Id)
	} else {
		currentModel.queueLesson(item, *suggestion)
		currentModel.rejectSuggestion(suggestion.Id)
	}
	currentModel.reportError(currentModel.repository.LogDecision(logged))
}

// applyDockReply makes the changes den proposed after the user told it something about a suggestion.
// Unless den kept the suggestion itself, the user changed it, which counts as a no to the original.
func (currentModel *model) applyDockReply(item dockItem, actions []brain.ChatAction) {
	keepsOriginal := slices.ContainsFunc(actions, func(action brain.ChatAction) bool { return action.Kind == "acceptSuggestion" })
	others := slices.DeleteFunc(slices.Clone(actions), func(action brain.ChatAction) bool {
		return action.Kind == "acceptSuggestion" || action.Kind == "withdrawSuggestion"
	})
	answered := currentModel.repository.FindSuggestion(item.suggestionId) != nil
	if answered {
		currentModel.answerDockItem(item, keepsOriginal)
	}
	done := currentModel.doChatActions(others)
	switch {
	case keepsOriginal:
		done = append([]string{"did what den suggested"}, done...)
	case len(others) == 0:
		done = []string{"dropped it · den won't offer it again"}
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
	about := currentModel.spelledOutCard(state.item, *suggestion)
	input := brain.ChatInput{
		Conversation:    []brain.ChatMessage{{Text: state.asked}},
		Repository:      &currentModel.repository,
		Digests:         currentModel.digests,
		About:           about,
		RepositoryNotes: currentModel.repository.ReadContext(""),
	}
	if len(suggestion.SessionIds) == 1 {
		input.SubjectSession = suggestion.SessionIds[0]
	}
	input.SubjectFolder = suggestion.FolderId
	if session := currentModel.repository.FindSession(input.SubjectSession); session != nil && suggestion.Kind == store.SuggestRule {
		input.SubjectFolder = session.FolderId
	}
	if input.SubjectFolder != "" {
		input.SubjectNotes = currentModel.folderNotesText(input.SubjectFolder)
	}
	input.Transcripts = currentModel.chatTranscripts(input.SubjectFolder, input.SubjectSession)
	return input, true
}

// spelledOutCard is a card as plain text for den's model: what it does, why, the evidence and the whole proposed text.
func (currentModel model) spelledOutCard(item dockItem, suggestion store.Suggestion) string {
	tag, subject, details, why := currentModel.describeDockItem(item, reviseTextWidth)
	card := tag + ": " + ansi.Strip(subject)
	for _, detail := range details {
		card += "\n" + ansi.Strip(detail)
	}
	if why != "" {
		card += "\nwhy: " + why
	}
	if len(suggestion.Evidence) > 0 {
		card += "\nwhat den based it on, quoted from the session:\n- " + strings.Join(suggestion.Evidence, "\n- ")
	}
	if suggestion.ProposedText != "" {
		card += "\nthe whole proposed text:\n" + suggestion.ProposedText
	}
	return card
}

// applyReviseAnswer shows den's answer in the dock, with the changes it would make instead, waiting for enter.
func (currentModel *model) applyReviseAnswer(done job, answer brain.ChatAnswer, input brain.ChatInput) {
	state := &currentModel.dock
	state.thinking = false
	if state.item.suggestionId != done.suggestionId {
		return
	}
	actions := brain.ResolveChatActions(answer, input, currentModel.workingDirectory)
	state.reply = &brain.ChatMessage{FromDen: true, Text: replyText(answer.Reply, actions), Actions: actions}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "revise", Action: "reply",
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
// The card's header and input always show; its body scrolls when it's longer than the panel allows.
func (currentModel model) dockLines(width int) []string {
	item, exists := currentModel.currentDockItem()
	if !exists {
		return nil
	}
	theme := currentModel.theme
	tag, subject, details, why := currentModel.describeDockItem(item, width)
	rule := theme.border.Render(strings.Repeat("─", width))
	if !currentModel.dock.focused {
		return []string{rule, truncateStyled(theme.badge.Render(" "+tag+" ")+" "+subject, width)}
	}
	items := currentModel.dockItems()
	position := fmt.Sprintf("%d of %d", min(currentModel.dock.index, len(items)-1)+1, len(items))
	header := []string{rule, currentModel.layoutRow([]segment{{text: " " + tag + " ", style: theme.badge}}, []segment{{text: position, style: theme.dim}}, width, false)}
	body := wrapAll(append([]string{subject}, details...), width)
	if why != "" {
		body = append(body, wrapAll([]string{theme.dim.Render(why)}, width)...)
	}
	state := currentModel.dock
	var footer []string
	if state.asked != "" {
		footer = append(footer, wrapAll([]string{theme.youLabel.Render("you ") + theme.text.Render(state.asked)}, width)...)
	}
	switch {
	case state.thinking:
		footer = append(footer, theme.denLabel.Render("den ")+theme.faint.Render("thinking…"))
	case state.reply != nil:
		for index, replyLine := range strings.Split(state.reply.Text, "\n") {
			label := "    "
			if index == 0 {
				label = theme.denLabel.Render("den ")
			}
			footer = append(footer, wrapAll([]string{label + theme.text.Render(replyLine)}, width)...)
		}
		if len(state.reply.Actions) > 0 {
			confirmKeys := []key.Binding{relabeled(currentModel.keys.yes, "confirm"), relabeled(currentModel.keys.no, "back")}
			footer = append(footer, currentModel.planCard(state.reply.Actions, "", confirmKeys, width)...)
		}
	}
	input := state.input
	input.SetWidth(width - 3)
	if state.reply != nil || state.thinking {
		input.Placeholder = "or tell den more…"
	}
	footer = append(footer, input.View())
	bodyHeight := max(currentModel.height-currentModel.treeTop()-currentModel.footerHeight()-minimumTreeLinesBesideDock-len(header)-len(footer), 3)
	if len(body) > bodyHeight {
		offset := min(state.bodyOffset, len(body)-bodyHeight+1)
		shown := body[offset : offset+bodyHeight-1]
		more := fmt.Sprintf("↓ %d more lines · pgdn", len(body)-offset-len(shown))
		if offset+len(shown) >= len(body) {
			more = "↑ pgup for the start"
		}
		body = append(shown, theme.faint.Render(more))
	}
	return append(append(header, body...), footer...)
}

// describeDockItem says what a suggestion does in a fixed shape: a type tag, one sentence of what yes does
// in the tree's own colors, everything it would write, and why den suggests it.
func (currentModel model) describeDockItem(item dockItem, width int) (tag string, subject string, details []string, why string) {
	theme := currentModel.theme
	repository := &currentModel.repository
	suggestion := repository.FindSuggestion(item.suggestionId)
	if suggestion == nil {
		return "", "", nil, ""
	}
	tag, why = suggestionTag(*suggestion), suggestion.Reason
	contextName := theme.folder.Render("◆ " + currentModel.contextHeading(suggestion.FolderId))
	writtenText := func(text string) []string {
		var lines []string
		if strings.TrimSpace(text) == "" {
			return nil
		}
		for _, line := range currentModel.renderedMarkdown(text, width-2) {
			lines = append(lines, "  "+line)
		}
		return lines
	}
	switch suggestion.Kind {
	case store.SuggestContext:
		current := currentModel.currentContextText(*suggestion)
		added := addedLines(current, suggestion.ProposedText)
		switch {
		case strings.TrimSpace(current) == "":
			subject = theme.text.Render("Start ") + contextName + theme.text.Render(fmt.Sprintf("'s notes, %d %s:", len(added), plural(len(added), "line")))
			details = writtenText(suggestion.ProposedText)
		case len(added) == 1:
			subject = theme.text.Render("Add to ") + contextName + theme.text.Render(": ") + theme.bold.Render(strings.TrimPrefix(added[0], "- "))
		default:
			subject = theme.text.Render(fmt.Sprintf("Add %d lines to ", len(added))) + contextName + theme.text.Render(":")
			for _, line := range added {
				details = append(details, hangingIndent(strings.TrimPrefix(line, "- "), theme.diffAdded, width, "+ ", "  ")...)
			}
		}
	case store.SuggestLink:
		subject = theme.text.Render("Use ") + theme.link.Render(currentModel.relativeToRepository(suggestion.LinkedPath, "")) +
			theme.text.Render(" as ") + contextName + theme.text.Render("'s context:")
		details = writtenText(store.ReadText(suggestion.LinkedPath))
	case store.SuggestFolder:
		verb, joiner := "New folder ", " for "
		if suggestion.Skill {
			verb, joiner = "New skill folder ", " from "
		}
		subject = theme.text.Render(verb) + theme.folder.Render("▸ "+suggestion.FolderName)
		if len(suggestion.SessionIds) == 0 {
			subject += theme.dim.Render(joiner) + theme.bold.Render(suggestion.Source)
		} else if session := repository.FindSession(suggestion.SessionIds[0]); session != nil {
			subject += theme.dim.Render(joiner) + theme.bold.Render("\""+session.Title+"\"")
		}
		if suggestion.ParentFolderId != "" {
			details = append(details, theme.dim.Render("  inside ")+theme.folder.Render(repository.FolderName(suggestion.ParentFolderId)))
		}
		if suggestion.LinkedPath != "" {
			details = append(details, theme.dim.Render("  context: ")+theme.link.Render(currentModel.relativeToRepository(suggestion.LinkedPath, "")))
			details = append(details, writtenText(store.ReadText(suggestion.LinkedPath))...)
		} else {
			details = append(details, theme.dim.Render("  its context:"))
			details = append(details, writtenText(suggestion.ProposedText)...)
		}
	case store.SuggestRule:
		subject = theme.text.Render("Add to ") + theme.link.Render(laptopFileNames(suggestion.FilePaths)) + theme.text.Render(": ") + theme.bold.Render(suggestion.Rule)
		keepIn := "a folder"
		if session := repository.FindSession(suggestion.SessionIds[0]); session != nil && session.FolderId != "" {
			keepIn = repository.FolderName(session.FolderId)
		}
		details = []string{theme.dim.Render("  agents follow it in every repo · or tell den \"keep it in " + keepIn + "\"")}
	}
	for _, quote := range suggestion.Evidence {
		details = append(details, hangingIndent("“"+quote+"”", theme.faint, width, "  ", "   ")...)
	}
	return tag, subject, details, why
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

// hangingIndent wraps a line to width, the first piece after first and the rest after continuation,
// so a wrapped bullet stays visibly one bullet.
func hangingIndent(text string, style lipgloss.Style, width int, first string, continuation string) []string {
	if text == "" {
		return []string{""}
	}
	var lines []string
	for index, piece := range wrapText(text, max(width-len(continuation), 1)) {
		prefix := continuation
		if index == 0 {
			prefix = first
		}
		lines = append(lines, style.Render(prefix+piece))
	}
	return lines
}

// queueLesson has den learn from a card the user said no to, with what the user told den about it and what den answered.
func (currentModel *model) queueLesson(item dockItem, suggestion store.Suggestion) {
	card := currentModel.spelledOutCard(item, suggestion)
	state := currentModel.dock
	switch {
	case state.item.suggestionId == suggestion.Id && state.asked != "":
		card += "\n\n" + currentModel.dockDiscussion()
	case state.discussedId == suggestion.Id:
		card += "\n\n" + state.discussion
	}
	currentModel.queueJob(job{kind: lessonJob, rejectedCard: card})
}

func (currentModel model) dockDiscussion() string {
	discussion := "the user told den about it: " + currentModel.dock.asked
	if reply := currentModel.dock.reply; reply != nil {
		discussion += "\nden answered: " + reply.Text
	}
	return discussion
}
