package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type Adapter interface {
	Name() string
	// NewSessionCommand returns the launch command and the session id, when the tool lets den choose it.
	NewSessionCommand(launch Launch) (command []string, sessionId string)
	ResumeCommand(sessionId string, launch Launch) []string
	// FindSession reports the session once its first prompt is saved; found stays false until then.
	// Tools that don't let den choose the id find it by working directory and launch time instead.
	FindSession(sessionId string, workingDirectory string, launchedAt time.Time) (foundSession FoundSession, found bool, err error)
	// Digest reads a session's transcript locally, without any model call.
	Digest(sessionId string) (digest Digest, found bool, err error)
	// PastSessions digests every session the tool recorded in one of the directories.
	PastSessions(directories []string) ([]Digest, error)
	// LaptopFilePath is the tool's own file of the user's rules, which it loads into every session in every repo.
	LaptopFilePath() (string, error)
	// Turns reads the user's prompts in order, each with the agent's last message before it.
	Turns(sessionId string) ([]Turn, error)
}

// Turn is one prompt the user typed, with what the agent said just before, since "nono, not like that"
// means nothing without it.
type Turn struct {
	Agent string
	User  string
}

// turnTextLength keeps each side of a turn short; the end of the agent's message is what the user answered.
const turnTextLength = 700

// Launch is what den adds to an agent's command line.
type Launch struct {
	// Context is appended to the agent's system prompt; empty means the session starts without context.
	Context string
	// WritableDirectories can be edited without a permission prompt, so the agent can update its notes.
	WritableDirectories []string
	// StatusCommand is run with "running" or "waiting" appended whenever the agent's state changes.
	StatusCommand []string
}

type FoundSession struct {
	SessionId   string
	FirstPrompt string
}

// Digest is what den knows about a session from its transcript alone.
type Digest struct {
	Tool             string
	SessionId        string
	WorkingDirectory string
	Branch           string
	FirstPrompt      string
	LastPrompt       string
	// LastAgentMessage is the end of the agent's latest reply, which says whether the work is done or waits on the user.
	LastAgentMessage string
	PromptCount      int
	// TouchedFiles are the files the agent edited, in the order it first touched them.
	TouchedFiles []string
	// AgentTitle is the name the tool itself gave the session, when it makes one.
	AgentTitle string
	// IssueKey is the issue the session works on: the key in its branch, or else the first one typed in a prompt.
	IssueKey string
	// Plan is the latest plan the user approved in the tool's plan mode.
	Plan string
	// StartingFiles are the markdown files the first prompt points at, like @docs/PLAN.md, that exist.
	StartingFiles []string
	// UsedSkills are the skills and slash commands run in the session, by name, once per run.
	UsedSkills []string
	// Recap is the tool's own latest note on where the session stands, when it writes one.
	Recap string
	// PullRequest is the link to the pull request the session opened, when the tool records one.
	PullRequest    string
	StartedAt      time.Time
	LastActivityAt time.Time
}

// LaptopFile is a tool's file of the user's own rules, which it loads in every repo.
type LaptopFile struct {
	Tool string
	Path string
}

var supportedAdapters = []Adapter{Claude{}, Codex{}}

var (
	issueKeyPattern = regexp.MustCompile(`\b([A-Z][A-Z0-9]+)-\d+\b`)
	// notIssuePrefixes look like issue keys but name standards and encodings, like UTF-8 or SHA-256.
	notIssuePrefixes = map[string]bool{"UTF": true, "SHA": true, "ISO": true, "RFC": true, "CVE": true, "HTTP": true, "TLS": true, "SSL": true, "AES": true, "RSA": true, "PEP": true, "GPT": true, "UTC": true, "GMT": true, "ES": true}
)

// maximumReasonLength keeps a waiting agent's reason to what fits on its pane border.
const maximumReasonLength = 70

// Hook states an agent's stop can mean.
const (
	// StateWaiting is an agent that needs the user: a permission to give, or a question to answer.
	StateWaiting = "waiting"
	// StateDone is an agent that finished its turn without asking anything.
	StateDone = "done"
)

// QuestionReasonPrefix starts the reason of an agent waiting on the question its reply ends on, not on a permission.
const QuestionReasonPrefix = "asks: "

// StopState reads why an agent stopped from the event its hook passed, and says it in a few words:
// waiting with what it asks permission for or the question its reply ends on, or done with what it did.
// An empty state means the event says nothing new, like Claude's reminder that it's still idle, so the pane keeps its state.
func StopState(event []byte) (state string, reason string) {
	var parsed struct {
		Message                   string `json:"message"`
		LastAssistantMessage      string `json:"last_assistant_message"`
		CodexLastAssistantMessage string `json:"last-assistant-message"`
	}
	if json.Unmarshal(event, &parsed) != nil {
		return StateWaiting, ""
	}
	if message := oneLine(parsed.Message); message != "" {
		if strings.Contains(message, "waiting for your input") {
			return "", ""
		}
		return StateWaiting, startOf(strings.TrimPrefix(strings.TrimPrefix(message, "Claude "), "needs your "), maximumReasonLength)
	}
	reply := parsed.LastAssistantMessage
	if reply == "" {
		reply = parsed.CodexLastAssistantMessage
	}
	if question := closingQuestion(reply); question != "" {
		return StateWaiting, startOf(QuestionReasonPrefix+question, maximumReasonLength)
	}
	return StateDone, startOf("done: "+firstSentence(reply), maximumReasonLength)
}

