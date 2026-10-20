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
	// quietAfter is how long a session sits idle before den reviews what it hasn't seen: its progress, and the replies no gate check read.
	quietAfter = 10 * time.Minute
	// replySettledAfter is how long the transcript of an agent den can't read the status of stays unchanged before its reply counts as done.
	replySettledAfter = time.Minute
	// repliesBetweenCardReviews keeps a session's card reviews apart, unless the local check found a plan or a markdown file.
	repliesBetweenCardReviews = 2
	// maximumCardReviewsPerHour caps what the card review costs in a busy hour, for the whole repo.
	maximumCardReviewsPerHour = 10
	// minimumCardReviewTurns is how many turns a card review reads at least, so a single new one has what led to it.
	minimumCardReviewTurns = 2
	// maximumTitleLength drops titles that ignored the naming rules.
	maximumTitleLength = 40
	// maximumHelperFailures turns the model off for the rest of the run, so a broken login doesn't fail every 20 seconds.
	maximumHelperFailures = 3
)

type jobKind int

const (
	// progressReviewJob keeps a session's title and progress current.
	progressReviewJob jobKind = iota
	// gateJob decides whether the replies since the last check are worth a card review.
	gateJob
	// cardReviewJob decides what of a session is worth a card.
	cardReviewJob
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
	return [...]string{"progress review", "gate check", "card review", "chat", "revise", "context", "lesson", "compact lessons"}[kind]
}

// job is one model call. Its input is built when it starts, so it sees the latest state.
type job struct {
	kind         jobKind
	sessionId    string
	suggestionId string
	// rejectedCard is the card a lesson job learns from, spelled out with anything said about it.
	rejectedCard string
}

type jobDoneMessage struct {
	job job
	// promptCount is how many prompts a review covered.
	promptCount int
	// cardLabels are the waiting cards a card review saw, by the label it withdraws them with, to their suggestion ids.
	cardLabels map[string]string
	answer     any
	chatInput  brain.ChatInput
	request    helper.Request
	usage      helper.Usage
	err        error
}

