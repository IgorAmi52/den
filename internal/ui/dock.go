package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	minimumTreeLinesBesideDock = 3
	dockScrollStep             = 5
)

type dockItem struct{ suggestionId string }

type dockState struct {
	focused    bool
	index      int
	input      textarea.Model
	item       dockItem
	bodyOffset int
	mode       string
	choice     int
}

func newDockState() dockState {
	return dockState{input: newMessageInput("↵ accept · ⌫ reject · or ask DEN…", 2000)}
}

func (currentModel model) dockItems() []dockItem {
	var items []dockItem
	for _, suggestion := range currentModel.repository.VisibleSuggestions() {
		items = append(items, dockItem{suggestionId: suggestion.Id})
	}
	return items
}

func (currentModel model) itemRow(item dockItem) func(listRow) bool {
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	if suggestion != nil {
		if suggestion.Kind == store.SuggestFolder && len(suggestion.SessionIds) > 0 {
			return func(row listRow) bool {
				return row.kind == sessionRow && row.session.SessionId == suggestion.SessionIds[0]
			}
		}
		if suggestion.FolderId != "" {
			return func(row listRow) bool { return row.kind == folderRow && row.folder.Id == suggestion.FolderId }
		}
	}
	return func(row listRow) bool { return row.kind == repositoryRow }
}

func (currentModel model) currentDockItem() (dockItem, bool) {
	items := currentModel.dockItems()
	if len(items) == 0 {
		return dockItem{}, false
	}
	if currentModel.dock.focused {
		if slices.Contains(items, currentModel.dock.item) {
			return currentModel.dock.item, true
		}
		return items[min(currentModel.dock.index, len(items)-1)], true
	}
	if selected, exists := currentModel.selectedRow(); exists {
		for _, item := range items {
			if currentModel.itemRow(item)(selected) {
				return item, true
			}
		}
	}
	for _, item := range items {
		if currentModel.repository.FindSuggestion(item.suggestionId).UnreadReply {
			return item, true
		}
	}
	return items[0], true
}

func (currentModel *model) persistDock() {
	if suggestion := currentModel.repository.FindSuggestion(currentModel.dock.item.suggestionId); suggestion != nil {
		if currentModel.dock.mode == "" {
			suggestion.Draft = currentModel.dock.input.Value()
		}
		maximumOffset := max(len(currentModel.cardBody(*suggestion, currentModel.panelWidth()))-currentModel.cardBodyHeight(currentModel.panelWidth()), 0)
		currentModel.dock.bodyOffset = min(currentModel.dock.bodyOffset, maximumOffset)
		suggestion.ScrollOffset = currentModel.dock.bodyOffset
		currentModel.reportError(currentModel.repository.Save())
	}
}

func (currentModel *model) loadDock(item dockItem) {
	currentModel.dock.item = item
	currentModel.dock.mode = ""
	if suggestion := currentModel.repository.FindSuggestion(item.suggestionId); suggestion != nil {
		currentModel.dock.input.SetValue(suggestion.Draft)
		currentModel.dock.bodyOffset = suggestion.ScrollOffset
	}
}

func (currentModel *model) focusDock() tea.Cmd {
	item, exists := currentModel.currentDockItem()
	if !exists {
		currentModel.setStatus("No visible cards; deferred proposals appear after revalidation.")
		return nil
	}
	currentModel.dock.focused = true
	currentModel.dock.index = max(slices.Index(currentModel.dockItems(), item), 0)
	currentModel.loadDock(item)
	currentModel.followDockItem()
	currentModel.persistDock()
	return currentModel.dock.input.Focus()
}

func (currentModel *model) blurDock() {
	currentModel.persistDock()
	currentModel.dock.focused = false
	currentModel.dock.input.Blur()
	currentModel.dock.mode = ""
}

func (currentModel *model) followDockItem() {
	item, exists := currentModel.currentDockItem()
	if !exists {
		return
	}
	if suggestion := currentModel.repository.FindSuggestion(item.suggestionId); suggestion != nil {
		currentModel.expandFolder(suggestion.FolderId)
		if suggestion.Kind == store.SuggestFolder {
			if session := firstSuggestionSession(&currentModel.repository, *suggestion); session != nil {
				currentModel.expandFolder(session.FolderId)
			}
		}
	}
	currentModel.cursorTo(currentModel.itemRow(item))
	currentModel.previewOffset = 0
}

func (currentModel model) updateDock(press tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	command := currentModel.handleDockKey(press)
	currentModel.persistDock()
	return currentModel, command
}

