package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	// reviewEvery is how many new prompts make a session worth another look while its agent is open.
	reviewEvery = 3
	// baselineAfter is how old a session's last activity is for den to start counting from now instead of reviewing it.
	baselineAfter = time.Hour
	// maximumTitleLength drops titles that ignored the naming rules.
	maximumTitleLength = 40
	// maximumHelperFailures turns the model off for the rest of the run, so a broken login doesn't fail every 20 seconds.
	maximumHelperFailures = 3
)

type jobKind int

const (
	reviewJob jobKind = iota
	chatJob
	// reviseJob answers what the user told den about one of its suggestions in the dock.
	reviseJob
)

func (kind jobKind) String() string {
	return [...]string{"review", "chat", "revise"}[kind]
}

// job is one model call. Its input is built when it starts, so it sees the latest state.
type job struct {
	kind         jobKind
	sessionId    string
	suggestionId string
}

type jobDoneMessage struct {
	job job
	// promptCount is how many prompts a review covered.
	promptCount int
	answer      any
	chatInput   brain.ChatInput
	usage       helper.Usage
	err         error
}

// queueJob adds a job unless the same one is waiting or running. Chats go first, since someone is waiting for them.
func (currentModel *model) queueJob(next job) {
	if currentModel.helperCaller == nil {
		return
	}
	if currentModel.runningJob != nil && *currentModel.runningJob == next {
		return
	}
	for _, queued := range currentModel.jobQueue {
		if queued == next {
			return
		}
	}
	if next.kind == chatJob || next.kind == reviseJob {
		currentModel.jobQueue = append([]job{next}, currentModel.jobQueue...)
		return
	}
	currentModel.jobQueue = append(currentModel.jobQueue, next)
}

// startNextJob runs the next waiting job in the background; one at a time.
func (currentModel *model) startNextJob() tea.Cmd {
	for currentModel.runningJob == nil && len(currentModel.jobQueue) > 0 && currentModel.helperCaller != nil {
		next := currentModel.jobQueue[0]
		currentModel.jobQueue = currentModel.jobQueue[1:]
		if command := currentModel.jobCommand(next); command != nil {
			currentModel.runningJob = &next
			return command
		}
	}
	return nil
}

func (currentModel *model) jobCommand(next job) tea.Cmd {
	caller := *currentModel.helperCaller
	run := func(request helper.Request, answer any, done jobDoneMessage) tea.Cmd {
		return func() tea.Msg {
			done.answer = answer
			done.usage, done.err = caller.Call(request, answer)
			return done
		}
	}
	repository := &currentModel.repository
	switch next.kind {
	case reviewJob:
		session := repository.FindSession(next.sessionId)
		if session == nil {
			return nil
		}
		adapter, known := agent.ByName(session.Tool)
		if !known {
			return nil
		}
		turns, err := adapter.Turns(session.SessionId)
		if err != nil || len(turns) <= session.ReviewedAtPrompt {
			return nil
		}
		request := brain.ReviewRequest(brain.ReviewInput{
			Session:         *session,
			Digest:          currentModel.digests[session.SessionId],
			Turns:           turns[session.ReviewedAtPrompt:],
			FirstTurn:       session.ReviewedAtPrompt,
			Repository:      repository,
			FolderNotes:     currentModel.folderNotesText(session.FolderId),
			RepositoryNotes: repository.ReadContext(""),
			LaptopRules:     currentModel.laptopRulesText(),
		})
		return run(request, &brain.ReviewAnswer{}, jobDoneMessage{job: next, promptCount: len(turns)})
	case chatJob:
		input := currentModel.chatInput()
		return run(brain.ChatRequest(input), &brain.ChatAnswer{}, jobDoneMessage{job: next, chatInput: input})
	case reviseJob:
		input, exists := currentModel.reviseInput()
		if !exists {
			currentModel.dock.thinking = false
			return nil
		}
		return run(brain.ChatRequest(input), &brain.ChatAnswer{}, jobDoneMessage{job: next, chatInput: input})
	}
	return nil
}

// finishJob applies what the model answered, then starts the next job.
func (currentModel *model) finishJob(done jobDoneMessage) tea.Cmd {
	currentModel.runningJob = nil
	if done.err != nil {
		currentModel.helperFailures++
		switch done.job.kind {
		case chatJob:
			currentModel.chat.thinking = false
			currentModel.chat.addMessage(brain.ChatMessage{FromDen: true, Text: "I couldn't think that through: " + done.err.Error()})
		case reviseJob:
			currentModel.dock.thinking = false
			currentModel.dock.reply = &brain.ChatMessage{FromDen: true, Text: "I couldn't think that through: " + done.err.Error()}
		}
		if done.job.kind == reviewJob {
			if session := currentModel.repository.FindSession(done.job.sessionId); session != nil {
				session.ReviewedAtPrompt = done.promptCount
			}
		}
		if currentModel.helperFailures >= maximumHelperFailures {
			currentModel.setError("den's model calls keep failing (" + done.err.Error() + "), so den uses its local rules only")
			currentModel.helperCaller, currentModel.jobQueue = nil, nil
		}
		return currentModel.startNextJob()
	}
	currentModel.helperFailures = 0
	currentModel.helperCalls++
	currentModel.helperUsage.InputTokens += done.usage.InputTokens
	currentModel.helperUsage.OutputTokens += done.usage.OutputTokens
	currentModel.helperUsage.CostDollars += done.usage.CostDollars
	switch answer := done.answer.(type) {
	case *brain.ReviewAnswer:
		currentModel.keepingSelection(func() { currentModel.applyReview(done.job.sessionId, done.promptCount, *answer) })
	case *brain.ChatAnswer:
		if done.job.kind == reviseJob {
			currentModel.applyReviseAnswer(done.job, *answer, done.chatInput)
		} else {
			currentModel.applyChatAnswer(*answer, done.chatInput)
		}
	}
	return tea.Batch(currentModel.animateRetitles(), currentModel.startNextJob())
}

