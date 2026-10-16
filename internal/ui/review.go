package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/IgorAmi52/den/internal/store"
)

const maximumCardWidth = 74

// reviewState walks the queue one card at a time, like git add -p.
type reviewState struct {
	// startAt puts one suggestion first, when the review was opened on it.
	startAt string
	skipped map[string]bool
	// reviewed counts the cards answered since the queue opened, for "2 of 3".
	reviewed int
	// cleanupCursor and cleanupKept are the row and the unchecked sessions of a cleanup card.
	cleanupCursor int
	cleanupKept   map[string]bool
}

func newReviewState() reviewState {
	return reviewState{skipped: map[string]bool{}, cleanupKept: map[string]bool{}}
}

func (currentModel *model) openReview() {
	currentModel.review = newReviewState()
	currentModel.openScreen(reviewScreen)
}

// reviewQueue lists the suggestions still to look at this round.
func (currentModel model) reviewQueue() []*store.Suggestion {
	var queue []*store.Suggestion
	for index := range currentModel.repository.Suggestions {
		suggestion := &currentModel.repository.Suggestions[index]
		if currentModel.review.skipped[suggestion.Id] {
			continue
		}
		if suggestion.Id == currentModel.review.startAt {
			queue = append([]*store.Suggestion{suggestion}, queue...)
		} else {
			queue = append(queue, suggestion)
		}
	}
	return queue
}

func (currentModel model) currentCard() *store.Suggestion {
	if queue := currentModel.reviewQueue(); len(queue) > 0 {
		return queue[0]
	}
	return nil
}

func (currentModel model) updateReview(message tea.Msg) (tea.Model, tea.Cmd) {
	keyPress, isKeyPress := message.(tea.KeyPressMsg)
	if currentModel.prompt != nil {
		return currentModel.updatePrompt(message)
	}
	if !isKeyPress {
		return currentModel, nil
	}
	if currentModel.picker != nil {
		return currentModel.updateFolderPicker(keyPress)
	}
	currentModel.clearStatus()
	keys := currentModel.keys
	card := currentModel.currentCard()
	if card == nil || key.Matches(keyPress, keys.done) {
		if key.Matches(keyPress, keys.done) || key.Matches(keyPress, keys.no) || key.Matches(keyPress, keys.open) {
			currentModel.closeScreen()
		}
		return currentModel, nil
	}
	suggestionId := card.Id
	switch {
	case key.Matches(keyPress, keys.yes):
		if card.Kind == store.SuggestCleanup {
			currentModel.acceptCleanup(card)
		} else {
			currentModel.acceptSuggestion(suggestionId)
		}
		currentModel.nextCard()
	case key.Matches(keyPress, keys.no):
		currentModel.rejectSuggestion(suggestionId)
		currentModel.nextCard()
	case key.Matches(keyPress, keys.skip):
		currentModel.review.skipped[suggestionId] = true
		currentModel.review.startAt = ""
		currentModel.review.cleanupCursor = 0
	case key.Matches(keyPress, keys.ask):
		return currentModel, currentModel.openChat(card)
	case key.Matches(keyPress, keys.help):
		currentModel.openScreen(helpScreen)
	case card.Kind == store.SuggestCleanup:
		currentModel.updateCleanupCard(keyPress, card)
	case key.Matches(keyPress, keys.edit):
		return currentModel, currentModel.editCard(card)
	}
	return currentModel, nil
}

func (currentModel *model) nextCard() {
	currentModel.review.reviewed++
	currentModel.review.startAt = ""
	currentModel.review.cleanupCursor = 0
	currentModel.review.cleanupKept = map[string]bool{}
}