// questionMark matches a question mark that ends a sentence: followed by whitespace, closing marks, or the end.
// One inside a word or a link, like x.com/item?id=1, doesn't count.
var questionMark = regexp.MustCompile(`\?[*_"')\]]*(\s|$)`)

// closingQuestion is the question a reply ends on, if it ends on one. A question asked earlier and then answered,
// or followed by more work, isn't waiting on the user.
func closingQuestion(text string) string {
	text = strings.TrimSpace(text)
	if paragraphStart := strings.LastIndex(text, "\n\n"); paragraphStart >= 0 {
		text = text[paragraphStart+2:]
	}
	matches := questionMark.FindAllStringIndex(text, -1)
	if len(matches) == 0 || matches[len(matches)-1][1] != len(text) {
		return ""
	}
	end := matches[len(matches)-1][0] + 1
	start := 0
	for _, boundary := range sentenceEnd.FindAllStringIndex(text[:end-1], -1) {
		start = boundary[1]
	}
	return oneLine(strings.TrimLeft(text[start:end], "*#>- "))
}

// sentenceEnd matches where a sentence ends: a full stop, exclamation or question mark followed by whitespace, or a line break.
var sentenceEnd = regexp.MustCompile(`[.!?]\s+|\n`)

// firstSentence is text up to its first sentence end, on one line, without markdown markers.
func firstSentence(text string) string {
	text = oneLine(strings.TrimLeft(strings.TrimSpace(text), "*#>- "))
	if end := strings.IndexAny(text, ".!?"); end >= 0 && end+1 < len(text) && text[end+1] == ' ' {
		return text[:end+1]
	}
	return text
}

// IssueKeyIn finds the first issue key in text, like YT-123.
func IssueKeyIn(text string) string {
	for _, match := range issueKeyPattern.FindAllStringSubmatch(text, -1) {
		if !notIssuePrefixes[match[1]] {
			return match[0]
		}
	}
	return ""
}

// MarkdownFilesIn finds the markdown files a text points at that exist: @path, file:/…, ~/…, an absolute path,
// or a path relative to one of the directories.
func MarkdownFilesIn(text string, directories ...string) []string {
	var found []string
	for _, field := range strings.Fields(text) {
		candidate := strings.Trim(field, "`'\"()[]{},;:")
		candidate = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(candidate, "@"), "file://"), "file:")
		candidate = strings.TrimSuffix(candidate, ".")
		if !strings.EqualFold(filepath.Ext(candidate), ".md") {
			continue
		}
		if rest, isHome := strings.CutPrefix(candidate, "~/"); isHome {
			if userHome, err := os.UserHomeDir(); err == nil {
				candidate = filepath.Join(userHome, rest)
			}
		}
		bases := []string{""}
		if !filepath.IsAbs(candidate) {
			bases = directories
		}
		for _, base := range bases {
			filePath := filepath.Clean(filepath.Join(base, candidate))
			if information, err := os.Stat(filePath); err == nil && !information.IsDir() {
				found = appendOnce(found, filePath)
				break
			}
		}
	}
	return found
}

// LaptopFiles lists every supported tool's file of the user's own rules, whether it exists yet or not.
func LaptopFiles() []LaptopFile {
	var laptopFiles []LaptopFile
	for _, adapter := range supportedAdapters {
		if filePath, err := adapter.LaptopFilePath(); err == nil {
			laptopFiles = append(laptopFiles, LaptopFile{Tool: adapter.Name(), Path: filePath})
		}
	}
	return laptopFiles
}

func Installed() []Adapter {
	var installedAdapters []Adapter
	for _, adapter := range supportedAdapters {
		if _, err := exec.LookPath(adapter.Name()); err == nil {
			installedAdapters = append(installedAdapters, adapter)
		}
	}
	return installedAdapters
}

func ByName(name string) (Adapter, bool) {
	for _, adapter := range supportedAdapters {
		if adapter.Name() == name {
			return adapter, true
		}
	}
	return nil, false
}

// homeDirectory returns the tool's data directory: the override environment variable when set, otherwise ~/defaultName.
func homeDirectory(overrideVariable string, defaultName string) (string, error) {
	if override := os.Getenv(overrideVariable); override != "" {
		return override, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, defaultName), nil
}

