package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	reviewEvery = 5
	// reviewTurns is how many of the latest turns a review reads, the new ones marked, so it sees where they came from.
	reviewTurns = 10
	// quietAfter is how long a session sits idle before den reviews what it hasn't seen, all of it once there's real work.
	quietAfter = 10 * time.Minute
	// minimumQuietPrompts keeps the review of a whole session to sessions with real work in them.
	minimumQuietPrompts = 3
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
	// contextJob answers what the user asked to change in a folder's context pane.
	contextJob
	// lessonJob learns from a card the user said no to, for den's lessons file.
	lessonJob
	// compactLessonsJob shortens den's lessons file once it grew too long.
	compactLessonsJob
)

func (kind jobKind) String() string {
	return [...]string{"review", "chat", "revise", "context", "lesson", "compact lessons"}[kind]
}

// job is one model call. Its input is built when it starts, so it sees the latest state.
type job struct {
	kind         jobKind
	sessionId    string
	suggestionId string
	// wholeSession makes a review the card review of a session that went quiet, which reads all of it,
	// instead of the progress review of its latest turns.
	wholeSession bool
	// rejectedCard is the card a lesson job learns from, spelled out with anything said about it.
	rejectedCard string
}

type jobDoneMessage struct {
	job job
	// promptCount is how many prompts a review covered.
	promptCount int
	answer      any
	chatInput   brain.ChatInput
	request     helper.Request
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
	if next.kind == chatJob || next.kind == reviseJob || next.kind == contextJob {
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
			done.answer, done.request = answer, request
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
		if err != nil || len(turns) == 0 || (!next.wholeSession && len(turns) <= session.ReviewedAtPrompt) {
			return nil
		}
		firstTurn := max(len(turns)-reviewTurns, 0)
		input := brain.ReviewInput{
			Session:         *session,
			Digest:          currentModel.digests[session.SessionId],
			Turns:           turns[firstTurn:],
			FirstTurn:       firstTurn,
			NewFromTurn:     session.ReviewedAtPrompt,
			Repository:      repository,
			FolderNotes:     currentModel.folderNotesText(session.FolderId),
			RepositoryNotes: repository.ReadContext(""),
			LaptopRules:     currentModel.laptopRulesText(),
			Lessons:         store.ReadLessons(),
		}
		if next.wholeSession {
			for _, turn := range turns {
				input.AllPrompts = append(input.AllPrompts, turn.User)
			}
		}
		request := brain.ReviewRequest(input)
		return run(request, &brain.ReviewAnswer{}, jobDoneMessage{job: next, promptCount: len(turns)})
	case contextJob:
		input := currentModel.contextPaneInput()
		input.Lessons = store.ReadLessons()
		return run(brain.ChatRequest(input), &brain.ChatAnswer{}, jobDoneMessage{job: next, chatInput: input})
	case chatJob:
		input := currentModel.chatInput()
		input.Lessons = store.ReadLessons()
		return run(brain.ChatRequest(input), &brain.ChatAnswer{}, jobDoneMessage{job: next, chatInput: input})
	case lessonJob:
		return run(brain.LessonRequest(next.rejectedCard, store.ReadLessons()), &brain.LessonAnswer{}, jobDoneMessage{job: next})
	case compactLessonsJob:
		return run(brain.CompactLessonsRequest(store.ReadLessons()), &brain.CompactLessonsAnswer{}, jobDoneMessage{job: next})
	case reviseJob:
		input, exists := currentModel.reviseInput()
		if !exists {
			currentModel.dock.thinking = false
			return nil
		}
		input.Lessons = store.ReadLessons()
		return run(brain.ChatRequest(input), &brain.ChatAnswer{}, jobDoneMessage{job: next, chatInput: input})
	}
	return nil
}

