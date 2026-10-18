package brain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	maximumTurnsPerReview = 10
	// maximumWholeSessionPrompts and wholeSessionPromptLength keep a whole-session review to the user's prompts, briefly.
	maximumWholeSessionPrompts = 40
	wholeSessionPromptLength   = 240
	maximumChatSessions        = 60
	maximumChatMessages        = 12
	// maximumChatTranscripts and maximumChatTurns are how much of the sessions' conversations a chat reads,
	// so "make this chat the folder's context" has the chat to work from.
	maximumChatTranscripts = 3
	maximumChatTurns       = 12
	// maximumTranscriptFileTurns is how much of each session's conversation its file for the chat holds: the latest turns.
	maximumTranscriptFileTurns = 40
	contextHeadLength          = 300
	notesExcerptLength         = 1500
	// maximumEditedContextLength is how much of a context den reads when asked to change it: all of any sensible one.
	maximumEditedContextLength = 60000
)

// editingInstructions tell den how to change a context it was given in full, so a small change never rewrites the rest.
const editingInstructions = "Do what they ask with one action: replaceText to change a passage, with from an exact passage of the current text " +
	"and text what replaces it, or editNotes only when the whole text should be rewritten. To change a title, replace just that line."

const titleRules = "A title is one emoji for the session's kind, a space, then a subject of 2-4 words in sentence case and at most 28 characters, " +
	"like \"🐛 Duplicate coupons\". The kinds: 🔍 investigate, explore, explain, review or research; 🐛 fix something broken; " +
	"✨ build a feature or add something new; 🧹 refactor or clean up; 📋 plan or design; 📝 write docs, notes or prompts; " +
	"💬 a quick question or small talk. Use only these emoji. The subject has no verb, since the emoji says the kind, and never " +
	"the repo's name, since every session is in that repo. It names the session's goal rather than its latest step, is specific enough " +
	"to tell apart from other sessions (never \"🐛 Bug\"), and has no branch names or issue keys."

// reviewIntroduction is what both reviews know about den and its folders.
const reviewIntroduction = "You are den. You watch a developer's AI coding agent sessions in one repo. Sessions sit in folders, and each folder has context: " +
	"notes every new session started in it begins with. A folder holds either a piece of work, like a feature, or a way of working, like research " +
	"(a skill). Never invent facts; everything must come from the session.\n\n"

// progressReviewSystem keeps a session's name and where it stands current, often and cheaply, while the user works.
const progressReviewSystem = reviewIntroduction + "You look at one session and answer two things.\n\n" +
	"title: the session's name in the tree. Return \"\" to keep the current one, which is the normal answer. Rename only when the current " +
	"title is a raw prompt, too long, or would mislead someone looking for this session. " + titleRules + "\n\n" +
	"progress: where the session stands, for whoever picks it up later. Plain facts, no voice. Update the current progress " +
	"rather than starting over: goal is what the session is after, at most 12 words, kept unless the goal changed; done is what's " +
	"finished, decided or found so far, at most 20 words, folding in what's new; next is what comes next or what it waits on, " +
	"at most 12 words, or \"\" when nothing is left. Like goal \"Stop coupons applying twice\", done \"Fixed the double apply; " +
	"tests pass\", next \"Handle mixed-case coupon codes\"."

// cardReviewSystem looks at all of a session once it went quiet, and decides whether any of it is worth a card.
const cardReviewSystem = reviewIntroduction + "The session went quiet, and you look at all of it to answer two things. " +
	"Most of the time nothing is worth keeping: return kind none and an empty rule.\n\n" + keepRules + "\n\n" + ruleRules

