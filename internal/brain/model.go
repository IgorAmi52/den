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
	maximumChatSessions   = 60
	maximumChatMessages   = 12
	// maximumChatTranscripts and maximumChatTurns are how much of the sessions' conversations a chat reads,
	// so "make this chat the folder's context" has the chat to work from.
	maximumChatTranscripts = 3
	maximumChatTurns       = 12
	contextHeadLength      = 300
	notesExcerptLength     = 1500
)

const titleRules = "A title is 2-4 words in sentence case and at most 28 characters, like \"Fix duplicate coupons\". It starts with an imperative verb (Fix, Add, Remove, Refactor, " +
	"Investigate, Review, Explain, Plan, Write, Design), names the session's goal rather than its latest step, is specific enough " +
	"to tell apart from other sessions (never \"Fix bug\"), and has no branch names or issue keys."

const reviewSystem = "You are den. You watch a developer's AI coding agent sessions in one repo. Sessions sit in folders, and each folder has context: " +
	"notes every new session started in it begins with. A folder holds either a piece of work, like a feature, or a way of working, like research " +
	"(a skill). You look at one session and answer three things. Most of the time nothing should change: return empty values and kind none. " +
	"Never invent facts; everything must come from the session.\n\n" +
	"title: the session's name in the tree. Return \"\" to keep the current one, which is the normal answer. Rename only when the current " +
	"title is a raw prompt, too long, or would mislead someone looking for this session. " + titleRules + "\n\n" +
	"summary: one plain sentence, at most 15 words, on what the session is doing and where it stands, " +
	"like \"Coupons no longer apply twice; tests pass, case handling is next.\"\n\n" + keepRules + "\n\n" + ruleRules

const chatSystem = "You are den. You organize a developer's AI coding agent sessions into folders, with context per folder that every new " +
	"session in the folder starts with. A folder holds a piece of work, like a feature, or a way of working, a skill, like Exploration with " +
	"subfolders Papers and Reddit for where to search. You chat with the developer about the sessions, folders and context of one repo. " +
	"Be brief: the user reads you in a narrow terminal panel. Plain text, no markdown, no filler like \"Okay\" or \"Sure\", no apologies. " +
	"When you propose actions, the reply is one short sentence of at most 12 words, because den lists the actions under it: never repeat them, " +
	"never explain how to confirm. When you answer a question, use at most 3 short sentences, from the facts given; when they don't say, " +
	"say you don't know. Call sessions by their title, never by their id. " +
	"You never run code and never touch the repo.\n\n" +
	"How context works: a folder's context is its linked file, when it has one, plus den's notes for it. A session starts with the repo's notes, " +
	"then the context of each folder above it, then its own folder's. Linking a file adds it next to den's notes; it never replaces them. " +
	"FOLDERS says where each folder's context comes from.\n\n" +
	"Writing context is your main job: people rarely write it themselves, they ask you to. When asked to make context from a chat or a session, " +
	"write it from TRANSCRIPTS: what someone continuing the work, or working this way again, needs to know. Markdown starting with \"# <folder name>\", " +
	"only sections you have facts for (## Goal, ## Findings, ## Decisions, ## Open questions, ## Pointers, or ## How to work for a skill), " +
	"under 200 words, facts only, no implementation steps. Use editNotes for an existing folder, or createFolder with text for a new one, " +
	"plus a move when the session belongs in it.\n\n" +
	"When the chat is about one of den's suggestions, the user's message answers it: do it as is (acceptSuggestion), do something else " +
	"instead (the actions that do what they want), or just answer a question about it, with no actions.\n\n" +
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
	"- moveNotes: move den's notes, and a linked file, from one folder into another (from, folder)\n" +
	"- editNotes: rewrite den's notes for a folder, or the repo's with folder \"repo\" (folder, text: the whole new notes in markdown)\n" +
	"- acceptSuggestion: do den's suggestion the chat is about, as it is"

const firstRunSystem = "You are den, opened for the first time in a repo. You look at the developer's most recent AI coding agent sessions " +
	"there and decide which to bring in. den archives sessions a few days after their last use, so bring in only what's worth returning to. " +
	"Judge by what each session did and how it ended, never by its branch. For every session:\n" +
	"verdict: continue when the work isn't done: the agent's latest reply asks the user something, stops partway, or the user said they'd come back. " +
	"keep when it's done but found or decided something worth having later. done for everything else: finished work, a quick question, " +
	"nothing worth returning to. Most short sessions are done.\n" +
	"title: " + titleRules + "\n" +
	"why: at most 10 words on why it's worth bringing in, or \"\" when done.\n\n" +
	"No folders exist yet unless FOLDERS lists some; every session is in the Inbox. Fill keep only for a keep verdict, with kind folder or skill, " +
	"otherwise kind none.\n" + keepRules

// ReviewInput is what a review looks at: one session, the turns since den last looked, and the folders.
type ReviewInput struct {
	Session         store.Session
	Digest          agent.Digest
	Turns           []agent.Turn
	FirstTurn       int
	Repository      *store.Repository
	FolderNotes     string
	RepositoryNotes string
	// LaptopRules is what the user's own files for every repo already say.
	LaptopRules string
}