func (currentModel *model) updateCleanupCard(keyPress tea.KeyPressMsg, card *store.Suggestion) {
	keys := currentModel.keys
	state := &currentModel.review
	// The cleanup's sessions change as den observes, so the cursor may point past a shorter list.
	if len(card.SessionIds) == 0 {
		return
	}
	state.cleanupCursor = min(state.cleanupCursor, len(card.SessionIds)-1)
	switch {
	case key.Matches(keyPress, keys.up):
		state.cleanupCursor = max(state.cleanupCursor-1, 0)
	case key.Matches(keyPress, keys.down):
		state.cleanupCursor = min(state.cleanupCursor+1, len(card.SessionIds)-1)
	case key.Matches(keyPress, keys.toggle):
		sessionId := card.SessionIds[state.cleanupCursor]
		state.cleanupKept[sessionId] = !state.cleanupKept[sessionId]
		state.cleanupCursor = min(state.cleanupCursor+1, len(card.SessionIds)-1)
	}
}

// acceptCleanup archives the checked sessions; the unchecked ones are kept and not offered again.
func (currentModel *model) acceptCleanup(card *store.Suggestion) {
	var archived, kept []string
	for _, sessionId := range card.SessionIds {
		if currentModel.review.cleanupKept[sessionId] {
			kept = append(kept, sessionId)
		} else {
			archived = append(archived, sessionId)
		}
	}
	if len(kept) > 0 {
		keptPart := *card
		keptPart.SessionIds = kept
		currentModel.repository.RejectedKeys = append(currentModel.repository.RejectedKeys, keptPart.RejectionKeys()...)
	}
	card.SessionIds = archived
	currentModel.acceptSuggestion(card.Id)
}

// editCard changes a suggestion before accepting it: another folder for a move, another name for a subfolder,
// or the proposed text itself for a context edit.
func (currentModel *model) editCard(card *store.Suggestion) tea.Cmd {
	suggestionId := card.Id
	switch card.Kind {
	case store.SuggestMove:
		currentModel.pickFolder("File it in", func(currentModel *model, folderId string) {
			if suggestion := currentModel.repository.FindSuggestion(suggestionId); suggestion != nil {
				suggestion.FolderId = folderId
				currentModel.acceptSuggestion(suggestionId)
				currentModel.nextCard()
			}
		})
	case store.SuggestSubfolder:
		return currentModel.askForText("Subfolder name", card.FolderName, func(currentModel *model, name string) {
			if suggestion := currentModel.repository.FindSuggestion(suggestionId); suggestion != nil {
				suggestion.FolderName = name
				currentModel.acceptSuggestion(suggestionId)
				currentModel.nextCard()
			}
		})
	case store.SuggestContext:
		return currentModel.editDraft("✦ "+currentModel.contextFileName(*card)+" · proposed", card.ProposedText, func(currentModel *model, text string) {
			if suggestion := currentModel.repository.FindSuggestion(suggestionId); suggestion != nil {
				suggestion.ProposedText = text
				currentModel.acceptSuggestion(suggestionId)
				currentModel.nextCard()
			}
		})
	}
	return nil
}