// denVoice is how den talks to the user, wherever it does: concise and clear first, with a light, upbeat personality.
// It never applies to titles or context files, which are working material.
const denVoice = "Your voice: concise and clear first, then warmth and play, like a sharp, cheerful teammate. Short sentences, plain words, " +
	"contractions, and one light touch per message where it fits: a playful verb (\"tuck it into Eval\"), a dry aside, a small " +
	"\"nice\", \"neat\" or \"hmm\". " +
	"Never cringe: no emoji, no exclamation pile-ups, no \"Great question\", no puns that bend the meaning, no hype, no baby talk. " +
	"Clarity always wins over flavor, but don't be flat: most replies should carry that one light touch. Vary it, and never reuse " +
	"the example phrases below word for word; they show the tone, they aren't lines to repeat. Examples of the voice: " +
	"\"Just Eval so far, and it's travelling light: no sessions, no context yet.\" / \"Hmm, that quote doesn't really back a folder. " +
	"Eval already has this covered. Drop it?\" / \"Spin up a Research skill folder?\" / \"Neat, Pagination's notes now know about the 500 cap.\""

const chatSystem = "You are den. You organize a developer's AI coding agent sessions into folders, with context per folder that every new " +
	"session in the folder starts with. A folder holds a piece of work, like a feature, or a way of working, a skill, like Exploration with " +
	"subfolders Papers and Reddit for where to search. You chat with the developer about the sessions, folders and context of one repo. " +
	"The user reads you in a narrow terminal panel: plain text, no markdown, no filler like \"Okay\" or \"Sure\", no apologies. " +
	denVoice + " " +
	"When you propose actions, the reply is one short question of at most 8 words asking the user to confirm, like \"Make the Exploration folder?\", " +
	"because den shows the actions in a plan card under it: never repeat them, never say you did them, never explain how to confirm. When you answer a question, say everything the answer needs and nothing more, from the facts given: for where work stands, " +
	"one short paragraph or line per session, by title, with when it was last used. When the facts don't say, " +
	"say you don't know. Call sessions by their title, never by their id. " +
	"You never run code and never change the repo. You can read the repo's files, at REPO PATH, but only when the user asks you to " +
	"look at a file or at what the repo says, like \"put PLAN.md into its context\"; otherwise answer from the facts given.\n\n" +
	"Finding where work stands: SESSIONS lists every session with when it was last used, whether its agent is open, and den's notes on " +
	"its goal, what's done and what's next. Answer from that list when it's enough, like \"what did I work on Friday?\" against TODAY. " +
	"Each session's whole conversation is in the file its line names, like s3.md, in your working directory: read the ones you need " +
	"whenever the list doesn't say enough, like what was decided or exactly where it stopped, and only those.\n\n" +
	"Picking work back up: when the user wants to continue earlier work in a new session, propose startSession with a handoff brief " +
	"you write after reading that session's file, for the agent that starts with it: markdown with only sections you have facts for " +
	"(## Where it stands, ## Decisions and constraints, ## Next, ## Pointers with the files involved), under 200 words, facts only.\n\n" +
	"How context works: each folder has one context file. By default it's den's own; linking a file of the user's, like docs/PLAN.md, " +
	"makes that file the context instead, and unlinking brings den's back. A session starts with the repo's notes, " +
	"then the context of each folder above it, then its own folder's. editNotes rewrites whichever file a folder's context is.\n\n" +
	"Writing context is your main job: people rarely write it themselves, they ask you to. When asked to make context from a chat or a session, " +
	"write it from TRANSCRIPTS: what someone continuing the work, or working this way again, needs to know. Markdown starting with \"# <folder name>\", " +
	"only sections you have facts for (## Goal, ## Findings, ## Decisions, ## Open questions, ## Pointers; for a skill, ## How to work), " +
	"under 200 words, facts only, no implementation steps. A subfolder's context only adds what its parents' context doesn't say, " +
	"since sessions there start with both. Use editNotes for an existing folder, or createFolder with text for a new one, " +
	"plus a move when the session belongs in it.\n\n" +
	"When the chat is about one of den's suggestions, the user's message answers it: do it as is (acceptSuggestion), do something else " +
	"instead (the actions that do what they want), drop it (withdrawSuggestion), or answer a question about it. You made that suggestion: " +
	"explain it from the evidence quoted with it, as your own reasoning. If the evidence doesn't hold up against the session, say so plainly, " +
	"end by asking whether to drop it, and include the withdrawSuggestion action, since the user's enter does exactly the actions you list. " +
	"Whenever your reply ends with a question offering a change, the actions for that change must be in actions.\n\n" +
	"When the user asks for a change, put it in actions; den lists them and the user confirms with enter. Only include actions the user asked for. " +
	"Use folder names and session ids exactly as listed, and leave unused fields empty. Action types:\n" +
	"- link: use a markdown file as a folder's context (folder, file)\n" +
	"- unlink: stop using a folder's linked file (folder)\n" +
	"- move: put a session in a folder (session, folder; folder \"Inbox\" for none)\n" +
	"- renameSession: (session, name)\n" +
	"- renameFolder: (folder, name)\n" +
	"- createFolder: a new folder (name; folder is its parent, or empty for the top level; text is its notes, or empty)\n" +
	"- archive: (session)\n" +
	"- addRule: a rule for agents (target folder, repo or laptop, where laptop is the user's CLAUDE.md and AGENTS.md for every repo; " +
	"folder when the target is folder; rule). When the user says a rule den suggested belongs in a folder or the repo instead, use this with that target.\n" +
	"- moveNotes: move one folder's context into another (from, folder)\n" +
	"- replaceText: change one passage of a folder's context, or the repo's with folder \"repo\" (folder, from: the exact current passage, " +
	"text: what replaces it). Prefer it to editNotes for any change that leaves the rest as it is.\n" +
	"- editNotes: rewrite a folder's context, or the repo's with folder \"repo\" (folder, text: the whole new notes in markdown)\n" +
	"- startSession: open a new agent session (folder: where it goes, as listed, \"Inbox\" for none, or a folder createFolder makes; " +
	"session: the session it continues, or empty; text: the handoff brief when it picks up earlier work, or empty for a fresh session)\n" +
	"- acceptSuggestion: do den's suggestion the chat is about, as it is\n" +
	"- withdrawSuggestion: drop den's suggestion the chat is about, when the user doesn't want it or its evidence doesn't hold up"

