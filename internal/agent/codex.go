package agent

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Codex writes a session file on the first prompt, and its first line records the launch time and working directory.
// A session launched by den is the one whose recorded launch time is closest to den's.
const codexLaunchTimeTolerance = 10 * time.Second

// codexPatchedFile finds the files an apply_patch call changes.
var codexPatchedFile = regexp.MustCompile(`\*\*\* (?:Update|Add) File: (\S+)`)

// codexReadSkill finds the skill a tool call loads: Codex runs a skill, asked for with $name or picked by the agent,
// by reading its SKILL.md.
var codexReadSkill = regexp.MustCompile(`/skills/([A-Za-z0-9][A-Za-z0-9_-]*)/SKILL\.md`)

type Codex struct{}

type codexSessionStart struct {
	Type    string `json:"type"`
	Payload struct {
		Id           string    `json:"id"`
		Timestamp    time.Time `json:"timestamp"`
		Cwd          string    `json:"cwd"`
		ThreadSource string    `json:"thread_source"`
		Git          struct {
			Branch string `json:"branch"`
		} `json:"git"`
	} `json:"payload"`
}

type codexEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		Type      string `json:"type"`
		Role      string `json:"role"`
		Input     string `json:"input"`
		Arguments string `json:"arguments"`
		Content   []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

func (Codex) Name() string {
	return "codex"
}

func (Codex) NewSessionCommand(launch Launch) ([]string, string) {
	return append([]string{"codex"}, codexLaunchArguments(launch)...), ""
}

func (Codex) ResumeCommand(sessionId string, launch Launch) []string {
	return append([]string{"codex", "resume", sessionId}, codexLaunchArguments(launch)...)
}

// codexLaunchArguments pass the context as developer instructions and the status command as notify,
// which Codex runs when a turn completes. Both values are TOML.
func codexLaunchArguments(launch Launch) []string {
	var arguments []string
	if launch.Context != "" {
		arguments = append(arguments, "-c", "developer_instructions="+strconv.Quote(launch.Context))
		for _, directory := range launch.WritableDirectories {
			arguments = append(arguments, "--add-dir", directory)
		}
	}
	if len(launch.StatusCommand) > 0 {
		var quoted []string
		for _, argument := range append(slices.Clone(launch.StatusCommand), "waiting") {
			quoted = append(quoted, strconv.Quote(argument))
		}
		arguments = append(arguments, "-c", "notify=["+strings.Join(quoted, ",")+"]")
	}
	return arguments
}

func (Codex) FindSession(_ string, workingDirectory string, launchedAt time.Time) (FoundSession, bool, error) {
	resolvedWorkingDirectory := resolvedPath(workingDirectory)
	closestFilePath, closestSessionId := "", ""
	closestDistance := codexLaunchTimeTolerance
	err := eachCodexSession(launchedAt, func(filePath string, start codexSessionStart) {
		if resolvedPath(start.Payload.Cwd) != resolvedWorkingDirectory {
			return
		}
		distance := start.Payload.Timestamp.Sub(launchedAt).Abs()
		if distance <= closestDistance {
			closestFilePath, closestSessionId, closestDistance = filePath, start.Payload.Id, distance
		}
	})
	if err != nil || closestFilePath == "" {
		return FoundSession{}, false, err
	}
	digest := readCodexDigest(closestFilePath)
	return FoundSession{SessionId: closestSessionId, FirstPrompt: digest.FirstPrompt}, digest.FirstPrompt != "", nil
}