func (currentModel *model) handleDockKey(press tea.KeyPressMsg) tea.Cmd {
	state := &currentModel.dock
	item, exists := currentModel.currentDockItem()
	if !exists {
		currentModel.blurDock()
		return nil
	}
	if state.item != item {
		currentModel.loadDock(item)
	}
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	if key.Matches(press, currentModel.keys.back) || press.String() == "tab" {
		if state.mode != "" {
			currentModel.loadDock(item)
		} else {
			currentModel.blurDock()
		}
		return nil
	}
	if state.mode != "" {
		return currentModel.handleCardForm(press, suggestion)
	}
	empty := strings.TrimSpace(state.input.Value()) == ""
	switch {
	case press.String() == "ctrl+e":
		suggestion.EvidenceExpanded = !suggestion.EvidenceExpanded
	case press.String() == "ctrl+j":
		state.bodyOffset = 1000000
		suggestion.UnreadReply = false
	case press.String() == "ctrl+d" && !suggestion.ReplyPending:
		currentModel.persistDock()
		state.mode = "move"
		state.choice = 0
		state.input.SetValue("")
	case press.String() == "ctrl+r" && !suggestion.ReplyPending:
		state.input.SetValue("Please revise this proposal: ")
	case press.String() == "pgdown", press.String() == "shift+down":
		state.bodyOffset += dockScrollStep
	case press.String() == "pgup", press.String() == "shift+up":
		state.bodyOffset = max(state.bodyOffset-dockScrollStep, 0)
	case (press.String() == "right" || press.String() == "left") && empty:
		currentModel.toggleWide()
	case (press.String() == "up" || press.String() == "down") && empty:
		currentModel.persistDock()
		step := 1
		if press.String() == "up" {
			step = -1
		}
		items := currentModel.dockItems()
		state.index = max(min(slices.Index(items, item)+step, len(items)-1), 0)
		currentModel.loadDock(items[state.index])
		currentModel.followDockItem()
	case key.Matches(press, currentModel.keys.yes) && !empty && !suggestion.ReplyPending:
		if currentModel.helperCaller == nil {
			currentModel.setError("DEN needs its model connection to answer this question.")
			return nil
		}
		for index := range suggestion.Conversation {
			if len(suggestion.Conversation[index].Actions) > 0 {
				suggestion.Conversation[index].Outcome = "superseded"
			}
		}
		question := strings.TrimSpace(state.input.Value())
		suggestion.Conversation = append(suggestion.Conversation, store.CardMessage{Text: question})
		suggestion.ReplyPending = true
		suggestion.Draft = ""
		state.input.SetValue("")
		state.bodyOffset = 1000000
		currentModel.queueJob(job{kind: reviseJob, suggestionId: suggestion.Id, proposalRevision: suggestion.Revision})
		currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "revise", Action: "question", Detail: question, Outcome: "queued", CallId: suggestion.CallId, Suggestion: suggestion}))
		currentModel.persistDock()
		return currentModel.startNextJob()
	case key.Matches(press, currentModel.keys.yes) && empty && !suggestion.ReplyPending:
		if actions := cardActions(*suggestion); len(actions) > 0 {
			if actions[0].Kind == "withdrawSuggestion" {
				state.mode = "reject"
				state.choice = 0
				return nil
			}
		}
		currentModel.answerDockItem(item, true)
		currentModel.afterDockAnswer()
	case key.Matches(press, currentModel.keys.no) && empty && !suggestion.ReplyPending:
		currentModel.persistDock()
		state.mode = "reject"
		state.choice = 0
	default:
		var command tea.Cmd
		state.input, command = state.input.Update(press)
		return command
	}
	return nil
}

func (currentModel *model) handleCardForm(press tea.KeyPressMsg, suggestion *store.Suggestion) tea.Cmd {
	state := &currentModel.dock
	count := len(store.RejectionReasons)
	if state.mode == "move" {
		count = len(currentModel.repository.Folders) + 1
	}
	switch press.String() {
	case "up":
		state.choice = max(state.choice-1, 0)
		return nil
	case "down":
		state.choice = min(state.choice+1, count-1)
		return nil
	case "enter":
		if state.mode == "move" {
			destination := ""
			if state.choice > 0 {
				destination = currentModel.repository.Folders[state.choice-1].Id
			}
			currentModel.moveCard(suggestion, destination)
			return nil
		}
		reason := store.RejectionReasons[state.choice]
		explanation := strings.TrimSpace(state.input.Value())
		snapshot := *suggestion
		if currentModel.reportError(currentModel.repository.Reject(suggestion.Id, reason, explanation)) {
			return nil
		}
		currentModel.queueLesson(snapshot, reason, explanation)
		currentModel.saveRepository()
		currentModel.afterDockAnswer()
		return currentModel.startNextJob()
	}
	if state.mode == "reject" {
		var command tea.Cmd
		state.input, command = state.input.Update(press)
		return command
	}
	return nil
}

