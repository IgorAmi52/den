package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Adapter interface {
	Name() string
	// NewSessionCommand returns the launch command and the session id, when the tool lets den choose it.
	NewSessionCommand() (command []string, sessionId string)
	ResumeCommand(sessionId string) []string
	// FindSession reports the session once its first prompt is saved; found stays false until then.
	// Tools that don't let den choose the id find it by working directory and launch time instead.
	FindSession(sessionId string, workingDirectory string, launchedAt time.Time) (foundSession FoundSession, found bool, err error)
}

type FoundSession struct {
	SessionId   string
	FirstPrompt string
}

var supportedAdapters = []Adapter{Claude{}, Codex{}}

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
	return trimmedText != "" && !strings.HasPrefix(trimmedText, "<") && !strings.HasPrefix(trimmedText, "# AGENTS.md")
}
