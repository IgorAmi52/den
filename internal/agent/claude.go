package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// claudeEditingTools are the tools whose file_path input means the agent changed that file.
var claudeEditingTools = map[string]bool{"Edit": true, "MultiEdit": true, "Write": true, "NotebookEdit": true}

// claudeProjectNameCharacters are replaced with "-" when Claude names a project directory after a working directory.
var claudeProjectNameCharacters = regexp.MustCompile(`[^a-zA-Z0-9]`)

type Claude struct{}

type claudeEvent struct {
	Type        string    `json:"type"`
	AiTitle     string    `json:"aiTitle"`
	IsMeta      bool      `json:"isMeta"`
	IsSidechain bool      `json:"isSidechain"`
	Cwd         string    `json:"cwd"`
	GitBranch   string    `json:"gitBranch"`
	Timestamp   time.Time `json:"timestamp"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func (Claude) Name() string {
	return "claude"
}

func (Claude) NewSessionCommand(launch Launch) ([]string, string) {
	sessionId := newUuid()
	return append([]string{"claude", "--session-id", sessionId}, claudeLaunchArguments(launch)...), sessionId
}

func (Claude) ResumeCommand(sessionId string, launch Launch) []string {
	return append([]string{"claude", "--resume", sessionId}, claudeLaunchArguments(launch)...)
}

// claudeLaunchArguments add the context and the status hooks through flags,
// so den never edits the user's Claude settings.
func claudeLaunchArguments(launch Launch) []string {
	var arguments []string
	if launch.Context != "" {
		arguments = append(arguments, "--append-system-prompt", launch.Context)
		for _, directory := range launch.WritableDirectories {
			arguments = append(arguments, "--add-dir", directory)
		}
	}
	if len(launch.StatusCommand) > 0 {
		statusHook := func(state string) []map[string]any {
			command := shellJoin(launch.StatusCommand) + " " + state
			return []map[string]any{{"hooks": []map[string]string{{"type": "command", "command": command}}}}
		}
		settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{
			"UserPromptSubmit": statusHook("running"),
			"PostToolUse":      statusHook("running"),
			"Notification":     statusHook("waiting"),
			"Stop":             statusHook("waiting"),
		}})
		arguments = append(arguments, "--settings", string(settings))
	}
	return arguments
}

func (Claude) FindSession(sessionId string, _ string, _ time.Time) (FoundSession, bool, error) {
	transcriptPath, found, err := claudeTranscriptPath(sessionId)
	if err != nil || !found {
		return FoundSession{}, false, err
	}
	firstPrompt := ""
	err = eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
		var event claudeEvent
		if json.Unmarshal(line, &event) != nil || event.Type != "user" || event.IsMeta {
			return true
		}
		firstPrompt = firstTypedText(event.Message.Content)
		return firstPrompt == ""
	})
	return FoundSession{SessionId: sessionId, FirstPrompt: firstPrompt}, firstPrompt != "", err
}

func (Claude) Digest(sessionId string) (Digest, bool, error) {
	transcriptPath, found, err := claudeTranscriptPath(sessionId)
	if err != nil || !found {
		return Digest{}, false, err
	}
	digest, err := cachedDigestOf(transcriptPath, readClaudeDigest)
	return digest, err == nil, err
}

func (Claude) PastSessions(directories []string) ([]Digest, error) {
	configurationDirectory, err := homeDirectory("CLAUDE_CONFIG_DIR", ".claude")
	if err != nil {
		return nil, err
	}
	var digests []Digest
	for _, directory := range directories {
		projectDirectory := filepath.Join(configurationDirectory, "projects", claudeProjectNameCharacters.ReplaceAllString(directory, "-"))
		transcriptPaths, _ := filepath.Glob(filepath.Join(projectDirectory, "*.jsonl"))
		for _, transcriptPath := range transcriptPaths {
			digest, err := cachedDigestOf(transcriptPath, readClaudeDigest)
			if err == nil && digest.FirstPrompt != "" {
				digests = append(digests, digest)
			}
		}
	}
	return digests, nil
}

func (Claude) LaptopFilePath() (string, error) {
	configurationDirectory, err := homeDirectory("CLAUDE_CONFIG_DIR", ".claude")
	return filepath.Join(configurationDirectory, "CLAUDE.md"), err
}

func claudeTranscriptPath(sessionId string) (string, bool, error) {
	configurationDirectory, err := homeDirectory("CLAUDE_CONFIG_DIR", ".claude")
	if err != nil {
		return "", false, err
	}
	transcriptPaths, err := filepath.Glob(filepath.Join(configurationDirectory, "projects", "*", sessionId+".jsonl"))
	if err != nil || len(transcriptPaths) == 0 {
		return "", false, err
	}
	return transcriptPaths[0], true, nil
}

func readClaudeDigest(transcriptPath string) Digest {
	digest := Digest{Tool: "claude", SessionId: strings.TrimSuffix(filepath.Base(transcriptPath), ".jsonl")}
	promptIssueKey := ""
	// proposedPlans waits for the answer to each plan, by tool call, since only an approved plan counts.
	proposedPlans := map[string]string{}
	eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
		var event claudeEvent
		if json.Unmarshal(line, &event) != nil || event.IsSidechain {
			return true
		}
		if event.Cwd != "" {
			digest.WorkingDirectory = event.Cwd
		}
		if event.GitBranch != "" && event.GitBranch != "HEAD" {
			digest.Branch = event.GitBranch
		}
		if !event.Timestamp.IsZero() {
			if digest.StartedAt.IsZero() {
				digest.StartedAt = event.Timestamp
			}
			digest.LastActivityAt = event.Timestamp
		}
		switch event.Type {
		case "ai-title":
			if event.AiTitle != "" {
				digest.AgentTitle = oneLine(event.AiTitle)
			}
		case "user":
			if prompt := firstTypedText(event.Message.Content); prompt != "" && !event.IsMeta {
				if digest.FirstPrompt == "" {
					digest.FirstPrompt = oneLine(prompt)
					digest.StartingFiles = MarkdownFilesIn(prompt, digest.WorkingDirectory)
				}
				digest.LastPrompt = oneLine(prompt)
				digest.PromptCount++
				if promptIssueKey == "" {
					promptIssueKey = IssueKeyIn(prompt)
				}
			}
			for _, block := range claudeBlocks(event.Message.Content) {
				if plan, proposed := proposedPlans[block.ToolUseId]; proposed && block.Type == "tool_result" {
					if !block.IsError {
						digest.Plan = plan
					}
					delete(proposedPlans, block.ToolUseId)
				}
			}
			digest.notePullRequests(line)
		case "assistant":
			for _, block := range claudeBlocks(event.Message.Content) {
				if block.Type != "tool_use" {
					continue
				}
				if claudeEditingTools[block.Name] && block.Input.FilePath != "" {
					digest.TouchedFiles = appendOnce(digest.TouchedFiles, block.Input.FilePath)
				}
				if block.Name == "ExitPlanMode" && strings.TrimSpace(block.Input.Plan) != "" {
					proposedPlans[block.Id] = block.Input.Plan
				}
			}
			digest.notePullRequests(line)
		}
		return true
	})
	digest.settleIssueKey(promptIssueKey)
	return digest
}

func (Claude) Turns(sessionId string) ([]Turn, error) {
	transcriptPath, found, err := claudeTranscriptPath(sessionId)
	if err != nil || !found {
		return nil, err
	}
	var turns []Turn
	agentText := ""
	err = eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
		var event claudeEvent
		if json.Unmarshal(line, &event) != nil || event.IsSidechain {
			return true
		}
		switch event.Type {
		case "assistant":
			for _, block := range claudeBlocks(event.Message.Content) {
				if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
					agentText = block.Text
				}
			}
		case "user":
			if prompt := firstTypedText(event.Message.Content); prompt != "" && !event.IsMeta {
				turns = append(turns, Turn{Agent: endOf(agentText, turnTextLength), User: startOf(prompt, turnTextLength)})
				agentText = ""
			}
		}
		return true
	})
	return turns, err
}

type claudeBlock struct {
	Type      string `json:"type"`
	Id        string `json:"id"`
	Name      string `json:"name"`
	ToolUseId string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
	Text      string `json:"text"`
	Input     struct {
		FilePath string `json:"file_path"`
		Plan     string `json:"plan"`
	} `json:"input"`
}

func claudeBlocks(content json.RawMessage) []claudeBlock {
	var blocks []claudeBlock
	json.Unmarshal(content, &blocks)
	return blocks
}

// firstTypedText reads a message content that is either plain text or a list of content blocks.
func firstTypedText(content json.RawMessage) string {
	var plainText string
	if json.Unmarshal(content, &plainText) == nil {
		if isTypedPrompt(plainText) {
			return plainText
		}
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(content, &blocks)
	for _, block := range blocks {
		if block.Type == "text" && isTypedPrompt(block.Text) {
			return block.Text
		}
	}
	return ""
}

// shellJoin quotes each argument for sh, since Claude runs hook commands through a shell.
func shellJoin(arguments []string) string {
	var quoted []string
	for _, argument := range arguments {
		quoted = append(quoted, "'"+strings.ReplaceAll(argument, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " ")
}

func newUuid() string {
	var bytes [16]byte
	rand.Read(bytes[:])
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
