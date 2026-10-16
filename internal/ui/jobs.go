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
	reviewEvery = 5
	// baselineAfter is how old a session's last activity is for den to start counting from now instead of reviewing it.
	baselineAfter = time.Hour
	// maximumTitleLength drops titles that ignored the naming rules.
	maximumTitleLength = 40
	// maximumHelperFailures turns the model off for the rest of the run, so a broken login doesn't fail every 20 seconds.
	maximumHelperFailures = 3
	maximumNotesPerReview = 3
	maximumRulesPerReview = 2
	minimumMoveConfidence = 0.75
)

type jobKind int

const (
	reviewJob jobKind = iota
	chatJob
	draftJob
	// reviseJob answers what the user told den about one of its suggestions in the dock.
	reviseJob
)

func (kind jobKind) String() string {
	return [...]string{"review", "chat", "draft", "revise"}[kind]
}

// job is one model call. Its input is built when it starts, so it sees the latest state.
type job struct {
	kind         jobKind
	sessionId    string
	folderId     string
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

// automaticChange is the last note den added on its own, which u undoes.
type automaticChange struct {
	filePath     string
	previousText string
	folderName   string
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
	case draftJob:
		parentNotes := repository.ReadContext("")
		if folder := repository.FindFolder(next.folderId); folder != nil && folder.ParentId != "" {
			parentNotes += "\n" + currentModel.folderNotesText(folder.ParentId)
		}
		return run(brain.DraftRequest(repository, next.folderId, currentModel.digests, parentNotes), &brain.DraftAnswer{}, jobDoneMessage{job: next})
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
	case *brain.DraftAnswer:
		currentModel.applyDraft(done.job.folderId, *answer)
	}
	return tea.Batch(currentModel.animateRetitles(), currentModel.startNextJob())
}

// quotedEvidence is the user's words from a review's evidence, without the speaker or quotes the model puts around them.
func quotedEvidence(evidence string) string {
	evidence = strings.TrimSpace(evidence)
	if speaker, words, found := strings.Cut(evidence, ":"); found && strings.EqualFold(strings.TrimSpace(speaker), "user") {
		evidence = strings.TrimSpace(words)
	}
	return strings.Trim(evidence, "\"'“”")
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

// queueDrafts asks the model to write the drafts den proposed from facts only.
func (currentModel *model) queueDrafts() {
	for _, suggestion := range currentModel.repository.Suggestions {
		if suggestion.Kind == store.SuggestContext && suggestion.Starter == store.StarterDraft && !suggestion.ByModel {
			currentModel.queueJob(job{kind: draftJob, folderId: suggestion.FolderId})
		}
	}
}

// applyReview carries out a review: a new name right away, notes into den's own notes with undo,
// and a move or rules as proposals. Everything is logged.
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
	if folderName := strings.TrimSpace(answer.Move.Folder); folderName != "" {
		folder := currentModel.folderNamed(folderName)
		switch {
		case session.FolderId != "" || session.FiledByHand:
			logDecision("move", folderName, "dropped: already filed")
		case brain.OnlyAskedQuestions(currentModel.digests, sessionId):
			logDecision("move", folderName, "dropped: it only asked questions, so it's for the archive")
		case folder == "" || answer.Move.Confidence < minimumMoveConfidence:
			logDecision("move", fmt.Sprintf("%s (%.2f)", folderName, answer.Move.Confidence), "withheld")
		default:
			repository.Offer(store.Suggestion{Kind: store.SuggestMove, SessionIds: []string{sessionId}, FolderId: folder, Reason: answer.Move.Reason})
			logDecision("move", folderName, "proposed")
		}
	}
	currentModel.addReviewNotes(session, answer, logDecision)
	for _, proposed := range answer.Rules[:min(len(answer.Rules), maximumRulesPerReview)] {
		rule := strings.TrimSpace(proposed.Rule)
		if rule == "" {
			continue
		}
		reason := "You said: \"" + quotedEvidence(proposed.Evidence) + "\""
		switch {
		case proposed.Target == "laptop":
			currentModel.offerLaptopRule(rule, reason+". A rule for every repo.")
		case proposed.Target == "folder" && session.FolderId != "":
			currentModel.offerRule(session.FolderId, rule, reason+". A rule for every session in "+repository.FolderName(session.FolderId)+".", session.Title)
		default:
			currentModel.offerRule("", rule, reason+". A rule for every session in "+currentModel.repositoryName()+".", session.Title)
		}
		logDecision("addRule", proposed.Target+": "+rule, "proposed")
	}
	currentModel.saveRepository()
}