const firstRunSystem = "You are den, opened for the first time in a repo. You look at the developer's most recent AI coding agent sessions " +
	"there and decide which to bring in. den archives sessions a few days after their last use, so bring in only what's worth returning to. " +
	"Judge by what each session did and how it ended, never by its branch. For every session:\n" +
	"verdict: continue when the work isn't done: the agent's latest reply asks the user something, stops partway, or the user said they'd come back. " +
	"keep when it's done but found or decided something worth having later. done for everything else: finished work, a quick question, " +
	"nothing worth returning to. Most short sessions are done.\n" +
	"title: " + titleRules + "\n" +
	"A long session may wander across topics; then its title names what most of its prompts were about, not just the first one.\n" +
	"why: at most 10 words on why it's worth bringing in, or \"\" when done.\n" +
	"You only see the start, a few prompts from the middle and the end of each session, so never propose folders or notes here; a session you keep gets a full review later."

// ReviewInput is what a review looks at: one session, the turns since den last looked, and the folders.
type ReviewInput struct {
	Session store.Session
	Digest  agent.Digest
	// Turns are the latest turns, numbered from FirstTurn; the ones from NewFromTurn on are new since den last looked.
	Turns       []agent.Turn
	FirstTurn   int
	NewFromTurn int
	// AllPrompts are every prompt of the session. Given, the review is the card review of a session that went quiet;
	// otherwise it's the progress review of its latest turns.
	AllPrompts      []string
	Repository      *store.Repository
	FolderNotes     string
	RepositoryNotes string
	// LaptopRules is what the user's own files for every repo already say.
	LaptopRules string
	// Lessons are what den learned from the cards the user said no to.
	Lessons string
}

type ReviewAnswer struct {
	Title    string         `json:"title"`
	Progress store.Progress `json:"progress"`
	Keep     Keep           `json:"keep"`
	Rule     Rule           `json:"rule"`
}