// queueJob adds a job unless the same one is waiting or running. Chats go first, since someone is waiting for them.
// It reports whether it added the job.
func (currentModel *model) queueJob(next job) bool {
	if currentModel.helperCaller == nil {
		return false
	}
	if currentModel.runningJob != nil && *currentModel.runningJob == next {
		return false
	}
	for _, queued := range currentModel.jobQueue {
		if queued == next {
			return false
		}
	}
	if next.kind == chatJob || next.kind == reviseJob || next.kind == contextJob {
		currentModel.jobQueue = append([]job{next}, currentModel.jobQueue...)
		return true
	}
	currentModel.jobQueue = append(currentModel.jobQueue, next)
	return true
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
	case progressReviewJob, gateJob, cardReviewJob:
		session := repository.FindSession(next.sessionId)
		if session == nil {
			return nil
		}
		adapter, known := agent.ByName(session.Tool)
		if !known {
			return nil
		}
		turns, err := adapter.Turns(session.SessionId)
		if err != nil || len(turns) == 0 {
			return nil
		}
		switch next.kind {
		case gateJob:
			return currentModel.gateCommand(next, *session, turns, run)
		case cardReviewJob:
			return currentModel.cardReviewCommand(next, *session, turns, run)
		}
		if len(turns) <= session.ReviewedAtPrompt {
			return nil
		}
		input := currentModel.reviewInput(*session, turns, max(len(turns)-reviewTurns, 0), session.ReviewedAtPrompt)
		return run(brain.ReviewRequest(input), &brain.ReviewAnswer{}, jobDoneMessage{job: next, promptCount: len(turns)})
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
	callId := currentModel.logCall(done)
	done.chatInput.CallId = callId
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
		if session := currentModel.repository.FindSession(done.job.sessionId); session != nil {
			switch done.job.kind {
			case progressReviewJob:
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
	case *brain.LessonAnswer:
		currentModel.applyLesson(answer.Lesson)
	case *brain.CompactLessonsAnswer:
		currentModel.applyCompactedLessons(answer.Lessons)
	case *brain.ReviewAnswer:
		currentModel.keepingSelection(func() { currentModel.applyReview(done, *answer, callId) })
	case *brain.GateAnswer:
		currentModel.applyGate(done, *answer, callId)
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

// queueReviewsIfDue keeps a session's name and progress current with the progress review: after its first prompt, even while
// its agent runs, so it gets a name right away, then every few prompts while its agent waits, and whenever it goes quiet,
// idle for a while, so den always knows where it stands. Every finished reply gets den's local check, which makes the card
// review due, leaves the reply for later, or asks the gate check; replies no gate check read get one once the session goes quiet.
// Sessions last active before den started watching the repo start counting from now. It reports whether it changed the session.
func (currentModel *model) queueReviewsIfDue(session *store.Session, digest agent.Digest) bool {
	if session.Archived || digest.PromptCount == 0 {
		return false
	}
	open := currentModel.openAgentFor(session.SessionId)
	if session.ReviewedAtPrompt == 0 && open == nil && session.LastActivityAt.Before(currentModel.repository.TrackedSince) {
		session.ReviewedAtPrompt = digest.PromptCount
		checkedUpTo(session, digest)
		return true
	}
	// Agents started outside den have no status den can read, so going quiet is going idle for a while, for every session.
	isQuiet := time.Since(session.LastActivityAt) > quietAfter
	running := open != nil && open.status == "running"
	if session.ReviewedAtPrompt > 0 && running {
		return false
	}
	unreviewed := digest.PromptCount - session.ReviewedAtPrompt
	if unreviewed > 0 && (session.ReviewedAtPrompt == 0 || unreviewed >= reviewEvery || isQuiet) {
		currentModel.queueJob(job{kind: progressReviewJob, sessionId: session.SessionId})
	}
	if running {
		return false
	}
	changed := false
	// An agent den opened says when its turn ended; waiting on a permission is still mid-turn. Others count as done once they settle.
	replyFinished := time.Since(digest.LastActivityAt) > replySettledAfter
	if open != nil && open.status != "" {
		replyFinished = open.status == agent.StateDone || (open.status == agent.StateWaiting && strings.HasPrefix(open.reason, agent.QuestionReasonPrefix))
	}
	if replyFinished && digest.PromptCount > session.CheckedAtPrompt {
		currentModel.checkReply(session, digest)
		changed = true
	}
	if !session.CardReviewDue && isQuiet && session.GatedAtPrompt < session.CheckedAtPrompt && currentModel.gateAllowed(*session) {
		currentModel.queueJob(job{kind: gateJob, sessionId: session.SessionId})
	}
	currentModel.queueCardReviewIfDue(*session)
	return changed
}

// checkedUpTo marks every reply of a session as checked and read, so den's checks start from its next reply.
func checkedUpTo(session *store.Session, digest agent.Digest) {
	session.CheckedAtPrompt, session.GatedAtPrompt, session.CardReviewedAtPrompt = digest.PromptCount, digest.PromptCount, digest.PromptCount
	session.CheckedFileCount, session.CheckedPlan = len(digest.TouchedFiles), planHash(digest.Plan)
}

// checkReply runs den's local check on a session's latest reply: a plan or a markdown file makes the card review due,
// a go-ahead or a question back waits for the next check, and everything else goes to the gate check.
func (currentModel *model) checkReply(session *store.Session, digest agent.Digest) {
	editedFiles := digest.TouchedFiles[min(session.CheckedFileCount, len(digest.TouchedFiles)):]
	verdict, rule := brain.CheckReply(brain.ReplyCheck{
		Prompt:          digest.LastPrompt,
		Reply:           digest.LastAgentMessage,
		PlanApproved:    digest.Plan != "" && planHash(digest.Plan) != session.CheckedPlan,
		MarkdownWritten: brain.WroteMarkdown(digest.WorkingDirectory, editedFiles),
	})
	session.CheckedAtPrompt, session.CheckedFileCount, session.CheckedPlan = digest.PromptCount, len(digest.TouchedFiles), planHash(digest.Plan)
	outcome := "skip"
	switch verdict {
	case brain.ReviewNow:
		session.CardReviewDue, outcome = true, "card review"
	case brain.AskGate:
		outcome = "gate check"
		if !currentModel.gateAllowed(*session) {
			outcome = "waits: card review limit"
		} else {
			currentModel.queueJob(job{kind: gateJob, sessionId: session.SessionId})
		}
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "check", SessionId: session.SessionId, FolderId: session.FolderId,
		Action: "reply", Detail: fmt.Sprintf("prompt %d · %s", digest.PromptCount, rule), Outcome: outcome}))
}

// planHash is a short hash of an approved plan, or "" for none, so a check sees when a new one was approved.
func planHash(plan string) string {
	if plan == "" {
		return ""
	}
	return promptVersion(plan)
}

// gateAllowed tells whether a gate check may run for a session: only when a card review it starts could run. A session
// with a waiting card review card skips the spacing, since its next reply may undo what the card keeps.
func (currentModel *model) gateAllowed(session store.Session) bool {
	spaced := session.CardReviewedAtPrompt == 0 || session.CheckedAtPrompt-session.CardReviewedAtPrompt >= repliesBetweenCardReviews || time.Since(session.LastActivityAt) > quietAfter
	hasWaitingCard := slices.ContainsFunc(currentModel.repository.WaitingFor(session.SessionId), func(waiting store.Suggestion) bool { return waiting.KeepKind != "" })
	return (spaced || hasWaitingCard) && currentModel.cardReviewsInLastHour() < maximumCardReviewsPerHour
}

func (currentModel *model) cardReviewsInLastHour() int {
	hourAgo := time.Now().Add(-time.Hour)
	currentModel.cardReviewTimes = slices.DeleteFunc(currentModel.cardReviewTimes, func(started time.Time) bool { return started.Before(hourAgo) })
	return len(currentModel.cardReviewTimes)
}

// queueCardReviewIfDue starts the card review of a session a check flagged, unless the hour's card reviews are used up;
// then it stays due until one is free.
func (currentModel *model) queueCardReviewIfDue(session store.Session) {
	if !session.CardReviewDue || currentModel.cardReviewsInLastHour() >= maximumCardReviewsPerHour {
		return
	}
	if currentModel.queueJob(job{kind: cardReviewJob, sessionId: session.SessionId}) {
		currentModel.cardReviewTimes = append(currentModel.cardReviewTimes, time.Now())
	}
}

// reviewInput is what both reviews read about a session: the turns from firstTurn on, the ones from newFromTurn marked new.
func (currentModel *model) reviewInput(session store.Session, turns []agent.Turn, firstTurn int, newFromTurn int) brain.ReviewInput {
	repository := &currentModel.repository
	return brain.ReviewInput{
		Session:         session,
		Digest:          currentModel.digests[session.SessionId],
		Turns:           turns[firstTurn:],
		FirstTurn:       firstTurn,
		NewFromTurn:     newFromTurn,
		Repository:      repository,
		FolderNotes:     currentModel.folderNotesText(session.FolderId),
		RepositoryNotes: repository.ReadContext(""),
		LaptopRules:     currentModel.laptopRulesText(),
		Lessons:         store.ReadLessons(),
	}
}

// gateCommand asks the gate check about the finished replies no gate check read yet.
func (currentModel *model) gateCommand(next job, session store.Session, turns []agent.Turn, run func(helper.Request, any, jobDoneMessage) tea.Cmd) tea.Cmd {
	digest := currentModel.digests[session.SessionId]
	checkedPrompts := min(session.CheckedAtPrompt, len(turns))
	var exchanges []brain.Exchange
	for index := session.GatedAtPrompt; index < checkedPrompts; index++ {
		reply := digest.LastAgentMessage
		if index+1 < len(turns) {
			reply = turns[index+1].Agent
		}
		exchanges = append(exchanges, brain.Exchange{Number: index + 1, Prompt: turns[index].User, Reply: reply})
	}
	if len(exchanges) == 0 {
		return nil
	}
	var waitingCards []string
	for _, waiting := range currentModel.repository.WaitingFor(session.SessionId) {
		waitingCards = append(waitingCards, store.CardSummary(waiting))
	}
	input := brain.GateInput{Session: session, FolderName: currentModel.repository.FolderName(session.FolderId), Exchanges: exchanges, WaitingCards: waitingCards}
	return run(brain.GateRequest(input), &brain.GateAnswer{}, jobDoneMessage{job: next, promptCount: checkedPrompts})
}

// cardReviewCommand runs the card review: the turns since its last look, at least a couple and at most reviewTurns,
// every prompt, and the session's cards, the waiting ones labeled so the answer can withdraw them.
func (currentModel *model) cardReviewCommand(next job, session store.Session, turns []agent.Turn, run func(helper.Request, any, jobDoneMessage) tea.Cmd) tea.Cmd {
	checkedPrompts := min(session.CheckedAtPrompt, len(turns))
	if checkedPrompts == 0 {
		return nil
	}
	firstTurn := max(min(max(session.CardReviewedAtPrompt, checkedPrompts-reviewTurns), checkedPrompts-minimumCardReviewTurns), 0)
	input := currentModel.reviewInput(session, turns[:checkedPrompts], firstTurn, session.CardReviewedAtPrompt)
	if checkedPrompts < len(turns) {
		input.Digest.LastAgentMessage = turns[checkedPrompts].Agent
	}
	input.CardReview, input.PastCards = true, session.Cards
	for _, turn := range turns[:checkedPrompts] {
		input.AllPrompts = append(input.AllPrompts, turn.User)
	}
	cardLabels := map[string]string{}
	for index, waiting := range currentModel.repository.WaitingFor(session.SessionId) {
		label := fmt.Sprintf("c%d", index+1)
		cardLabels[label] = waiting.Id
		kind := waiting.KeepKind
		if kind == "" {
			kind = string(waiting.Kind)
		}
		input.WaitingCards = append(input.WaitingCards, brain.LabeledCard{Label: label, Kind: kind, Summary: store.CardSummary(waiting), Content: waiting.ProposedText})
	}
	return run(brain.ReviewRequest(input), &brain.ReviewAnswer{}, jobDoneMessage{job: next, promptCount: checkedPrompts, cardLabels: cardLabels})
}

// applyGate notes what the gate check read, and starts the card review when it found something.
func (currentModel *model) applyGate(done jobDoneMessage, answer brain.GateAnswer, callId string) {
	session := currentModel.repository.FindSession(done.job.sessionId)
	if session == nil {
		return
	}
	session.GatedAtPrompt = max(session.GatedAtPrompt, done.promptCount)
	outcome := "no"
	if answer.WorthReview {
		session.CardReviewDue, outcome = true, "card review"
	}
	currentModel.logReviewDecision(session, callId, "gate "+answer.Kind, answer.Reason, outcome)
	currentModel.queueCardReviewIfDue(*session)
	currentModel.saveRepository()
}

// applyReview carries out a review: a progress review renames the session and updates where it stands,
// a card review offers what's worth keeping as suggestions and withdraws the waiting ones it moved past. Everything is logged.
func (currentModel *model) applyReview(done jobDoneMessage, answer brain.ReviewAnswer, callId string) {
	session := currentModel.repository.FindSession(done.job.sessionId)
	if session == nil {
		return
	}
	if done.job.kind == cardReviewJob {
		session.CardReviewedAtPrompt = done.promptCount
		session.GatedAtPrompt = max(session.GatedAtPrompt, done.promptCount)
		session.CardReviewDue = session.CheckedAtPrompt > done.promptCount
		currentModel.withdrawCards(session, answer.Withdraw, done.cardLabels, callId)
		currentModel.offerCards(session, answer, callId)
	} else {
		currentModel.applyProgress(session, done.promptCount, answer, callId)
	}
	currentModel.saveRepository()
}

// withdrawCards drops the waiting cards a card review said the session moved past.
func (currentModel *model) withdrawCards(session *store.Session, labels []string, cardLabels map[string]string, callId string) {
	for _, label := range labels {
		suggestion := currentModel.repository.FindSuggestion(cardLabels[strings.TrimSpace(label)])
		if suggestion == nil {
			currentModel.logReviewDecision(session, callId, "withdraw", label, "dropped: no such waiting card")
			continue
		}
		currentModel.logReviewDecision(session, callId, "withdraw", store.CardSummary(*suggestion), "withdrawn")
		currentModel.repository.Withdraw(suggestion.Id)
	}
}

// applyProgress takes a progress review's title, unless the user pinned one or it only rewords the current one, and its progress.
func (currentModel *model) applyProgress(session *store.Session, promptCount int, answer brain.ReviewAnswer, callId string) {
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
		currentModel.logReviewDecision(session, callId, "retitle", title, "dropped: pinned")
	case len([]rune(title)) > maximumTitleLength:
		currentModel.logReviewDecision(session, callId, "retitle", title, "dropped: too long")
	case wasTitled && isRewording(session.Title, title):
		currentModel.logReviewDecision(session, callId, "retitle", session.Title+" → "+title, "dropped: same name, reworded")
	default:
		currentModel.logReviewDecision(session, callId, "retitle", session.Title+" → "+title, "applied")
		currentModel.retitle(session, title)
	}
}

