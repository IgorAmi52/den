package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// copilotEditingTools are the tools whose path argument means the agent changed that file.
// apply_patch, which Copilot uses with GPT models, takes a patch as one string instead.
var copilotEditingTools = map[string]bool{"edit": true, "create": true}

// copilotInstructionsVariable lists directories Copilot loads extra instructions from. Copilot ignores an AGENTS.md there,
// so den's context goes in a .github/instructions file that applies to every file.
const copilotInstructionsVariable = "COPILOT_CUSTOM_INSTRUCTIONS_DIRS"

type Copilot struct{}

// copilotEvent is one line of a session's events.jsonl. Events from Copilot's subagents carry an agentId.
type copilotEvent struct {
	Type      string    `json:"type"`
	AgentId   string    `json:"agentId"`
	Timestamp time.Time `json:"timestamp"`
	Data      struct {
		SessionId string    `json:"sessionId"`
		StartTime time.Time `json:"startTime"`
		Context   struct {
			Cwd    string `json:"cwd"`
			Branch string `json:"branch"`
		} `json:"context"`
		Content string `json:"content"`
		// Source is set on prompts Copilot injects, like a skill's, and IsAutopilotContinuation on its own follow-ups.
		Source                  string          `json:"source"`
		IsAutopilotContinuation bool            `json:"isAutopilotContinuation"`
		Title                   string          `json:"title"`
		ToolName                string          `json:"toolName"`
		Arguments               json.RawMessage `json:"arguments"`
	} `json:"data"`
}

func (Copilot) Name() string {
	return "copilot"
}

func (Copilot) NewSessionCommand(launch Launch) ([]string, string) {
	sessionId := newUuid()
	return copilotCommand(sessionId, launch, "--session-id", sessionId), sessionId
}

func (Copilot) ResumeCommand(sessionId string, launch Launch) []string {
	return copilotCommand(sessionId, launch, "--resume="+sessionId)
}

// copilotCommand passes the context through an instructions file of the session's, since Copilot has no switch for it.
// Copilot has no switch for hooks either, so the pane gets no status and den goes by the transcript, as for agents started elsewhere.
func copilotCommand(sessionId string, launch Launch, sessionArguments ...string) []string {
	command := append([]string{"copilot"}, sessionArguments...)
	if launch.Context == "" {
		return command
	}
	for _, directory := range launch.WritableDirectories {
		command = append(command, "--add-dir="+directory)
	}
	instructionsDirectory, err := writeCopilotContext(sessionId, launch.Context)
	if err != nil {
		return command
	}
	if existing := os.Getenv(copilotInstructionsVariable); existing != "" {
		instructionsDirectory = existing + "," + instructionsDirectory
	}
	return append([]string{"env", copilotInstructionsVariable + "=" + instructionsDirectory}, command...)
}

// writeCopilotContext writes the context where Copilot finds it through copilotInstructionsVariable, one directory per session.
func writeCopilotContext(sessionId string, context string) (string, error) {
	denHome, err := homeDirectory("DEN_HOME", ".den")
	if err != nil {
		return "", err
	}
	instructionsDirectory := filepath.Join(denHome, "copilot-context", sessionId)
	filePath := filepath.Join(instructionsDirectory, ".github", "instructions", "den.instructions.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return "", err
	}
	text := "---\napplyTo: \"**\"\n---\n\n" + context
	return instructionsDirectory, os.WriteFile(filePath, []byte(text), 0o644)
}

func (Copilot) FindSession(sessionId string, _ string, _ time.Time) (FoundSession, bool, error) {
	transcriptPath, found, err := Copilot{}.TranscriptPath(sessionId)
	if err != nil || !found {
		return FoundSession{}, false, err
	}
	firstPrompt := ""
	err = eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
		var event copilotEvent
		if json.Unmarshal(line, &event) == nil && isCopilotTypedPrompt(event) {
			firstPrompt = event.Data.Content
		}
		return firstPrompt == ""
	})
	return FoundSession{SessionId: sessionId, FirstPrompt: oneLine(firstPrompt)}, firstPrompt != "", err
}

func (Copilot) Digest(sessionId string) (Digest, bool, error) {
	transcriptPath, found, err := Copilot{}.TranscriptPath(sessionId)
	if err != nil || !found {
		return Digest{}, false, err
	}
	digest, err := cachedDigestOf(transcriptPath, readCopilotDigest)
	return digest, err == nil, err
}

// PastSessions reads only the start of each session to see where it ran, since Copilot keeps every repo's sessions together,
// den's own model calls among them.
func (Copilot) PastSessions(directories []string) ([]Digest, error) {
	sessionDirectory, err := copilotSessionDirectory()
	if err != nil {
		return nil, err
	}
	resolvedDirectories := map[string]bool{}
	for _, directory := range directories {
		resolvedDirectories[resolvedPath(directory)] = true
	}
	transcriptPaths, _ := filepath.Glob(filepath.Join(sessionDirectory, "*", "events.jsonl"))
	var digests []Digest
	for _, transcriptPath := range transcriptPaths {
		workingDirectory := ""
		eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
			var event copilotEvent
			if json.Unmarshal(line, &event) == nil && event.Type == "session.start" {
				workingDirectory = event.Data.Context.Cwd
				return false
			}
			return true
		})
		if workingDirectory == "" || !resolvedDirectories[resolvedPath(workingDirectory)] {
			continue
		}
		if digest, err := cachedDigestOf(transcriptPath, readCopilotDigest); err == nil && digest.FirstPrompt != "" {
			digests = append(digests, digest)
		}
	}
	return digests, nil
}