func (currentModel model) reviewView() string {
	theme := currentModel.theme
	width := currentModel.width
	lines := []string{currentModel.screenTopBar("review", width), theme.border.Render(strings.Repeat("─", width)), ""}
	card := currentModel.currentCard()
	cardWidth := min(maximumCardWidth, width-4)
	if card == nil {
		message := []string{theme.suggestion.Render("✦ Nothing to review"), ""}
		if skipped := len(currentModel.review.skipped); skipped > 0 && len(currentModel.repository.Suggestions) > 0 {
			message = append(message, theme.dim.Render(fmt.Sprintf("%d skipped, still waiting in the queue.", len(currentModel.repository.Suggestions))))
		} else {
			message = append(message, wrapAll([]string{theme.dim.Render("den suggests only when it's sure. New suggestions show up as faint text on rows and in the ✦ badge.")}, cardWidth-6)...)
		}
		message = append(append(message, ""), currentModel.hintLines([]key.Binding{relabeled(currentModel.keys.done, "back")}, cardWidth, 1)...)
		lines = append(lines, centered(theme.card.Width(cardWidth).Render(strings.Join(message, "\n")), width)...)
		return strings.Join(lines, "\n")
	}
	innerWidth := cardWidth - 6
	queueLength := len(currentModel.reviewQueue())
	position := currentModel.review.reviewed + len(currentModel.review.skipped) + 1
	total := currentModel.review.reviewed + len(currentModel.review.skipped) + queueLength
	label, _ := currentModel.suggestionSummary(*card)
	if card.Kind == store.SuggestContext {
		label = "Context · ◆ " + currentModel.contextHeading(card.FolderId)
		if card.Guide != "" {
			label = "Guide · " + card.Guide + ".md"
		}
	}
	cardLines := []string{currentModel.layoutRow([]segment{{text: "✦ " + label, style: theme.suggestionBold}},
		[]segment{{text: fmt.Sprintf("%d of %d", position, total), style: theme.dim}}, innerWidth, false), ""}
	cardLines = append(cardLines, currentModel.suggestionBody(*card, innerWidth)...)
	cardLines = append(cardLines, "")
	cardLines = append(cardLines, currentModel.hintLines(currentModel.cardHints(*card), innerWidth, 2)...)
	lines = append(lines, centered(theme.card.Width(cardWidth).Render(strings.Join(cardLines, "\n")), width)...)
	if currentModel.prompt != nil {
		lines = append(append(lines, ""), centered(theme.dim.Render(currentModel.prompt.question+"  ")+currentModel.prompt.input.View(), width)...)
	} else if currentModel.statusMessage != "" {
		lines = append(lines, centered(theme.dim.Render(currentModel.statusMessage), width)...)
	} else {
		lines = append(lines, centered(theme.faint.Render("a rejected suggestion is remembered and never offered again"), width)...)
	}
	content := strings.Join(lines, "\n")
	if currentModel.picker != nil {
		content = currentModel.withOverlay(content, currentModel.folderPickerBox())
	}
	return content
}

func (currentModel model) cardHints(card store.Suggestion) []key.Binding {
	keys := currentModel.keys
	switch card.Kind {
	case store.SuggestMove:
		return []key.Binding{relabeled(keys.yes, "move"), keys.no, relabeled(keys.edit, "other folder"), keys.skip, keys.ask, keys.done}
	case store.SuggestSubfolder:
		return []key.Binding{relabeled(keys.yes, "create"), keys.no, relabeled(keys.edit, "rename"), keys.skip, keys.ask, keys.done}
	case store.SuggestCleanup:
		archiveCount := 0
		for _, sessionId := range card.SessionIds {
			if !currentModel.review.cleanupKept[sessionId] {
				archiveCount++
			}
		}
		return []key.Binding{relabeled(keys.yes, fmt.Sprintf("archive %d", archiveCount)), keys.toggle, relabeled(keys.no, "keep all"), keys.skip, keys.done}
	}
	return []key.Binding{relabeled(keys.yes, "apply"), keys.no, keys.edit, keys.skip, keys.ask, keys.done}
}