func (currentModel *model) answerDockItem(item dockItem, accepted bool) {
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	if suggestion == nil {
		return
	}
	if !accepted {
		currentModel.dock.mode = "reject"
		currentModel.dock.choice = 0
		return
	}
	snapshot := *suggestion
	currentModel.acceptSuggestion(suggestion.Id)
	if currentModel.repository.FindSuggestion(item.suggestionId) == nil {
		currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "dock", FolderId: snapshot.FolderId, Action: string(snapshot.Kind), Outcome: "accepted", CallId: snapshot.CallId, Suggestion: &snapshot}))
	}
}

func (currentModel model) reviseInput(identifier string) (brain.ChatInput, bool) {
	suggestion := currentModel.repository.FindSuggestion(identifier)
	if suggestion == nil || !suggestion.ReplyPending {
		return brain.ChatInput{}, false
	}
	var conversation []brain.ChatMessage
	for _, message := range suggestion.Conversation {
		conversation = append(conversation, brain.ChatMessage{FromDen: message.FromDen, Text: message.Text})
	}
	input := brain.ChatInput{Conversation: conversation, Repository: &currentModel.repository, Digests: currentModel.digests, About: currentModel.spelledOutCard(*suggestion), RepositoryNotes: currentModel.repository.ReadContext(""), Now: time.Now(), SubjectFolder: suggestion.FolderId}
	if len(suggestion.SessionIds) > 0 {
		input.SubjectSession = suggestion.SessionIds[0]
	}
	input.SubjectNotes = currentModel.folderNotesText(suggestion.FolderId)
	input.Transcripts = currentModel.chatTranscripts(input.SubjectFolder, input.SubjectSession)
	return input, true
}

func (currentModel model) spelledOutCard(suggestion store.Suggestion) string {
	destination := suggestion.FolderId
	if suggestion.Kind == store.SuggestFolder {
		destination = suggestion.ParentFolderId
	}
	lines := []string{suggestionTag(suggestion) + ": " + store.CardSummary(suggestion), "destination: " + currentModel.repository.ContextDestination(destination), "future use: " + suggestion.FutureUse, "reason: " + suggestion.Reason,
		"source: " + suggestion.Source, "evidence: " + strings.Join(suggestion.Evidence, "\n"), "whole proposed text:\n" + suggestion.ProposedText}
	if suggestion.LinkedPath != "" {
		lines = append(lines, "linked file: "+suggestion.LinkedPath, store.ReadText(suggestion.LinkedPath))
	}
	return strings.Join(lines, "\n")
}

func cardActions(suggestion store.Suggestion) []brain.ChatAction {
	if len(suggestion.Conversation) == 0 {
		return nil
	}
	message := suggestion.Conversation[len(suggestion.Conversation)-1]
	if message.Outcome != "" {
		return nil
	}
	var actions []brain.ChatAction
	json.Unmarshal(message.Actions, &actions)
	return actions
}

func (currentModel *model) applyReviseAnswer(done jobDoneMessage, answer brain.ChatAnswer, input brain.ChatInput) {
	suggestion := currentModel.repository.FindSuggestion(done.job.suggestionId)
	if suggestion == nil {
		return
	}
	actions := brain.ResolveChatActions(answer, input, currentModel.workingDirectory)
	if suggestion.Revision != done.job.proposalRevision || currentModel.proposalEnvironment(*suggestion) != done.proposalEnvironment {
		actions = nil
		answer.Reply += "\nThe proposal changed while I was answering; review its current version."
	}
	if len(actions) == 1 && actions[0].Kind != "acceptSuggestion" && actions[0].Kind != "withdrawSuggestion" {
		if currentModel.reviseCard(suggestion, actions[0]) {
			answer.Reply += "\nThe proposed change above is updated; it still needs your acceptance."
		} else {
			answer.Reply += "\nI could not revise this proposal. Its previous version is still waiting."
		}
		actions = nil
	} else if len(actions) > 1 {
		answer.Reply += "\nPlease request one card revision at a time. No changes have been applied."
		actions = nil
	}
	encoded, _ := json.Marshal(actions)
	suggestion.ReplyPending = false
	suggestion.UnreadReply = true
	suggestion.Conversation = append(suggestion.Conversation, store.CardMessage{FromDen: true, Text: answer.Reply, Actions: encoded, CallIdentifier: input.CallId})
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "revise", Action: "reply", Outcome: "answered", Detail: answer.Reply, CallId: input.CallId, Suggestion: suggestion}))
	currentModel.saveRepository()
}