func (Copilot) LaptopFilePath() (string, error) {
	copilotHome, err := homeDirectory("COPILOT_HOME", ".copilot")
	return filepath.Join(copilotHome, "copilot-instructions.md"), err
}

func (Copilot) Turns(sessionId string) ([]Turn, error) {
	transcriptPath, found, err := Copilot{}.TranscriptPath(sessionId)
	if err != nil || !found {
		return nil, err
	}
	return cachedTurnsOf(transcriptPath, readCopilotTurns)
}

// TranscriptPath finds the session's events.jsonl, which a session that never got a prompt may not have.
func (Copilot) TranscriptPath(sessionId string) (string, bool, error) {
	sessionDirectory, err := copilotSessionDirectory()
	if err != nil {
		return "", false, err
	}
	transcriptPath := filepath.Join(sessionDirectory, sessionId, "events.jsonl")
	if _, err := os.Stat(transcriptPath); err != nil {
		return "", false, nil
	}
	return transcriptPath, true, nil
}

func copilotSessionDirectory() (string, error) {
	copilotHome, err := homeDirectory("COPILOT_HOME", ".copilot")
	return filepath.Join(copilotHome, "session-state"), err
}

// isCopilotTypedPrompt keeps the prompts the user typed to the main agent, leaving out what Copilot injects.
func isCopilotTypedPrompt(event copilotEvent) bool {
	return event.Type == "user.message" && event.AgentId == "" && event.Data.Source == "" &&
		!event.Data.IsAutopilotContinuation && isTypedPrompt(event.Data.Content)
}

func readCopilotTurns(transcriptPath string) ([]Turn, error) {
	var turns []Turn
	agentText := ""
	err := eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
		var event copilotEvent
		if json.Unmarshal(line, &event) != nil || event.AgentId != "" {
			return true
		}
		switch {
		case event.Type == "assistant.message" && strings.TrimSpace(event.Data.Content) != "":
			agentText = event.Data.Content
			if len(turns) > 0 {
				turns[len(turns)-1].Reply += event.Data.Content + "\n\n"
			}
		case isCopilotTypedPrompt(event):
			turns = append(turns, Turn{Agent: agentText, User: event.Data.Content})
			agentText = ""
		}
		return true
	})
	return turns, err
}

func readCopilotDigest(transcriptPath string) Digest {
	digest := Digest{Tool: "copilot", SessionId: filepath.Base(filepath.Dir(transcriptPath))}
	promptIssueKey := ""
	eachJsonLine(transcriptPath, func(line json.RawMessage) bool {
		var event copilotEvent
		if json.Unmarshal(line, &event) != nil || event.AgentId != "" {
			return true
		}
		if !event.Timestamp.IsZero() {
			digest.LastActivityAt = event.Timestamp
		}
		switch event.Type {
		case "session.start", "session.resume":
			if event.Type == "session.start" {
				digest.StartedAt = event.Data.StartTime
			}
			if event.Data.Context.Cwd != "" {
				digest.WorkingDirectory = event.Data.Context.Cwd
			}
			if event.Data.Context.Branch != "" {
				digest.Branch = event.Data.Context.Branch
			}
		case "session.title_changed":
			if title := oneLine(event.Data.Title); title != "" {
				digest.AgentTitle = title
			}
		case "user.message":
			if !isCopilotTypedPrompt(event) {
				return true
			}
			prompt := event.Data.Content
			if digest.FirstPrompt == "" {
				digest.FirstPrompt = oneLine(prompt)
				digest.StartingFiles = MarkdownFilesIn(prompt, digest.WorkingDirectory)
			}
			digest.LastPrompt = oneLine(prompt)
			digest.PromptCount++
			if promptIssueKey == "" {
				promptIssueKey = IssueKeyIn(prompt)
			}
		case "assistant.message":
			if strings.TrimSpace(event.Data.Content) != "" {
				digest.LastAgentMessage = endOf(event.Data.Content, turnTextLength)
			}
		case "tool.execution_start":
			for _, filePath := range copilotEditedFiles(event.Data.ToolName, event.Data.Arguments) {
				digest.TouchedFiles = appendOnce(digest.TouchedFiles, filePath)
			}
		}
		return true
	})
	digest.settleIssueKey(promptIssueKey)
	return digest
}

// copilotEditedFiles reads the files a tool call changes: the path of an edit or create, or the files an apply_patch names.
func copilotEditedFiles(toolName string, arguments json.RawMessage) []string {
	if toolName == "apply_patch" {
		var patch string
		if json.Unmarshal(arguments, &patch) != nil {
			return nil
		}
		var filePaths []string
		for _, match := range codexPatchedFile.FindAllStringSubmatch(patch, -1) {
			filePaths = append(filePaths, match[1])
		}
		return filePaths
	}
	if !copilotEditingTools[toolName] {
		return nil
	}
	var parsed struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(arguments, &parsed) != nil || parsed.Path == "" {
		return nil
	}
	return []string{parsed.Path}
}