// addReviewNotes writes a review's notes into den's notes for the session's folder, only when that file already exists:
// den keeps notes it has, but starting them is a draft the user approves.
func (currentModel *model) addReviewNotes(session *store.Session, answer brain.ReviewAnswer, logDecision func(string, string, string)) {
	if session.FolderId == "" || len(answer.Notes) == 0 {
		return
	}
	notesPath, err := currentModel.repository.ContextPath(session.FolderId)
	if currentModel.reportError(err) {
		return
	}
	previousText := store.ReadText(notesPath)
	if strings.TrimSpace(previousText) == "" {
		for _, note := range answer.Notes {
			logDecision("notesAdd", note.Line, "withheld: the folder has no notes of den's")
		}
		return
	}
	text, added := previousText, 0
	for _, note := range answer.Notes[:min(len(answer.Notes), maximumNotesPerReview)] {
		line := strings.TrimSpace(note.Line)
		if line == "" || store.HasRule(text, line) {
			continue
		}
		text = store.AddToSection(text, note.Section, line)
		added++
		logDecision("notesAdd", note.Section+": "+line, "applied")
	}
	if added == 0 || currentModel.reportError(currentModel.repository.WriteContext(session.FolderId, text)) {
		return
	}
	folderName := currentModel.repository.FolderName(session.FolderId)
	currentModel.lastAutomaticChange = &automaticChange{filePath: notesPath, previousText: previousText, folderName: folderName}
	currentModel.refreshContextTokens()
	lines := "a line"
	if added > 1 {
		lines = fmt.Sprintf("%d lines", added)
	}
	currentModel.setStatus("den added " + lines + " to " + folderName + "'s notes · u undoes it")
}

// offerRule proposes a rule for a folder's notes or, with no folder, the repo's context.
func (currentModel *model) offerRule(folderId string, rule string, reason string, source string) {
	current := currentModel.repository.ReadContext(folderId)
	if store.HasRule(current, rule) {
		return
	}
	if strings.TrimSpace(current) == "" {
		current = "# " + currentModel.contextHeading(folderId) + "\n"
	}
	currentModel.repository.Offer(store.Suggestion{Kind: store.SuggestContext, FolderId: folderId, ProposedText: store.AddToSection(current, "Rules", rule), Source: source, Reason: reason})
}

// offerLaptopRule proposes a rule for the laptop files that don't have it yet.
func (currentModel *model) offerLaptopRule(rule string, reason string) {
	var filePaths []string
	for _, laptopFile := range currentModel.laptopFiles {
		if (laptopFile.Exists || laptopFile.Installed) && !store.HasRule(laptopFile.Text, rule) {
			filePaths = append(filePaths, laptopFile.Path)
		}
	}
	if len(filePaths) > 0 {
		currentModel.repository.Offer(store.Suggestion{Kind: store.SuggestLaptopRule, Rule: rule, FilePaths: filePaths, Reason: reason})
	}
}

// applyDraft puts the model's draft into the waiting proposal, which keeps its place and its rejection key.
func (currentModel *model) applyDraft(folderId string, answer brain.DraftAnswer) {
	for index := range currentModel.repository.Suggestions {
		suggestion := &currentModel.repository.Suggestions[index]
		if suggestion.Kind == store.SuggestContext && suggestion.Starter == store.StarterDraft && suggestion.FolderId == folderId {
			suggestion.ByModel = true
			if notes := strings.TrimSpace(answer.Notes); notes != "" {
				suggestion.ProposedText = notes + "\n"
				suggestion.Reason = "New sessions in " + currentModel.repository.FolderName(folderId) + " will start with these."
			}
		}
	}
	currentModel.saveRepository()
}

// undoAutomaticChange puts back the notes den last changed on its own.
func (currentModel *model) undoAutomaticChange() {
	change := currentModel.lastAutomaticChange
	if change == nil {
		currentModel.setStatus("nothing den did on its own to undo")
		return
	}
	currentModel.lastAutomaticChange = nil
	if currentModel.reportError(store.WriteText(change.filePath, change.previousText)) {
		return
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "review", Action: "notesAdd", Detail: change.folderName, Outcome: "undone"}))
	currentModel.refreshContextTokens()
	currentModel.setStatus("undone · " + change.folderName + "'s notes are back as they were")
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