type ReviewAnswer struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Keep    Keep   `json:"keep"`
	Rule    Rule   `json:"rule"`
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
	prompt.WriteString("\nTURNS SINCE DEN LAST LOOKED\n")
	writeTurns(&prompt, input.Turns, input.FirstTurn, maximumTurnsPerReview)
	writeField(&prompt, "agent's latest reply", input.Digest.LastAgentMessage)
	prompt.WriteString("\nFOLDERS\n" + folderOverview(repository))
	if input.Session.FolderId != "" {
		prompt.WriteString("\nTHIS FOLDER'S CONTEXT\n" + orNone(notesExcerpt(input.FolderNotes)) + "\n")
	}
	prompt.WriteString("\nREPO NOTES\n" + orNone(notesExcerpt(input.RepositoryNotes)) + "\n")
	prompt.WriteString("\nLAPTOP RULES (already in every repo)\n" + orNone(notesExcerpt(input.LaptopRules)) + "\n")
	return helper.Request{
		System: reviewSystem,
		Prompt: prompt.String(),
		Schema: objectSchema(map[string]any{"title": stringSchema(), "summary": stringSchema(), "keep": keepSchema(), "rule": ruleSchema()}),
	}
}

// writeTurns writes the last few turns, numbered from the start of the session.
func writeTurns(prompt *strings.Builder, turns []agent.Turn, firstTurn int, maximum int) {
	if len(turns) > maximum {
		firstTurn += len(turns) - maximum
		turns = turns[len(turns)-maximum:]
	}
	for index, turn := range turns {
		fmt.Fprintf(prompt, "turn %d\n", firstTurn+index+1)
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
	Keep    Keep   `json:"keep"`
}

type FirstRunAnswer struct {
	Sessions []FirstRunVerdict `json:"sessions"`
}

// FirstRunRequest asks which recent sessions are worth bringing in; the model names them s1, s2 and so on.
func FirstRunRequest(digests []agent.Digest, repository *store.Repository, now time.Time) helper.Request {
	var prompt strings.Builder
	for index, digest := range digests {
		fmt.Fprintf(&prompt, "SESSION s%d · last used %s ago · %d prompts\n", index+1, roughAge(now.Sub(digest.LastActivityAt)), digest.PromptCount)
		writeField(&prompt, "files edited", strings.Join(relativePaths(repository.RepositoryPath, digest.TouchedFiles, 6), ", "))
		writeField(&prompt, "first prompt", startOfText(digest.FirstPrompt, 400))
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
		Schema: objectSchema(map[string]any{
			"sessions": arraySchema(objectSchema(map[string]any{
				"session": stringSchema(),
				"verdict": enumSchema("continue", "keep", "done"),
				"title":   stringSchema(),
				"why":     stringSchema(),
				"keep":    keepSchema(),
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
	case input.About != "":
		prompt.WriteString("THE CHAT IS ABOUT THIS SUGGESTION OF DEN'S\n" + input.About + "\n")
	case input.SubjectSession != "":
		prompt.WriteString("THE CHAT IS ABOUT SESSION s1, in folder " + repository.FolderName(repository.FindSession(input.SubjectSession).FolderId) + "\n")
	case input.SubjectFolder != "":
		prompt.WriteString("THE CHAT IS ABOUT FOLDER " + repository.FolderName(input.SubjectFolder) + "\n")
	default:
		prompt.WriteString("THE CHAT IS ABOUT THE WHOLE REPO " + filepath.Base(repository.RepositoryPath) + "\n")
	}
	prompt.WriteString("\nFOLDERS\n" + folderOverview(repository))
	prompt.WriteString("\nSESSIONS (id | title | folder | files edited | latest prompt)\n")
	sessionIds := ChatSessionIds(input)
	for index, sessionId := range sessionIds {
		session := repository.FindSession(sessionId)
		digest := input.Digests[sessionId]
		state := ""
		if session.Archived {
			state = " (archived)"
		}
		fmt.Fprintf(&prompt, "s%d | %s%s | %s | %s | %s\n", index+1, session.Title, state, repository.FolderName(session.FolderId),
			strings.Join(relativePaths(repository.RepositoryPath, digest.TouchedFiles, 5), ", "), startOfText(digest.LastPrompt, 160))
	}
	for _, transcript := range input.Transcripts {
		session := repository.FindSession(transcript.SessionId)
		if session == nil {
			continue
		}
		fmt.Fprintf(&prompt, "\nTRANSCRIPT OF s%d \"%s\" (its latest turns)\n", slices.Index(sessionIds, transcript.SessionId)+1, session.Title)
		writeTurns(&prompt, transcript.Turns, 0, maximumChatTurns)
		writeField(&prompt, "agent's latest reply", transcript.LastAgentMessage)
	}
	if input.SubjectNotes != "" {
		prompt.WriteString("\nCONTEXT OF THE FOLDER THE CHAT IS ABOUT\n" + notesExcerpt(input.SubjectNotes) + "\n")
	}
	prompt.WriteString("\nREPO NOTES\n" + orNone(notesExcerpt(input.RepositoryNotes)) + "\n")
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
		System: chatSystem,
		Prompt: prompt.String(),
		Schema: objectSchema(map[string]any{
			"reply": stringSchema(),
			"actions": arraySchema(objectSchema(map[string]any{
				"type": enumSchema("link", "unlink", "move", "renameSession", "renameFolder", "createFolder", "archive", "addRule",
					"moveNotes", "editNotes", "acceptSuggestion"),
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
		if folder.LinkedPath != "" {
			fmt.Fprintf(&overview, " | linked file: %s, starts: %s", relativeTo(repository.RepositoryPath, folder.LinkedPath),
				startOfText(store.ReadText(folder.LinkedPath), contextHeadLength))
		}
		if notes := strings.TrimSpace(repository.ReadContext(folder.Id)); notes != "" {
			fmt.Fprintf(&overview, " | den's notes: %s", startOfText(notes, contextHeadLength))
		}
		if folder.LinkedPath == "" && strings.TrimSpace(repository.ReadContext(folder.Id)) == "" {
			overview.WriteString(" | no context")
		}
		if len(titles) > 0 {
			fmt.Fprintf(&overview, " | sessions: %s", strings.Join(titles, "; "))
		}
		overview.WriteString("\n")
	}
	return overview.String()
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