func (currentModel *model) afterDockAnswer() {
	items := currentModel.dockItems()
	currentModel.dock.mode = ""
	if len(items) == 0 {
		currentModel.blurDock()
		return
	}
	currentModel.dock.index = min(currentModel.dock.index, len(items)-1)
	if index := slices.Index(items, currentModel.dock.item); index >= 0 {
		currentModel.dock.index = index
	}
	currentModel.loadDock(items[currentModel.dock.index])
	currentModel.followDockItem()
}

func (currentModel model) cardBody(suggestion store.Suggestion, width int) []string {
	theme := currentModel.theme
	_, subject, details, _ := currentModel.describeDockItem(dockItem{suggestion.Id}, width)
	body := []string{theme.bold.Render("Proposed change")}
	body = append(body, wrapAll([]string{subject}, width)...)
	body = append(body, details...)
	if suggestion.ReviewConcern != "" {
		body = append(body, "", theme.bold.Render("Needs revision"), suggestion.ReviewConcern)
	}
	body = append(body, "", theme.bold.Render("Why this helps a future session"))
	usefulness := suggestion.FutureUse
	if usefulness == "" {
		usefulness = suggestion.Reason
	}
	body = append(body, wrapAll([]string{usefulness}, width)...)
	body = append(body, "", theme.bold.Render("Source"))
	body = append(body, wrapAll([]string{suggestion.Source}, width)...)
	body = append(body, "", theme.bold.Render("Evidence · ctrl+e to expand/collapse"))
	if suggestion.EvidenceExpanded {
		for _, quote := range suggestion.Evidence {
			body = append(body, wrapAll([]string{"“" + quote + "”"}, width)...)
		}
	} else {
		body = append(body, theme.dim.Render(fmt.Sprintf("%d supporting quotes", len(suggestion.Evidence))))
	}
	if len(suggestion.Conversation) > 0 {
		body = append(body, "", theme.bold.Render("Conversation"))
		for _, message := range suggestion.Conversation {
			speaker := "You: "
			if message.FromDen {
				speaker = "DEN: "
			}
			body = append(body, wrapAll([]string{speaker + message.Text}, width)...)
			body = append(body, "")
		}
	}
	return wrapAll(body, width)
}

func (currentModel model) cardBodyHeight(width int) int {
	input := currentModel.dock.input
	input.SetWidth(max(width, 1))
	return max(currentModel.height-currentModel.treeTop()-currentModel.footerHeight()-minimumTreeLinesBesideDock-4-strings.Count(input.View(), "\n")-1, 3)
}

