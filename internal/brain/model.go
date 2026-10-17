package brain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	maximumTurnsPerReview = 10
	maximumChatSessions   = 60
	maximumChatMessages   = 12
	maximumFirstRunInput  = 300
	contextHeadLength     = 300
	notesExcerptLength    = 1500
)

// NotesSections are where review notes go in a folder's notes.
var NotesSections = []string{"Decisions", "Status", "Open questions"}

const titleRules = "A title is 2-4 words in sentence case and at most 28 characters, like \"Fix duplicate coupons\". It starts with an imperative verb (Fix, Add, Remove, Refactor, " +
	"Investigate, Review, Explain, Plan, Write, Design), names the session's goal rather than its latest step, is specific enough " +
	"to tell apart from other sessions (never \"Fix bug\"), and has no branch names or issue keys."

const reviewSystem = "You are den. You organize a developer's AI coding agent sessions into folders and keep short notes per folder. " +
	"You look at one session and decide whether anything about it should change. Most of the time nothing should: return empty values. " +
	"Never invent facts; everything must come from the user's own words in the turns.\n\n" +
	"title: the session's name in the tree. Return \"\" to keep the current one, which is the normal answer. Rename only when the current " +
	"title is a raw prompt, too long, or would mislead someone looking for this session. " + titleRules + "\n\n" +
	"move: only for a session in the Inbox. Name an existing folder exactly as listed when the session clearly belongs there, " +
	"with your confidence from 0 to 1, and a reason of at most 10 words, like \"It edits src/cart.js, like the others there.\" " +
	"Otherwise return folder \"\".\n\n" +
	"notes: lines for the notes of the folder the session is in, only when someone starting a new session on this work next week would need them: " +
	"a decision the user stated or confirmed, a status change of a piece of work, an open question raised or answered, a change of scope. " +
	"Never implementation steps, debugging, file edits, things the agent proposed that the user didn't confirm, one-off corrections, " +
	"or anything the notes already say. A correction about how agents should work is a rule, never also a note. " +
	"At most 3 short lines, each with evidence quoting the user. Return none for a session in the Inbox.\n\n" +
	"rules: only from a clear correction by the user about how agents should work, like \"don't add comments\" or \"always run the tests with X\". " +
	"target folder for a rule about one area of work, repo for the whole repo, laptop for a personal preference that holds in any repo. " +
	"Evidence quotes the user. Usually there are none.\n\n" +
	"summary: one plain sentence, at most 15 words, on what the session is doing and where it stands, " +
	"like \"Coupons no longer apply twice; tests pass, case handling is next.\""

const chatSystem = "You are den. You organize a developer's AI coding agent sessions into folders, with notes per folder that every new " +
	"session in the folder starts with. You chat with the developer about the sessions, folders and notes of one repo. " +
	"Be brief: the user reads you in a narrow terminal panel. Plain text, no markdown, no filler like \"Okay\" or \"Sure\", no apologies. " +
	"When you propose actions, the reply is one short sentence of at most 12 words, because den lists the actions under it: never repeat them, " +
	"never explain how to confirm. When you answer a question, use at most 3 short sentences, from the facts given; when they don't say, " +
	"say you don't know. Call sessions by their title, never by their id. " +
	"You never run code and never touch the repo.\n\n" +
	"How context works: a folder's context is its linked file, when it has one, plus den's notes for it. A session starts with the repo's notes, " +
	"then the context of each folder above it, then its own folder's. Linking a file adds it next to den's notes; it never replaces them. " +
	"FOLDERS says where each folder's context comes from.\n\n" +
	"When the chat is about one of den's suggestions, the user's message answers it: do it as is (acceptSuggestion), do something else " +
	"instead (the actions that do what they want), or just answer a question about it, with no actions.\n\n" +
	"When the user asks for a change, put it in actions; den lists them and the user confirms with enter. Only include actions the user asked for. " +
	"Use folder names and session ids exactly as listed, and leave unused fields empty. Action types:\n" +
	"- link: use a markdown file as a folder's context (folder, file)\n" +
	"- unlink: stop using a folder's linked file (folder)\n" +
	"- move: put a session in a folder (session, folder; folder \"Inbox\" for none)\n" +
	"- renameSession: (session, name)\n" +
	"- renameFolder: (folder, name)\n" +
	"- createFolder: a new folder (name; folder is its parent, or empty for the top level)\n" +
	"- archive: (session)\n" +
	"- addRule: a rule for agents (target folder, repo or laptop; folder when the target is folder; rule)\n" +
	"- moveNotes: move den's notes, and a linked file, from one folder into another (from, folder)\n" +
	"- editNotes: rewrite den's notes for a folder, or the repo's with folder \"repo\" (folder, text: the whole new notes in markdown)\n" +
	"- acceptSuggestion: do den's suggestion the chat is about, as it is"