func ReviewRequest(input ReviewInput) helper.Request {
	repository := input.Repository
	var prompt strings.Builder
	prompt.WriteString("SESSION\n")
	title := input.Session.Title
	if input.Session.TitledAtPrompt == 0 {
		title += " (this is still the raw first prompt, so give the session a real title)"
	}
	fmt.Fprintf(&prompt, "title: %s\nfolder: %s\n", title, repository.FolderName(input.Session.FolderId))
	writeField(&prompt, "first prompt", startOfText(input.Digest.FirstPrompt, 600))
	writeField(&prompt, "files edited", strings.Join(relativePaths(repository.RepositoryPath, input.Digest.TouchedFiles, 15), ", "))
	writeField(&prompt, "goal so far", input.Session.Progress.Goal)
	writeField(&prompt, "done so far", input.Session.Progress.Done)
	writeField(&prompt, "next so far", input.Session.Progress.Next)
	isCardReview := len(input.AllPrompts) > 0
	if isCardReview {
		prompt.WriteString("\nEVERY PROMPT THE USER TYPED\n")
		for index, userPrompt := range input.AllPrompts[max(len(input.AllPrompts)-maximumWholeSessionPrompts, 0):] {
			fmt.Fprintf(&prompt, "%d. %s\n", index+1, startOfText(userPrompt, wholeSessionPromptLength))
		}
	}
	fmt.Fprintf(&prompt, "\nLATEST TURNS (turns marked new came after den last looked)\n")
	writeTurns(&prompt, input.Turns, input.FirstTurn, maximumTurnsPerReview, input.NewFromTurn)
	writeField(&prompt, "agent's latest reply", input.Digest.LastAgentMessage)
	if !isCardReview {
		return helper.Request{
			System: progressReviewSystem,
			Prompt: prompt.String(),
			Schema: objectSchema(map[string]any{"title": stringSchema(), "progress": objectSchema(map[string]any{"goal": stringSchema(), "done": stringSchema(), "next": stringSchema()})}),
			Tier:   helper.Light,
		}
	}
	prompt.WriteString("\nFOLDERS\n" + folderOverview(repository))
	if skillFiles := writtenSkillFiles(input.Digest); len(skillFiles) > 0 {
		prompt.WriteString("\nSKILL FILES THIS SESSION WROTE\n")
		for _, skillFile := range skillFiles {
			fmt.Fprintf(&prompt, "- %s: %s\n", relativeTo(repository.RepositoryPath, skillFile), orNone(skillDescription(skillFile)))
		}
	}
	if input.Session.FolderId != "" {
		prompt.WriteString("\nTHIS FOLDER'S CONTEXT\n" + orNone(notesExcerpt(input.FolderNotes)) + "\n")
	}
	prompt.WriteString("\nREPO NOTES\n" + orNone(notesExcerpt(input.RepositoryNotes)) + "\n")
	prompt.WriteString("\nLAPTOP RULES (already in every repo)\n" + orNone(notesExcerpt(input.LaptopRules)) + "\n")
	prompt.WriteString(lessonsSection(input.Lessons))
	return helper.Request{
		System: cardReviewSystem,
		Prompt: prompt.String(),
		Schema: objectSchema(map[string]any{"keep": keepSchema(), "rule": ruleSchema()}),
		Tier:   helper.Reasoning,
	}
}

// writeTurns writes the last few turns, numbered from the start of the session, marking the ones from newFromTurn on
// as new; a negative newFromTurn marks none.
func writeTurns(prompt *strings.Builder, turns []agent.Turn, firstTurn int, maximum int, newFromTurn int) {
	if len(turns) > maximum {
		firstTurn += len(turns) - maximum
		turns = turns[len(turns)-maximum:]
	}
	for index, turn := range turns {
		number := firstTurn + index
		mark := ""
		if newFromTurn >= 0 && number >= newFromTurn {
			mark = " (new)"
		}
		fmt.Fprintf(prompt, "turn %d%s\n", number+1, mark)
		writeField(prompt, "agent", turn.Agent)
		writeField(prompt, "user", turn.User)
	}
}

