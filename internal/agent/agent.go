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
}

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
	PromptCount      int
	// TouchedFiles are the files the agent edited, in the order it first touched them.
	TouchedFiles []string
	// AgentTitle is the name the tool itself gave the session, when it makes one.
	AgentTitle string
	// IssueKey is the issue the session works on: the key in its branch, or else the first one typed in a prompt.
	IssueKey string
	// PullRequests are the pull request links that came up in the session, in order.
	PullRequests []string
	// Plan is the latest plan the user approved in the tool's plan mode.
	Plan string
	// StartingFiles are the markdown files the first prompt points at, like @docs/PLAN.md, that exist.
	StartingFiles  []string
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
	notIssuePrefixes   = map[string]bool{"UTF": true, "SHA": true, "ISO": true, "RFC": true, "CVE": true, "HTTP": true, "TLS": true, "SSL": true, "AES": true, "RSA": true, "PEP": true, "GPT": true, "UTC": true, "GMT": true, "ES": true}
	pullRequestPattern = regexp.MustCompile(`https://github\.com/[\w.-]+/[\w.-]+/pull/\d+|https://gitlab\.[\w.-]+/[\w./-]+/-/merge_requests/\d+`)
)

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

// notePullRequests adds the pull request links in a transcript line to the digest.
func (digest *Digest) notePullRequests(line json.RawMessage) {
	for _, link := range pullRequestPattern.FindAllString(string(line), -1) {
		digest.PullRequests = appendOnce(digest.PullRequests, link)
	}
}

// settleIssueKey prefers the issue key in the branch over one typed in a prompt.
func (digest *Digest) settleIssueKey(promptIssueKey string) {
	digest.IssueKey = IssueKeyIn(digest.Branch)
	if digest.IssueKey == "" {
		digest.IssueKey = promptIssueKey
	}
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