func (Codex) Digest(sessionId string) (Digest, bool, error) {
	codexHome, err := homeDirectory("CODEX_HOME", ".codex")
	if err != nil {
		return Digest{}, false, err
	}
	transcriptPaths, _ := filepath.Glob(filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*-"+sessionId+".jsonl"))
	if len(transcriptPaths) == 0 {
		return Digest{}, false, nil
	}
	digest, err := cachedDigestOf(transcriptPaths[0], readCodexDigest)
	digest.AgentTitle = codexThreadNames()[sessionId]
	return digest, err == nil, err
}

func (Codex) PastSessions(directories []string) ([]Digest, error) {
	resolvedDirectories := map[string]bool{}
	for _, directory := range directories {
		resolvedDirectories[resolvedPath(directory)] = true
	}
	var digests []Digest
	threadNames := codexThreadNames()
	err := eachCodexSession(time.Time{}, func(filePath string, start codexSessionStart) {
		if !resolvedDirectories[resolvedPath(start.Payload.Cwd)] {
			return
		}
		if digest, err := cachedDigestOf(filePath, readCodexDigest); err == nil && digest.FirstPrompt != "" {
			digest.AgentTitle = threadNames[digest.SessionId]
			digests = append(digests, digest)
		}
	})
	return digests, err
}

func (Codex) LaptopFilePath() (string, error) {
	codexHome, err := homeDirectory("CODEX_HOME", ".codex")
	return filepath.Join(codexHome, "AGENTS.md"), err
}

func (Codex) Turns(sessionId string) ([]Turn, error) {
	codexHome, err := homeDirectory("CODEX_HOME", ".codex")
	if err != nil {
		return nil, err
	}
	transcriptPaths, _ := filepath.Glob(filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*-"+sessionId+".jsonl"))
	if len(transcriptPaths) == 0 {
		return nil, nil
	}
	var turns []Turn
	agentText := ""
	err = eachJsonLine(transcriptPaths[0], func(line json.RawMessage) bool {
		var event codexEvent
		if json.Unmarshal(line, &event) != nil || event.Payload.Type != "message" {
			return true
		}
		for _, item := range event.Payload.Content {
			if event.Payload.Role == "assistant" && strings.TrimSpace(item.Text) != "" {
				agentText = item.Text
			}
			if event.Payload.Role == "user" && isTypedPrompt(item.Text) {
				turns = append(turns, Turn{Agent: endOf(agentText, turnTextLength), User: startOf(item.Text, turnTextLength)})
				agentText = ""
				break
			}
		}
		return true
	})
	return turns, err
}

// codexThreadNameCache keeps the names from Codex's session index while the index stays unchanged.
var (
	codexThreadNameCache        map[string]string
	codexThreadNameModifiedTime time.Time
	codexThreadNameMutex        sync.Mutex
)

// codexThreadNames reads the names Codex gave its sessions. The index lists every rename in order, so the last one wins.
func codexThreadNames() map[string]string {
	codexHome, err := homeDirectory("CODEX_HOME", ".codex")
	if err != nil {
		return nil
	}
	indexPath := filepath.Join(codexHome, "session_index.jsonl")
	information, err := os.Stat(indexPath)
	if err != nil {
		return nil
	}
	codexThreadNameMutex.Lock()
	defer codexThreadNameMutex.Unlock()
	if codexThreadNameCache != nil && codexThreadNameModifiedTime.Equal(information.ModTime()) {
		return codexThreadNameCache
	}
	threadNames := map[string]string{}
	eachJsonLine(indexPath, func(line json.RawMessage) bool {
		var entry struct {
			Id         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Id != "" && entry.ThreadName != "" {
			threadNames[entry.Id] = oneLine(entry.ThreadName)
		}
		return true
	})
	codexThreadNameCache, codexThreadNameModifiedTime = threadNames, information.ModTime()
	return threadNames
}

// eachCodexSession visits every session the user started that changed since modifiedSince,
// skipping the subagents Codex spawns inside them.
func eachCodexSession(modifiedSince time.Time, visit func(filePath string, start codexSessionStart)) error {
	codexHome, err := homeDirectory("CODEX_HOME", ".codex")
	if err != nil {
		return err
	}
	return filepath.WalkDir(filepath.Join(codexHome, "sessions"), func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(filePath, ".jsonl") {
			return nil
		}
		if information, err := entry.Info(); err != nil || information.ModTime().Before(modifiedSince) {
			return nil
		}
		eachJsonLine(filePath, func(line json.RawMessage) bool {
			var start codexSessionStart
			if json.Unmarshal(line, &start) == nil && start.Type == "session_meta" && start.Payload.ThreadSource != "subagent" {
				visit(filePath, start)
			}
			return false
		})
		return nil
	})
}

// resolvedPath follows symlinks, so /tmp and /private/tmp count as the same directory.
func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func readCodexDigest(filePath string) Digest {
	digest := Digest{Tool: "codex"}
	promptIssueKey := ""
	eachJsonLine(filePath, func(line json.RawMessage) bool {
		var start codexSessionStart
		if json.Unmarshal(line, &start) == nil && start.Type == "session_meta" {
			digest.SessionId = start.Payload.Id
			digest.WorkingDirectory = start.Payload.Cwd
			digest.Branch = start.Payload.Git.Branch
			digest.StartedAt = start.Payload.Timestamp
			return true
		}
		var event codexEvent
		if json.Unmarshal(line, &event) != nil {
			return true
		}
		if !event.Timestamp.IsZero() {
			digest.LastActivityAt = event.Timestamp
		}
		switch event.Payload.Type {
		case "message":
			if event.Payload.Role == "assistant" {
				for _, item := range event.Payload.Content {
					if strings.TrimSpace(item.Text) != "" {
						digest.LastAgentMessage = endOf(item.Text, turnTextLength)
					}
				}
				return true
			}
			if event.Payload.Role != "user" {
				return true
			}
			for _, item := range event.Payload.Content {
				if isTypedPrompt(item.Text) {
					if digest.FirstPrompt == "" {
						digest.FirstPrompt = oneLine(item.Text)
						digest.StartingFiles = MarkdownFilesIn(item.Text, digest.WorkingDirectory)
					}
					digest.LastPrompt = oneLine(item.Text)
					digest.PromptCount++
					if promptIssueKey == "" {
						promptIssueKey = IssueKeyIn(item.Text)
					}
					break
				}
			}
		case "custom_tool_call", "function_call":
			call := event.Payload.Input + event.Payload.Arguments
			patched := codexPatchedFile.FindAllStringSubmatch(call, -1)
			for _, match := range patched {
				digest.TouchedFiles = appendOnce(digest.TouchedFiles, match[1])
			}
			if len(patched) == 0 {
				for _, match := range codexReadSkill.FindAllStringSubmatch(call, -1) {
					digest.UsedSkills = appendOnce(digest.UsedSkills, match[1])
				}
			}
		}
		return true
	})
	digest.settleIssueKey(promptIssueKey)
	return digest
}