// FirstRunVerdict is what the first run decides about one past session.
type FirstRunVerdict struct {
	Session string `json:"session"`
	Verdict string `json:"verdict"`
	Title   string `json:"title"`
	Why     string `json:"why"`
}

type FirstRunAnswer struct {
	Sessions []FirstRunVerdict `json:"sessions"`
}

// LongSessionPrompts is how many prompts make a session long enough that the first run also sees prompts from its middle.
const LongSessionPrompts = 10

// middlePromptSamples is how many prompts the first run sees from the middle of a long session.
const middlePromptSamples = 6

// FirstRunRequest asks which recent sessions are worth bringing in; the model names them s1, s2 and so on.
// promptsBySessionId holds every prompt of the long sessions, so the model sees where they went after the first prompt.
func FirstRunRequest(digests []agent.Digest, promptsBySessionId map[string][]string, repository *store.Repository, now time.Time) helper.Request {
	var prompt strings.Builder
	for index, digest := range digests {
		fmt.Fprintf(&prompt, "SESSION s%d · last used %s ago · %d prompts\n", index+1, roughAge(now.Sub(digest.LastActivityAt)), digest.PromptCount)
		writeField(&prompt, "files edited", strings.Join(relativePaths(repository.RepositoryPath, digest.TouchedFiles, 6), ", "))
		writeField(&prompt, "first prompt", startOfText(digest.FirstPrompt, 400))
		for _, middlePrompt := range middlePrompts(promptsBySessionId[digest.SessionId]) {
			writeField(&prompt, "later prompt", startOfText(middlePrompt, 100))
		}
		if digest.LastPrompt != digest.FirstPrompt {
			writeField(&prompt, "last prompt", startOfText(digest.LastPrompt, 300))
		}
		writeField(&prompt, "agent's latest reply", startOfText(digest.LastAgentMessage, 400))
		prompt.WriteString("\n")
	}
	prompt.WriteString("FOLDERS\n" + folderOverview(repository))
	return helper.Request{
		System: firstRunSystem,
		Prompt: prompt.String(),
		Tier:   helper.Reasoning,
		Schema: objectSchema(map[string]any{
			"sessions": arraySchema(objectSchema(map[string]any{
				"session": stringSchema(),
				"verdict": enumSchema("continue", "keep", "done"),
				"title":   stringSchema(),
				"why":     stringSchema(),
			})),
		}),
	}
}

func roughAge(elapsed time.Duration) string {
	switch {
	case elapsed < time.Hour:
		return fmt.Sprintf("%d minutes", int(elapsed.Minutes()))
	case elapsed < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(elapsed.Hours()))
	}
	return fmt.Sprintf("%d days", int(elapsed.Hours()/24))
}

// Transcript is the end of one session's conversation, for the chat to read.
type Transcript struct {
	SessionId        string
	Turns            []agent.Turn
	LastAgentMessage string
}

// ChatInput is what the chat knows: the conversation, what it's about, the repo's folders and sessions,
// and the conversations of the sessions it's about.
type ChatInput struct {
	Conversation    []ChatMessage
	Repository      *store.Repository
	Digests         map[string]agent.Digest
	SubjectFolder   string
	SubjectSession  string
	About           string
	SubjectNotes    string
	RepositoryNotes string
	Transcripts     []Transcript
	// EditingContext is set when the user asks from a folder's context pane, so the answer is a change to that context.
	EditingContext bool
	Lessons        string
	Now            time.Time
	// AgentStatuses are the open agents' statuses by session id: running, waiting or done.
	AgentStatuses map[string]string
	// SessionTurns are the conversations of the listed sessions by session id, which the chat reads as files when it needs them.
	SessionTurns map[string][]agent.Turn
	// CallId is the logged call that answered, set once it has, so the actions it proposed trace back to it.
	CallId string
}