// eachJsonLine decodes every JSON line of a session file until visit returns false.
func eachJsonLine(filePath string, visit func(line json.RawMessage) bool) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && json.Valid(line) && !visit(line) {
			return nil
		}
		if readErr != nil {
			return nil
		}
	}
}

// isTypedPrompt filters out the instructions and markup tools inject as user messages.
func isTypedPrompt(text string) bool {
	trimmedText := strings.TrimSpace(text)
	return trimmedText != "" && !strings.HasPrefix(trimmedText, "<") && !strings.HasPrefix(trimmedText, "# AGENTS.md") &&
		!strings.HasPrefix(trimmedText, "[Request interrupted")
}

type cachedDigest struct {
	size         int64
	modifiedTime time.Time
	digest       Digest
}

// digestCache keeps digests by transcript path, valid while the file's size and modification time stay the same.
var (
	digestCache      = map[string]cachedDigest{}
	digestCacheMutex sync.Mutex
)

func cachedDigestOf(transcriptPath string, read func(transcriptPath string) Digest) (Digest, error) {
	information, err := os.Stat(transcriptPath)
	if err != nil {
		return Digest{}, err
	}
	digestCacheMutex.Lock()
	cached, exists := digestCache[transcriptPath]
	digestCacheMutex.Unlock()
	if exists && cached.size == information.Size() && cached.modifiedTime.Equal(information.ModTime()) {
		return cached.digest, nil
	}
	digest := read(transcriptPath)
	if digest.LastActivityAt.IsZero() {
		digest.LastActivityAt = information.ModTime()
	}
	digestCacheMutex.Lock()
	digestCache[transcriptPath] = cachedDigest{size: information.Size(), modifiedTime: information.ModTime(), digest: digest}
	digestCacheMutex.Unlock()
	return digest, nil
}

type cachedTurns struct {
	size         int64
	modifiedTime time.Time
	turns        []Turn
}

// turnsCache keeps turns by transcript path, valid while the file's size and modification time stay the same,
// so a chat can list many sessions' conversations without reading every transcript again.
var (
	turnsCache      = map[string]cachedTurns{}
	turnsCacheMutex sync.Mutex
)

func cachedTurnsOf(transcriptPath string, read func(transcriptPath string) ([]Turn, error)) ([]Turn, error) {
	information, err := os.Stat(transcriptPath)
	if err != nil {
		return nil, err
	}
	turnsCacheMutex.Lock()
	cached, exists := turnsCache[transcriptPath]
	turnsCacheMutex.Unlock()
	if exists && cached.size == information.Size() && cached.modifiedTime.Equal(information.ModTime()) {
		return cached.turns, nil
	}
	turns, err := read(transcriptPath)
	if err != nil {
		return nil, err
	}
	turnsCacheMutex.Lock()
	turnsCache[transcriptPath] = cachedTurns{size: information.Size(), modifiedTime: information.ModTime(), turns: turns}
	turnsCacheMutex.Unlock()
	return turns, nil
}

// settleIssueKey prefers the issue key in the branch over one typed in a prompt.
func (digest *Digest) settleIssueKey(promptIssueKey string) {
	digest.IssueKey = IssueKeyIn(digest.Branch)
	if digest.IssueKey == "" {
		digest.IssueKey = promptIssueKey
	}
}

// endOf keeps the last characters of a text, where the agent's question or result usually is.
func endOf(text string, length int) string {
	runes := []rune(oneLine(text))
	if len(runes) <= length {
		return string(runes)
	}
	return "…" + string(runes[len(runes)-length:])
}

// startOf keeps the first characters of a text.
func startOf(text string, length int) string {
	runes := []rune(oneLine(text))
	if len(runes) <= length {
		return string(runes)
	}
	return string(runes[:length]) + "…"
}

func appendOnce(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// SkillFile finds the file of a skill or slash command of the user's: in the repo, or in their own claude, codex
// or shared agents directory. Built-in commands, like Claude's /compact, have none.
func SkillFile(name string, repositoryPath string) (string, bool) {
	if strings.ContainsAny(name, "/:") || strings.HasPrefix(name, ".") {
		return "", false
	}
	homePath, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	claudeDirectory, _ := homeDirectory("CLAUDE_CONFIG_DIR", ".claude")
	codexDirectory, _ := homeDirectory("CODEX_HOME", ".codex")
	directories := []string{filepath.Join(repositoryPath, ".claude"), filepath.Join(repositoryPath, ".codex"), filepath.Join(repositoryPath, ".agents"),
		claudeDirectory, codexDirectory, filepath.Join(homePath, ".agents")}
	for _, directory := range directories {
		for _, candidate := range []string{filepath.Join(directory, "skills", name, "SKILL.md"), filepath.Join(directory, "commands", name+".md")} {
			if information, err := os.Stat(candidate); err == nil && !information.IsDir() {
				return candidate, true
			}
		}
	}
	return "", false
}