// suggestionBody describes a suggestion, for its review card and the organize preview.
func (currentModel model) suggestionBody(suggestion store.Suggestion, width int) []string {
	theme := currentModel.theme
	var lines []string
	sessionTitle := func(sessionId string) string {
		if session := currentModel.repository.FindSession(sessionId); session != nil {
			return session.Title
		}
		return sessionId
	}
	switch suggestion.Kind {
	case store.SuggestMove:
		if len(suggestion.SessionIds) == 1 {
			lines = append(lines, theme.bold.Render(sessionTitle(suggestion.SessionIds[0])))
		} else {
			for _, sessionId := range suggestion.SessionIds {
				lines = append(lines, theme.text.Render("· "+truncate(sessionTitle(sessionId), width-2)))
			}
		}
		from := "Inbox"
		if session := currentModel.repository.FindSession(suggestion.SessionIds[0]); session != nil {
			from = currentModel.repository.FolderName(session.FolderId)
		}
		lines = append(lines, theme.dim.Render(from)+theme.dim.Render("  →  ")+theme.accentBold.Render(currentModel.repository.FolderName(suggestion.FolderId)))
	case store.SuggestSubfolder:
		lines = append(lines, theme.text.Render("New ")+theme.accentBold.Render("⎇ "+suggestion.FolderName)+theme.text.Render(" in "+currentModel.repository.FolderName(suggestion.ParentFolderId)))
		lines = append(lines, theme.dim.Render("moves "+countedSessions(len(suggestion.SessionIds))+":"))
		for _, sessionId := range suggestion.SessionIds {
			lines = append(lines, theme.text.Render("  · "+truncate(sessionTitle(sessionId), width-4)))
		}
	case store.SuggestCleanup:
		lines = append(lines, theme.bold.Render(countedSessions(len(suggestion.SessionIds))+" look finished"))
		isCard := currentModel.currentScreen() == reviewScreen
		for index, sessionId := range suggestion.SessionIds {
			session := currentModel.repository.FindSession(sessionId)
			if session == nil {
				continue
			}
			checkbox := segment{text: "[x] ", style: theme.accent}
			if currentModel.review.cleanupKept[sessionId] && isCard {
				checkbox = segment{text: "[ ] ", style: theme.faint}
			}
			right := []segment{{text: relativeAge(session.LastActivityAt), style: theme.dim}}
			if currentModel.mergedBranches[session.Branch] {
				right = append(right, segment{text: " merged", style: theme.running})
			}
			lines = append(lines, currentModel.layoutRow([]segment{checkbox, {text: session.Title, style: theme.text}}, right, width, isCard && index == currentModel.review.cleanupCursor))
		}
	case store.SuggestContext:
		if suggestion.Source != "" {
			lines = append(lines, theme.dim.Render("learned in "+suggestion.Source))
		}
		lines = append(lines, currentModel.diffLines(currentModel.currentContextText(suggestion), suggestion.ProposedText, width)...)
	}
	if suggestion.Reason != "" {
		lines = append(lines, "")
		lines = append(lines, wrapAll([]string{theme.dim.Render(suggestion.Reason)}, width)...)
	}
	if suggestion.Kind == store.SuggestCleanup {
		lines = append(lines, theme.faint.Render("Archived isn't deleted: ↵ still resumes them."))
	}
	return lines
}

// diffLines shows a context change as a unified diff, green and red on faint backgrounds.
func (currentModel model) diffLines(before string, after string, width int) []string {
	theme := currentModel.theme
	var lines []string
	for _, line := range diffHunks(lineDiff(before, after), 2) {
		style, marker := theme.diffContext, "  "
		switch line.kind {
		case addedLine:
			style, marker = theme.diffAdded, "+ "
		case removedLine:
			style, marker = theme.diffRemoved, "- "
		case skippedLines:
			lines = append(lines, theme.faint.Render("  …"))
			continue
		}
		for pieceIndex, piece := range wrapText(line.text, width-2) {
			if pieceIndex > 0 {
				marker = "  "
			}
			if line.kind == unchangedLine {
				lines = append(lines, style.Render(marker+piece))
			} else {
				lines = append(lines, style.Render(padRight(marker+piece, width)))
			}
		}
	}
	return lines
}

// screenTopBar is the first line of a full-window screen: where you are, the agents, and the way back.
func (currentModel model) screenTopBar(name string, width int) string {
	theme := currentModel.theme
	left := []segment{{text: " den", style: theme.accentBold}, {text: " · " + currentModel.repositoryName() + " · " + name, style: theme.dim}}
	var right []segment
	for _, open := range currentModel.openAgents {
		right = append(right, segment{text: fmt.Sprint(open.number), style: theme.accentBold}, currentModel.statusGlyph(open.status), segment{text: "  ", style: theme.text})
	}
	right = append(right, segment{text: "esc", style: theme.key}, segment{text: " back ", style: theme.action})
	return currentModel.layoutRow(left, right, width, false)
}

func centered(block string, width int) []string {
	return strings.Split(lipgloss.PlaceHorizontal(width, lipgloss.Center, block), "\n")
}