type ChatAnswer struct {
	Reply   string `json:"reply"`
	Actions []struct {
		Type    string `json:"type"`
		Folder  string `json:"folder"`
		Session string `json:"session"`
		File    string `json:"file"`
		Name    string `json:"name"`
		From    string `json:"from"`
		Text    string `json:"text"`
		Target  string `json:"target"`
		Rule    string `json:"rule"`
	} `json:"actions"`
}

// ChatSessionIds are the sessions the chat lists, in order; the model names them s1, s2 and so on.
func ChatSessionIds(input ChatInput) []string {
	var sessionIds []string
	if input.SubjectSession != "" {
		sessionIds = append(sessionIds, input.SubjectSession)
	}
	for _, session := range input.Repository.Sessions {
		if len(sessionIds) == maximumChatSessions {
			break
		}
		if session.SessionId != input.SubjectSession {
			sessionIds = append(sessionIds, session.SessionId)
		}
	}
	return sessionIds
}

// ChatTranscriptSessionIds are the sessions whose conversations the chat reads: the session it's about,
// the latest sessions of the folder it's about, or for the whole repo the latest session, which "this session" usually means.
// Sessions are kept newest first.
func ChatTranscriptSessionIds(repository *store.Repository, subjectFolder string, subjectSession string) []string {
	if subjectSession != "" {
		return []string{subjectSession}
	}
	var sessionIds []string
	for _, session := range repository.Sessions {
		if subjectFolder == "" && !session.Archived {
			return []string{session.SessionId}
		}
		if session.FolderId == subjectFolder && len(sessionIds) < maximumChatTranscripts {
			sessionIds = append(sessionIds, session.SessionId)
		}
	}
	return sessionIds
}