const draftSystem = "You write the first notes for a folder of AI coding sessions. Every new session in the folder starts with them, " +
	"so they say what someone picking up this work needs: what it is and why, decisions the user made, status, open questions, " +
	"and pointers to key files. Use only facts from the sessions given; never invent. Markdown, starting with \"# <folder name>\", " +
	"with a section only when you have something for it (## Goal, ## Decisions, ## Status, ## Open questions, ## Pointers). " +
	"Status says in one short line per piece of work what the sessions show was done, without retelling the conversation. " +
	"Open questions are only questions the user asked that weren't answered; " +
	"never add your own. Under 200 words. No implementation steps."

const firstRunSystem = "You group a developer's past AI coding sessions in one repo into a few folders, the way the developer would: " +
	"by feature, area or kind of work. Prefer a few broad folders over many small ones: merge closely related topics into one " +
	"(coupons and payments both belong in a checkout folder, for example). Propose at most 6 folders, each with at least 2 sessions, " +
	"and leave out sessions that fit nowhere, like one-off questions. " +
	"Folder names are 1-3 words, title case, and specific: never Misc, Other, General or similar. " +
	"The description says in a few words what the folder covers. Also name every session, including the ones in no folder. " + titleRules

// ReviewInput is what a review looks at: one session, the turns since den last looked, and the folders.
type ReviewInput struct {
	Session         store.Session
	Digest          agent.Digest
	Turns           []agent.Turn
	FirstTurn       int
	Repository      *store.Repository
	FolderNotes     string
	RepositoryNotes string
}