// finishJob applies what the model answered, then starts the next job.
func (currentModel *model) finishJob(done jobDoneMessage) tea.Cmd {
	currentModel.runningJob = nil
	currentModel.logCall(done)
	if done.err != nil {
		currentModel.helperFailures++
		switch done.job.kind {
		case chatJob:
			currentModel.chat.thinking = false
			currentModel.chat.addMessage(brain.ChatMessage{FromDen: true, Text: "I couldn't think that through: " + done.err.Error()})
		case reviseJob:
			currentModel.dock.thinking = false
			currentModel.dock.reply = &brain.ChatMessage{FromDen: true, Text: "I couldn't think that through: " + done.err.Error()}
		case contextJob:
			currentModel.contextPane.thinking = false
			currentModel.contextPane.reply = &brain.ChatMessage{FromDen: true, Text: "I couldn't think that through: " + done.err.Error()}
		}
		if done.job.kind == reviewJob {
			if session := currentModel.repository.FindSession(done.job.sessionId); session != nil {
				if done.job.wholeSession {
					session.QuietReviewedAtPrompt = done.promptCount
				} else {
					session.ReviewedAtPrompt = done.promptCount
				}
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
	case *brain.LessonAnswer:
		currentModel.applyLesson(answer.Lesson)
	case *brain.CompactLessonsAnswer:
		currentModel.applyCompactedLessons(answer.Lessons)
	case *brain.ReviewAnswer:
		currentModel.keepingSelection(func() { currentModel.applyReview(done.job, done.promptCount, *answer) })
	case *brain.ChatAnswer:
		if done.job.kind == reviseJob {
			currentModel.applyReviseAnswer(done.job, *answer, done.chatInput)
		} else if done.job.kind == contextJob {
			currentModel.applyContextPaneAnswer(*answer, done.chatInput)
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

// queueReviewIfDue keeps a session's name and progress current with the progress review: after its first prompt, even while
// its agent runs, so it gets a name right away, then every few prompts while its agent waits, and whenever it goes quiet,
// idle for a while, so den always knows where it stands. Once a quiet session has real work, the card review reads all of it
// for what's worth keeping; the progress review goes first, so the card review sees where it stands.
// Sessions last active before den started watching the repo start counting from now. It reports whether it changed the session.
func (currentModel *model) queueReviewIfDue(session *store.Session, digest agent.Digest) bool {
	if session.Archived || digest.PromptCount == 0 {
		return false
	}
	open := currentModel.openAgentFor(session.SessionId)
	if session.ReviewedAtPrompt == 0 && open == nil && session.LastActivityAt.Before(currentModel.repository.TrackedSince) {
		session.ReviewedAtPrompt, session.QuietReviewedAtPrompt = digest.PromptCount, digest.PromptCount
		return true
	}
	if session.ReviewedAtPrompt > 0 && open != nil && open.status == "running" {
		return false
	}
	// Agents started outside den have no status den can read, so going quiet is going idle for a while, for every session.
	isQuiet := time.Since(session.LastActivityAt) > quietAfter
	unreviewed := digest.PromptCount - session.ReviewedAtPrompt
	if unreviewed > 0 && (session.ReviewedAtPrompt == 0 || unreviewed >= reviewEvery || isQuiet) {
		currentModel.queueJob(job{kind: reviewJob, sessionId: session.SessionId})
	}
	if isQuiet && digest.PromptCount >= minimumQuietPrompts && session.QuietReviewedAtPrompt < digest.PromptCount {
		currentModel.queueJob(job{kind: reviewJob, sessionId: session.SessionId, wholeSession: true})
	}
	return false
}

// applyReview carries out a review: a progress review renames the session and updates where it stands,
// a card review offers what's worth keeping as a suggestion. Everything is logged.
func (currentModel *model) applyReview(done job, promptCount int, answer brain.ReviewAnswer) {
	repository := &currentModel.repository
	session := repository.FindSession(done.sessionId)
	if session == nil {
		return
	}
	if done.wholeSession {
		session.QuietReviewedAtPrompt = promptCount
		currentModel.offerCards(session, answer)
	} else {
		currentModel.applyProgress(session, promptCount, answer)
	}
	currentModel.saveRepository()
}

// applyProgress takes a progress review's title, unless the user pinned one or it only rewords the current one, and its progress.
func (currentModel *model) applyProgress(session *store.Session, promptCount int, answer brain.ReviewAnswer) {
	wasTitled := session.TitledAtPrompt > 0
	session.ReviewedAtPrompt, session.TitledAtPrompt = promptCount, promptCount
	if goal := strings.TrimSpace(answer.Progress.Goal); goal != "" {
		session.Progress = store.Progress{Goal: goal, Done: strings.TrimSpace(answer.Progress.Done), Next: strings.TrimSpace(answer.Progress.Next)}
	}
	title := strings.Trim(strings.TrimSpace(answer.Title), "\"'.")
	if title == "" || strings.EqualFold(title, session.Title) {
		return
	}
	switch {
	case session.TitlePinned:
		currentModel.logReviewDecision(session, "retitle", title, "dropped: pinned")
	case len([]rune(title)) > maximumTitleLength:
		currentModel.logReviewDecision(session, "retitle", title, "dropped: too long")
	case wasTitled && isRewording(session.Title, title):
		currentModel.logReviewDecision(session, "retitle", session.Title+" → "+title, "dropped: same name, reworded")
	default:
		currentModel.logReviewDecision(session, "retitle", session.Title+" → "+title, "applied")
		currentModel.retitle(session, title)
	}
}

// offerCards offers a card review's keep and rule as suggestions, unless the user already said no to them or enough are waiting.
func (currentModel *model) offerCards(session *store.Session, answer brain.ReviewAnswer) {
	repository := &currentModel.repository
	keepKind := answer.Keep.Kind
	if keepKind == "" {
		keepKind = "none"
	}
	suggestion, outcome := brain.KeepSuggestion(answer.Keep, *session, currentModel.digests[session.SessionId], repository)
	if outcome == "proposed" && !repository.Offer(suggestion) {
		outcome = "withheld: already rejected, or enough waiting"
	}
	currentModel.logReviewDecision(session, "keep "+keepKind, fmt.Sprintf("%s (%.2f)", answer.Keep.Reason, answer.Keep.Confidence), outcome)
	if strings.TrimSpace(answer.Rule.Rule) != "" {
		suggestion, outcome := brain.RuleSuggestion(answer.Rule, *session, currentModel.laptopTexts())
		if outcome == "proposed" && !repository.Offer(suggestion) {
			outcome = "withheld: already rejected, or enough waiting"
		}
		currentModel.logReviewDecision(session, "rule", fmt.Sprintf("%s (%.2f)", answer.Rule.Rule, answer.Rule.Confidence), outcome)
	}
}

func (currentModel *model) logReviewDecision(session *store.Session, action string, detail string, outcome string) {
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "review", SessionId: session.SessionId, FolderId: session.FolderId, Action: action, Detail: detail, Outcome: outcome}))
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

// folderNotesText is what a folder's context says.
func (currentModel model) folderNotesText(folderId string) string {
	return strings.TrimSpace(currentModel.repository.ReadContext(folderId))
}

// logCall keeps every model call whole in calls.jsonl, its prompt, answer, cost and time, for evaluating den later.
func (currentModel *model) logCall(done jobDoneMessage) {
	if done.request.Prompt == "" {
		return
	}
	call := store.Call{
		Job:           done.job.kind.String(),
		SessionId:     done.job.sessionId,
		Model:         done.usage.Model,
		PromptVersion: promptVersion(done.request.System),
		Prompt:        done.request.Prompt,
		InputTokens:   done.usage.InputTokens,
		OutputTokens:  done.usage.OutputTokens,
		CostDollars:   done.usage.CostDollars,
		Milliseconds:  done.usage.Duration.Milliseconds(),
	}
	if done.job.wholeSession {
		call.Job = "review whole session"
	}
	if done.err != nil {
		call.Error = done.err.Error()
	} else if answer, err := json.Marshal(done.answer); err == nil {
		call.Answer = answer
	}
	currentModel.reportError(currentModel.repository.LogCall(call))
}

// promptVersion is a short hash of a system prompt, which changes whenever the prompt does.
func promptVersion(system string) string {
	hash := sha256.Sum256([]byte(system))
	return hex.EncodeToString(hash[:4])
}

// applyLesson adds what den learned from a rejected card to its lessons, and compacts them once they grew too long.
func (currentModel *model) applyLesson(lesson string) {
	lessons := store.ReadLessons()
	updated := brain.AddLesson(lessons, lesson)
	outcome := "nothing new"
	if updated != lessons {
		outcome = "learned"
		if currentModel.reportError(store.WriteLessons(updated)) {
			return
		}
		if len(updated) > brain.MaximumLessonsLength {
			currentModel.queueJob(job{kind: compactLessonsJob})
		}
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "lesson", Action: "lesson", Detail: lesson, Outcome: outcome}))
}

// applyCompactedLessons replaces the lessons with their shorter version, unless the model failed to shorten them.
func (currentModel *model) applyCompactedLessons(compacted string) {
	lessons := store.ReadLessons()
	compacted = strings.TrimSpace(compacted) + "\n"
	outcome := "applied"
	if strings.TrimSpace(compacted) == "" || len(compacted) >= len(lessons) {
		outcome = "dropped: not shorter"
	} else if currentModel.reportError(store.WriteLessons(compacted)) {
		return
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "lesson", Action: "compact",
		Detail: fmt.Sprintf("%d → %d characters", len(lessons), len(compacted)), Outcome: outcome}))
}