func ChatRequest(input ChatInput) helper.Request {
	repository := input.Repository
	var prompt strings.Builder
	switch {
	case input.EditingContext && input.SubjectFolder == "":
		prompt.WriteString("THE USER IS EDITING THE REPO'S CONTEXT, folder \"repo\". " + editingInstructions + "\n")
	case input.EditingContext:
		prompt.WriteString("THE USER IS EDITING THE CONTEXT OF FOLDER " + repository.FolderName(input.SubjectFolder) + ". " + editingInstructions + "\n")
	case input.About != "":
		prompt.WriteString("THE CHAT IS ABOUT THIS SUGGESTION OF DEN'S\n" + input.About + "\n")
	case input.SubjectSession != "":
		prompt.WriteString("THE CHAT IS ABOUT SESSION s1, in folder " + repository.FolderName(repository.FindSession(input.SubjectSession).FolderId) + "\n")
	case input.SubjectFolder != "":
		prompt.WriteString("THE CHAT IS ABOUT FOLDER " + repository.FolderName(input.SubjectFolder) + "\n")
	default:
		prompt.WriteString("THE CHAT IS ABOUT THE WHOLE REPO " + filepath.Base(repository.RepositoryPath) + "\n")
	}
	prompt.WriteString("\nTODAY\n" + input.Now.Format("Monday, Jan 2 2006, 15:04") + "\n")
	prompt.WriteString("\nREPO PATH\n" + repository.RepositoryPath + "\n")
	prompt.WriteString("\nFOLDERS\n" + folderOverview(repository))
	prompt.WriteString("\nSESSIONS (newest first)\n")
	sessionIds := ChatSessionIds(input)
	files := map[string]string{}
	for index, sessionId := range sessionIds {
		handle := fmt.Sprintf("s%d", index+1)
		session := repository.FindSession(sessionId)
		prompt.WriteString(sessionIndexLine(handle, session, input))
		if turns := input.SessionTurns[sessionId]; len(turns) > 0 {
			files[handle+".md"] = transcriptFile(session, turns, input.Digests[sessionId])
		}
	}
	for _, transcript := range input.Transcripts {
		session := repository.FindSession(transcript.SessionId)
		if session == nil {
			continue
		}
		fmt.Fprintf(&prompt, "\nTRANSCRIPT OF s%d \"%s\" (its latest turns)\n", slices.Index(sessionIds, transcript.SessionId)+1, session.Title)
		writeTurns(&prompt, transcript.Turns, 0, maximumChatTurns, -1)
		writeField(&prompt, "agent's latest reply", transcript.LastAgentMessage)
	}
	switch {
	case input.EditingContext:
		prompt.WriteString("\nTHE WHOLE CURRENT CONTEXT BEING EDITED\n" + orNone(truncateText(input.SubjectNotes, maximumEditedContextLength)) + "\n")
	case input.SubjectNotes != "":
		prompt.WriteString("\nCONTEXT OF THE FOLDER THE CHAT IS ABOUT\n" + notesExcerpt(input.SubjectNotes) + "\n")
	}
	prompt.WriteString("\nREPO NOTES\n" + orNone(notesExcerpt(input.RepositoryNotes)) + "\n")
	prompt.WriteString(lessonsSection(input.Lessons))
	prompt.WriteString("\nCONVERSATION\n")
	conversation := input.Conversation[max(len(input.Conversation)-maximumChatMessages, 0):]
	for _, message := range conversation {
		speaker := "user"
		if message.FromDen {
			speaker = "den"
		}
		fmt.Fprintf(&prompt, "%s: %s\n", speaker, message.Text)
	}
	return helper.Request{
		System:          chatSystem,
		Prompt:          prompt.String(),
		Tier:            helper.Reasoning,
		ReadsRepository: repository.RepositoryPath,
		Files:           files,
		Schema: objectSchema(map[string]any{
			"reply": stringSchema(),
			"actions": arraySchema(objectSchema(map[string]any{
				"type": enumSchema("link", "unlink", "move", "renameSession", "renameFolder", "createFolder", "archive", "addRule",
					"moveNotes", "replaceText", "editNotes", "startSession", "acceptSuggestion", "withdrawSuggestion"),
				"target":  enumSchema("", "folder", "repo", "laptop"),
				"rule":    stringSchema(),
				"from":    stringSchema(),
				"text":    stringSchema(),
				"folder":  stringSchema(),
				"session": stringSchema(),
				"file":    stringSchema(),
				"name":    stringSchema(),
			})),
		}),
	}
}

var agentStatusDescriptions = map[string]string{"running": "working", "waiting": "waiting for the user", "done": "done, the user's turn"}

// sessionIndexLine is what the chat always knows about a session: one line with when it was used, its agent, den's notes on
// where it stands, and the file holding its conversation, if there is one.
func sessionIndexLine(handle string, session *store.Session, input ChatInput) string {
	repository := input.Repository
	digest := input.Digests[session.SessionId]
	fields := []string{handle, session.Title}
	if session.Archived {
		fields[1] += " (archived)"
	}
	fields = append(fields, "folder "+repository.FolderName(session.FolderId))
	lastUsed := digest.LastActivityAt
	if lastUsed.IsZero() {
		lastUsed = session.LastActivityAt
	}
	fields = append(fields, "last used "+lastUsed.In(input.Now.Location()).Format("Mon Jan 2 15:04")+", "+roughAge(input.Now.Sub(lastUsed))+" ago")
	agentStatus := "agent closed"
	if status, open := input.AgentStatuses[session.SessionId]; open {
		agentStatus = "agent open"
		if described := agentStatusDescriptions[status]; described != "" {
			agentStatus += ", " + described
		}
	}
	fields = append(fields, agentStatus)
	labeled := []struct{ label, value string }{
		{"branch", digest.Branch},
		{"pull request", digest.PullRequest},
		{"goal", session.Progress.Goal},
		{"done", session.Progress.Done},
		{"next", session.Progress.Next},
		{"agent's recap", digest.Recap},
		{"files edited", strings.Join(relativePaths(repository.RepositoryPath, digest.TouchedFiles, 5), ", ")},
		{"latest prompt", startOfText(digest.LastPrompt, 160)},
	}
	for _, field := range labeled {
		if strings.TrimSpace(field.value) != "" {
			fields = append(fields, field.label+": "+field.value)
		}
	}
	if len(input.SessionTurns[session.SessionId]) > 0 {
		fields = append(fields, "conversation in "+handle+".md")
	}
	return strings.Join(fields, " | ") + "\n"
}