// isRewording tells whether a new title mostly repeats the words of the current one, which isn't worth a rename.
func isRewording(currentTitle string, newTitle string) bool {
	currentWords := strings.Fields(strings.ToLower(currentTitle))
	newWords := strings.Fields(strings.ToLower(newTitle))
	shared := 0
	for _, word := range newWords {
		if slices.Contains(currentWords, word) {
			shared++
		}
	}
	return shared*2 >= max(len(currentWords), len(newWords))
}

// queueReviewIfDue reviews a session after its first prompt, then every few prompts while its agent is open,
// and once more when it closes with prompts not looked at. A running agent is left alone until it stops.
// It reports whether it changed the session.
func (currentModel *model) queueReviewIfDue(session *store.Session, digest agent.Digest) bool {
	unreviewed := digest.PromptCount - session.ReviewedAtPrompt
	if session.Archived || unreviewed <= 0 {
		return false
	}
	open := currentModel.openAgentFor(session.SessionId)
	if session.ReviewedAtPrompt == 0 && open == nil && time.Since(session.LastActivityAt) > baselineAfter {
		session.ReviewedAtPrompt = digest.PromptCount
		return true
	}
	if open != nil && open.status == "running" {
		return false
	}
	if session.ReviewedAtPrompt == 0 || unreviewed >= reviewEvery || open == nil {
		currentModel.queueJob(job{kind: reviewJob, sessionId: session.SessionId})
	}
	return false
}

// applyReview carries out a review: a new name right away, and what's worth keeping as a suggestion. Everything is logged.
func (currentModel *model) applyReview(sessionId string, promptCount int, answer brain.ReviewAnswer) {
	repository := &currentModel.repository
	session := repository.FindSession(sessionId)
	if session == nil {
		return
	}
	session.ReviewedAtPrompt, session.TitledAtPrompt = promptCount, promptCount
	if summary := strings.TrimSpace(answer.Summary); summary != "" {
		session.Summary = summary
	}
	logDecision := func(action string, detail string, outcome string) {
		currentModel.reportError(repository.LogDecision(store.Decision{Job: "review", SessionId: sessionId, FolderId: session.FolderId, Action: action, Detail: detail, Outcome: outcome}))
	}
	title := strings.Trim(strings.TrimSpace(answer.Title), "\"'.")
	if title != "" && !strings.EqualFold(title, session.Title) {
		switch {
		case session.TitlePinned:
			logDecision("retitle", title, "dropped: pinned")
		case len([]rune(title)) > maximumTitleLength:
			logDecision("retitle", title, "dropped: too long")
		case session.TitledAtPrompt > 0 && isRewording(session.Title, title):
			logDecision("retitle", session.Title+" → "+title, "dropped: same name, reworded")
		default:
			logDecision("retitle", session.Title+" → "+title, "applied")
			currentModel.retitle(session, title)
		}
	}
	if answer.Keep.Kind != "" && answer.Keep.Kind != "none" {
		suggestion, outcome := brain.KeepSuggestion(answer.Keep, *session, currentModel.digests[sessionId], repository)
		if outcome == "proposed" && !repository.Offer(suggestion) {
			outcome = "withheld: already rejected, or enough waiting"
		}
		logDecision("keep "+answer.Keep.Kind, fmt.Sprintf("%s (%.2f)", answer.Keep.Reason, answer.Keep.Confidence), outcome)
	}
	if strings.TrimSpace(answer.Rule.Rule) != "" {
		suggestion, outcome := brain.RuleSuggestion(answer.Rule, *session, currentModel.laptopTexts())
		if outcome == "proposed" && !repository.Offer(suggestion) {
			outcome = "withheld: already rejected, or enough waiting"
		}
		logDecision("rule", fmt.Sprintf("%s (%.2f)", answer.Rule.Rule, answer.Rule.Confidence), outcome)
	}
	currentModel.saveRepository()
}

// laptopTexts are the laptop files a rule can go to, by path: the ones that exist, and those of installed tools.
func (currentModel model) laptopTexts() map[string]string {
	texts := map[string]string{}
	for _, file := range currentModel.shownLaptopFiles() {
		texts[file.Path] = file.Text
	}
	return texts
}

// laptopRulesText is everything the laptop files say, so den doesn't propose what they have.
func (currentModel model) laptopRulesText() string {
	var texts []string
	for _, text := range currentModel.laptopTexts() {
		if strings.TrimSpace(text) != "" {
			texts = append(texts, strings.TrimSpace(text))
		}
	}
	return strings.Join(texts, "\n\n")
}

// folderNotesText is everything a folder's context says: its linked file, then den's notes.
func (currentModel model) folderNotesText(folderId string) string {
	var texts []string
	for _, filePath := range currentModel.repository.ContextFilePaths(folderId) {
		if text := strings.TrimSpace(store.ReadText(filePath)); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n\n")
}