type ReviewAnswer struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Move    struct {
		Folder     string  `json:"folder"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	} `json:"move"`
	Notes []struct {
		Section  string `json:"section"`
		Line     string `json:"line"`
		Evidence string `json:"evidence"`
	} `json:"notes"`
	Rules []struct {
		Target   string `json:"target"`
		Rule     string `json:"rule"`
		Evidence string `json:"evidence"`
	} `json:"rules"`
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
	writeField(&prompt, "branch", input.Session.Branch)
	writeField(&prompt, "issue", input.Digest.IssueKey)
	writeField(&prompt, "first prompt", startOfText(input.Digest.FirstPrompt, 600))
	writeField(&prompt, "files edited", strings.Join(relativePaths(repository.RepositoryPath, input.Digest.TouchedFiles, 15), ", "))
	writeField(&prompt, "pull requests", strings.Join(input.Digest.PullRequests, ", "))
	prompt.WriteString("\nTURNS SINCE DEN LAST LOOKED\n")
	turns := input.Turns
	firstTurn := input.FirstTurn
	if len(turns) > maximumTurnsPerReview {
		firstTurn += len(turns) - maximumTurnsPerReview
		turns = turns[len(turns)-maximumTurnsPerReview:]
	}
	for index, turn := range turns {
		fmt.Fprintf(&prompt, "turn %d\n", firstTurn+index+1)
		writeField(&prompt, "agent", turn.Agent)
		writeField(&prompt, "user", turn.User)
	}
	prompt.WriteString("\nFOLDERS\n" + folderOverview(repository))
	if input.Session.FolderId != "" {
		prompt.WriteString("\nTHIS FOLDER'S NOTES\n" + orNone(notesExcerpt(input.FolderNotes)) + "\n")
	}
	prompt.WriteString("\nREPO NOTES\n" + orNone(notesExcerpt(input.RepositoryNotes)) + "\n")
	return helper.Request{
		System: reviewSystem,
		Prompt: prompt.String(),
		Schema: objectSchema(map[string]any{
			"title":   stringSchema(),
			"summary": stringSchema(),
			"move":    objectSchema(map[string]any{"folder": stringSchema(), "confidence": map[string]any{"type": "number"}, "reason": stringSchema()}),
			"notes":   arraySchema(objectSchema(map[string]any{"section": enumSchema(NotesSections...), "line": stringSchema(), "evidence": stringSchema()})),
			"rules":   arraySchema(objectSchema(map[string]any{"target": enumSchema("folder", "repo", "laptop"), "rule": stringSchema(), "evidence": stringSchema()})),
		}),
	}
}

// ChatInput is what the chat knows: the conversation, what it's about, and the repo's folders and sessions.
type ChatInput struct {
	Conversation    []ChatMessage
	Repository      *store.Repository
	Digests         map[string]agent.Digest
	SubjectFolder   string
	SubjectSession  string
	About           string
	SubjectNotes    string
	RepositoryNotes string
}

type ChatAnswer struct {
	Reply   string `json:"reply"`
	Actions []struct {
		Type    string `json:"type"`
		Folder  string `json:"folder"`
		Session string `json:"session"`
		File    string `json:"file"`
		Name    string `json:"name"`
		Target  string `json:"target"`
		Rule    string `json:"rule"`
		From    string `json:"from"`
		Text    string `json:"text"`
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
	prompt.WriteString("\nSESSIONS (id | title | folder | branch | files edited | latest prompt)\n")
	for index, sessionId := range ChatSessionIds(input) {
		session := repository.FindSession(sessionId)
		digest := input.Digests[sessionId]
		state := ""
		if session.Archived {
			state = " (archived)"
		}
		fmt.Fprintf(&prompt, "s%d | %s%s | %s | %s | %s | %s\n", index+1, session.Title, state, repository.FolderName(session.FolderId), session.Branch,
			strings.Join(relativePaths(repository.RepositoryPath, digest.TouchedFiles, 5), ", "), startOfText(digest.LastPrompt, 160))
	}
	if input.SubjectNotes != "" {
		prompt.WriteString("\nNOTES OF THE FOLDER THE CHAT IS ABOUT\n" + notesExcerpt(input.SubjectNotes) + "\n")
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
				"from":    stringSchema(),
				"text":    stringSchema(),
				"folder":  stringSchema(),
				"session": stringSchema(),
				"file":    stringSchema(),
				"name":    stringSchema(),
				"target":  enumSchema("", "folder", "repo", "laptop"),
				"rule":    stringSchema(),
			})),
		}),
	}
}

type DraftAnswer struct {
	Notes string `json:"notes"`
}

// DraftRequest asks for a folder's first notes from what its sessions did.
func DraftRequest(repository *store.Repository, folderId string, digests map[string]agent.Digest, parentNotes string) helper.Request {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "FOLDER: %s\n\nSESSIONS\n", repository.FolderName(folderId))
	for _, session := range repository.Sessions {
		if session.FolderId != folderId || session.Archived {
			continue
		}
		digest := digests[session.SessionId]
		fmt.Fprintf(&prompt, "- %s\n", session.Title)
		writeField(&prompt, "  branch", session.Branch)
		writeField(&prompt, "  first prompt", startOfText(digest.FirstPrompt, 500))
		if digest.LastPrompt != digest.FirstPrompt {
			writeField(&prompt, "  latest prompt", startOfText(digest.LastPrompt, 300))
		}
		writeField(&prompt, "  files edited", strings.Join(relativePaths(repository.RepositoryPath, digest.TouchedFiles, 8), ", "))
	}
	if parentNotes != "" {
		prompt.WriteString("\nNOTES THE FOLDER ALREADY GETS FROM ABOVE (don't repeat them)\n" + notesExcerpt(parentNotes) + "\n")
	}
	return helper.Request{System: draftSystem, Prompt: prompt.String(), Schema: objectSchema(map[string]any{"notes": stringSchema()})}
}

type FirstRunAnswer struct {
	Folders []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Sessions    []string `json:"sessions"`
	} `json:"folders"`
	Titles []struct {
		Session string `json:"session"`
		Title   string `json:"title"`
	} `json:"titles"`
}

// FirstRunRequest asks how to group past sessions and what to name them; the model names them s1, s2 and so on.
func FirstRunRequest(digests []agent.Digest, repositoryPath string) helper.Request {
	var prompt strings.Builder
	prompt.WriteString("SESSIONS (id | branch | files edited | the agent's own title | first prompt)\n")
	for index, digest := range digests[:min(len(digests), maximumFirstRunInput)] {
		fmt.Fprintf(&prompt, "s%d | %s | %s | %s | %s\n", index+1, digest.Branch, strings.Join(relativePaths(repositoryPath, digest.TouchedFiles, 3), ", "),
			digest.AgentTitle, startOfText(digest.FirstPrompt, 200))
	}
	return helper.Request{
		System: firstRunSystem,
		Prompt: prompt.String(),
		Schema: objectSchema(map[string]any{
			"folders": arraySchema(objectSchema(map[string]any{"name": stringSchema(), "description": stringSchema(), "sessions": arraySchema(stringSchema())})),
			"titles":  arraySchema(objectSchema(map[string]any{"session": stringSchema(), "title": stringSchema()})),
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