func (currentModel model) dockLines(width int) []string {
	item, exists := currentModel.currentDockItem()
	if !exists {
		if len(currentModel.repository.Suggestions) > 0 {
			return []string{currentModel.theme.dim.Render(fmt.Sprintf("%d proposals queued for revalidation", len(currentModel.repository.Suggestions)))}
		}
		return nil
	}
	suggestion := currentModel.repository.FindSuggestion(item.suggestionId)
	theme := currentModel.theme
	tag, subject, _, _ := currentModel.describeDockItem(item, width)
	status := "Waiting for your decision"
	if suggestion.ReviewConcern != "" {
		status = "Needs revision before acceptance"
	}
	if suggestion.ReplyPending {
		status = "DEN is answering"
	} else if suggestion.UnreadReply {
		status = "New reply · ctrl+j to read"
	}
	rule := theme.border.Render(strings.Repeat("─", max(width, 0)))
	if !currentModel.dock.focused {
		return []string{rule, truncateStyled(theme.badge.Render(" "+tag+" ")+" "+subject, width), truncateStyled(theme.dim.Render(status), width)}
	}
	state := currentModel.dock
	visible := len(currentModel.dockItems())
	header := []string{rule, truncateStyled(fmt.Sprintf("%s · %d/%d · %d queued", tag, slices.Index(currentModel.dockItems(), item)+1, visible, len(currentModel.repository.Suggestions)-visible), width), truncateStyled(theme.dim.Render(status), width)}
	body := currentModel.cardBody(*suggestion, width)
	input := state.input
	input.SetWidth(max(width, 1))
	if suggestion.ReplyPending {
		input.Placeholder = "DEN is answering; your draft will be saved…"
	}
	choiceOffset := 0
	if state.mode != "" {
		body = nil
		if state.mode == "reject" {
			body = append(body, theme.bold.Render("Why reject this proposal?"))
			for index, reason := range store.RejectionReasons {
				prefix := "  "
				if index == state.choice {
					prefix = "› "
					choiceOffset = len(body)
				}
				body = append(body, wrapAll([]string{prefix + reason}, width)...)
			}
			body = append(body, "↑↓ choose · ↵ reject · esc cancel")
			input.Placeholder = "Optional explanation; required for Something else"
		} else {
			body = append(body, theme.bold.Render("Choose the destination"))
			names := []string{"Repo context"}
			for _, folder := range currentModel.repository.Folders {
				names = append(names, currentModel.repository.ContextDestination(folder.Id))
			}
			for index, name := range names {
				prefix := "  "
				if index == state.choice {
					prefix = "› "
					choiceOffset = len(body)
				}
				body = append(body, wrapAll([]string{prefix + name}, width)...)
			}
			body = append(body, "↑↓ choose · ↵ move proposal · esc cancel")
		}
	}
	body = wrapAll(body, width)
	footer := strings.Split(input.View(), "\n")
	bodyHeight := currentModel.cardBodyHeight(width)
	offset := min(max(state.bodyOffset, 0), max(len(body)-bodyHeight, 0))
	if state.mode != "" {
		offset = max(min(choiceOffset-1, max(len(body)-bodyHeight, 0)), 0)
	}
	end := min(offset+bodyHeight, len(body))
	shown := append([]string{}, body[offset:end]...)
	if len(body) > bodyHeight {
		shown = append(shown, truncateStyled(theme.dim.Render(fmt.Sprintf("Lines %d–%d/%d · ⇧↑↓", offset+1, end, len(body))), width))
	}
	return append(append(header, shown...), footer...)
}

func (currentModel model) dockHints() []key.Binding {
	return []key.Binding{hint("↵", "accept/send"), hint("⌫", "reject with reason"), hint("ctrl+d", "destination"), hint("ctrl+r", "revise"), hint("ctrl+e", "evidence"), hint("↑↓", "cards"), hint("esc", "leave")}
}

func (currentModel *model) queueLesson(suggestion store.Suggestion, reason string, explanation string) {
	card := currentModel.spelledOutCard(suggestion) + "\nRejection reason: " + reason + "\nExplanation: " + explanation
	for _, message := range suggestion.Conversation {
		speaker := "user"
		if message.FromDen {
			speaker = "den"
		}
		card += "\n" + speaker + ": " + message.Text
	}
	currentModel.queueJob(job{kind: lessonJob, rejectedCard: card})
}

func firstSuggestionSession(repository *store.Repository, suggestion store.Suggestion) *store.Session {
	if len(suggestion.SessionIds) == 0 {
		return nil
	}
	return repository.FindSession(suggestion.SessionIds[0])
}
func (currentModel model) describeDockItem(item dockItem, width int) (tag string, subject string, details []string, why string) {
	theme := currentModel.theme
	repository := &currentModel.repository
	suggestion := repository.FindSuggestion(item.suggestionId)
	if suggestion == nil {
		return "", "", nil, ""
	}
	tag, why = suggestionTag(*suggestion), suggestion.Reason
	contextName := theme.folder.Render("◆ " + repository.ContextDestination(suggestion.FolderId))
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
		subject = theme.text.Render("Update ") + theme.folder.Render(repository.ContextDestination(suggestion.FolderId))
		details = currentModel.diffLines(currentModel.currentContextText(*suggestion), suggestion.ProposedText, width)
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
		} else if session := firstSuggestionSession(repository, *suggestion); session != nil {
			subject += theme.dim.Render(joiner) + theme.bold.Render("\""+session.Title+"\"")
		}
		if suggestion.ParentFolderId != "" {
			details = append(details, theme.dim.Render("  inside ")+theme.folder.Render(repository.ContextDestination(suggestion.ParentFolderId)))
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
		if session := firstSuggestionSession(repository, *suggestion); session != nil && session.FolderId != "" {
			keepIn = repository.FolderName(session.FolderId)
		}
		details = []string{theme.dim.Render("  agents follow it in every repo · or tell den \"keep it in " + keepIn + "\"")}
	}
	return tag, subject, details, why
}

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