// transcriptFile is a session's conversation as the chat reads it from a file: how it started, then its latest turns.
func transcriptFile(session *store.Session, turns []agent.Turn, digest agent.Digest) string {
	var file strings.Builder
	file.WriteString("# " + session.Title + "\n\n")
	writeField(&file, "first prompt", startOfText(digest.FirstPrompt, 600))
	file.WriteString("\n")
	writeTurns(&file, turns, 0, maximumTranscriptFileTurns, -1)
	writeField(&file, "agent's latest reply", digest.LastAgentMessage)
	return file.String()
}

// folderOverview lists the folders with what's in them, so the model can file by more than a name.
func folderOverview(repository *store.Repository) string {
	if len(repository.Folders) == 0 {
		return "(no folders yet)\n"
	}
	var overview strings.Builder
	for _, folder := range repository.Folders {
		name := folder.Name
		if parent := repository.FindFolder(folder.ParentId); parent != nil {
			name = parent.Name + " / " + folder.Name
		}
		var titles []string
		for _, session := range repository.Sessions {
			if session.FolderId == folder.Id && !session.Archived && len(titles) < 5 {
				titles = append(titles, session.Title)
			}
		}
		fmt.Fprintf(&overview, "- %s (%d sessions)", name, repository.SessionCount(folder.Id))
		context := strings.TrimSpace(repository.ReadContext(folder.Id))
		switch {
		case context == "":
			overview.WriteString(" | no context")
		case folder.LinkedPath != "":
			fmt.Fprintf(&overview, " | context, the user's file %s: %s", relativeTo(repository.RepositoryPath, folder.LinkedPath), startOfText(context, contextHeadLength))
		default:
			fmt.Fprintf(&overview, " | context: %s", startOfText(context, contextHeadLength))
		}
		if len(titles) > 0 {
			fmt.Fprintf(&overview, " | sessions: %s", strings.Join(titles, "; "))
		}
		overview.WriteString("\n")
	}
	return overview.String()
}

// middlePrompts are a few prompts spread evenly between the first and the last.
func middlePrompts(prompts []string) []string {
	if len(prompts) < 3 {
		return nil
	}
	middle := prompts[1 : len(prompts)-1]
	count := min(len(middle), middlePromptSamples)
	var samples []string
	for index := range count {
		samples = append(samples, middle[index*len(middle)/count])
	}
	return samples
}

func writeField(builder *strings.Builder, name string, value string) {
	if strings.TrimSpace(value) != "" {
		fmt.Fprintf(builder, "%s: %s\n", name, value)
	}
}

func relativePaths(repositoryPath string, filePaths []string, maximum int) []string {
	var relative []string
	for _, filePath := range filePaths[:min(len(filePaths), maximum)] {
		relative = append(relative, relativeTo(repositoryPath, filePath))
	}
	return relative
}

func startOfText(text string, length int) string {
	text = strings.Join(strings.Fields(text), " ")
	return truncateText(text, length)
}

// notesExcerpt keeps the start of a notes file with its lines, so its sections stay readable.
func notesExcerpt(text string) string {
	return truncateText(strings.TrimSpace(text), notesExcerptLength)
}

func orNone(text string) string {
	if strings.TrimSpace(text) == "" {
		return "(none)"
	}
	return text
}

// objectSchema lists every property as required and allows no others, which both CLIs' strict schemas need.
func objectSchema(properties map[string]any) map[string]any {
	var required []string
	for name := range properties {
		required = append(required, name)
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
