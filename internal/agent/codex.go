package agent

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// Codex writes a session file on the first prompt, and its first line records the launch time and working directory.
// A session launched by den is the one whose recorded launch time is closest to den's.
const codexLaunchTimeTolerance = 10 * time.Second

type Codex struct{}

type codexSessionStart struct {
	Type    string `json:"type"`
	Payload struct {
		Id        string    `json:"id"`
		Timestamp time.Time `json:"timestamp"`
		Cwd       string    `json:"cwd"`
	} `json:"payload"`
}

func (Codex) Name() string {
	return "codex"
}

func (Codex) NewSessionCommand() ([]string, string) {
	return []string{"codex"}, ""
}

func (Codex) ResumeCommand(sessionId string) []string {
	return []string{"codex", "resume", sessionId}
}

func (Codex) FindSession(_ string, workingDirectory string, launchedAt time.Time) (FoundSession, bool, error) {
	codexHome, err := homeDirectory("CODEX_HOME", ".codex")
	if err != nil {
		return FoundSession{}, false, err
	}
	resolvedWorkingDirectory := resolvedPath(workingDirectory)
	closestFilePath, closestSessionId := "", ""
	closestDistance := codexLaunchTimeTolerance
	err = filepath.WalkDir(filepath.Join(codexHome, "sessions"), func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(filePath, ".jsonl") {
			return nil
		}
		if information, err := entry.Info(); err != nil || information.ModTime().Before(launchedAt) {
			return nil
		}
		eachJsonLine(filePath, func(line json.RawMessage) bool {
			var start codexSessionStart
			if json.Unmarshal(line, &start) == nil && start.Type == "session_meta" && resolvedPath(start.Payload.Cwd) == resolvedWorkingDirectory {
				distance := start.Payload.Timestamp.Sub(launchedAt).Abs()
				if distance <= closestDistance {
					closestFilePath, closestSessionId, closestDistance = filePath, start.Payload.Id, distance
				}
			}
			return false
		})
		return nil
	})
	if err != nil || closestFilePath == "" {
		return FoundSession{}, false, err
	}
	firstPrompt := codexFirstPrompt(closestFilePath)
	return FoundSession{SessionId: closestSessionId, FirstPrompt: firstPrompt}, firstPrompt != "", nil
}

// resolvedPath follows symlinks, so /tmp and /private/tmp count as the same directory.
func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func codexFirstPrompt(filePath string) string {
	firstPrompt := ""
	eachJsonLine(filePath, func(line json.RawMessage) bool {
		var event struct {
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &event) != nil || event.Payload.Type != "message" || event.Payload.Role != "user" {
			return true
		}
		for _, item := range event.Payload.Content {
			if isTypedPrompt(item.Text) {
				firstPrompt = item.Text
				return false
			}
		}
		return true
	})
	return firstPrompt
}