// offerCards offers a card review's keeps, one of each kind, and its rule as suggestions, each in place of the session's waiting
// card of its kind, unless the user already said no to it or enough are waiting.
func (currentModel *model) offerCards(session *store.Session, answer brain.ReviewAnswer, callId string) {
	offered := map[string]bool{}
	for _, keep := range answer.Keeps {
		if keep.Kind == "" || keep.Kind == "none" {
			continue
		}
		suggestion, outcome := brain.KeepSuggestion(keep, *session, currentModel.digests[session.SessionId], &currentModel.repository)
		if offered[keep.Kind] {
			outcome = "dropped: one of its kind already"
		} else if outcome == "proposed" {
			suggestion.CallId, suggestion.KeepKind = callId, keep.Kind
			outcome = currentModel.offerInPlace(session, suggestion)
		}
		offered[keep.Kind] = true
		currentModel.logReviewDecision(session, callId, "keep "+keep.Kind, fmt.Sprintf("%s (%.2f)", keep.Reason, keep.Confidence), outcome)
	}
	if len(offered) == 0 {
		currentModel.logReviewDecision(session, callId, "keep none", "", "nothing to keep")
	}
	if strings.TrimSpace(answer.Rule.Rule) != "" {
		suggestion, outcome := brain.RuleSuggestion(answer.Rule, *session, currentModel.laptopTexts())
		if outcome == "proposed" {
			suggestion.CallId, suggestion.KeepKind = callId, "rule"
			outcome = currentModel.offerInPlace(session, suggestion)
		}
		currentModel.logReviewDecision(session, callId, "rule", fmt.Sprintf("%s (%.2f)", answer.Rule.Rule, answer.Rule.Confidence), outcome)
	}
}

// offerInPlace offers a card review's card, replacing the session's waiting card of the same kind, and says what happened.
func (currentModel *model) offerInPlace(session *store.Session, suggestion store.Suggestion) string {
	repository := &currentModel.repository
	for _, waiting := range repository.WaitingFor(session.SessionId) {
		if waiting.KeepKind == suggestion.KeepKind {
			if repository.Replace(waiting.Id, suggestion) {
				return "proposed, in place of the waiting one"
			}
			return "withheld: already rejected, or enough waiting"
		}
	}
	if repository.Offer(suggestion) {
		return "proposed"
	}
	return "withheld: already rejected, or enough waiting"
}

func (currentModel *model) logReviewDecision(session *store.Session, callId string, action string, detail string, outcome string) {
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "review", SessionId: session.SessionId, FolderId: session.FolderId,
		Action: action, Detail: detail, Outcome: outcome, CallId: callId}))
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

// logCall keeps every model call whole in calls.jsonl, its prompt, files, answer, cost and time, for evaluating den later,
// and the system prompt once per version. It returns the call's id, or "" when nothing was called.
func (currentModel *model) logCall(done jobDoneMessage) string {
	if done.request.Prompt == "" {
		return ""
	}
	call := store.Call{
		Job:           done.job.kind.String(),
		SessionId:     done.job.sessionId,
		Model:         done.usage.Model,
		PromptVersion: promptVersion(done.request.System),
		Prompt:        done.request.Prompt,
		Files:         done.request.Files,
		InputTokens:   done.usage.InputTokens,
		OutputTokens:  done.usage.OutputTokens,
		CostDollars:   done.usage.CostDollars,
		Milliseconds:  done.usage.Duration.Milliseconds(),
	}
	call.SlimFailure = done.usage.SlimFailure
	if done.err != nil {
		call.Error = done.err.Error()
	} else if answer, err := json.Marshal(done.answer); err == nil {
		call.Answer = answer
	}
	currentModel.reportError(currentModel.repository.KeepSystemPrompt(call.PromptVersion, done.request.System))
	callId, err := currentModel.repository.LogCall(call)
	currentModel.reportError(err)
	return callId
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
