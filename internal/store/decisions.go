package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// Decision is one thing den proposed, applied, or the user answered, kept as evidence den can learn from later.
type Decision struct {
	At        time.Time `json:"at"`
	Job       string    `json:"job"`
	SessionId string    `json:"sessionId,omitempty"`
	FolderId  string    `json:"folderId,omitempty"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail,omitempty"`
	Outcome   string    `json:"outcome"`
	// CallId is the model call that made what this decision is about, so an outcome can be traced back to its prompt.
	CallId string `json:"callId,omitempty"`
	// Before and After are a file's whole text around an edit the user made by hand.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Call is one model call den made, kept whole so den's judgment can be evaluated and the call replayed later,
// on another model or another version of the prompt.
type Call struct {
	Id        string    `json:"id"`
	At        time.Time `json:"at"`
	Job       string    `json:"job"`
	SessionId string    `json:"sessionId,omitempty"`
	FolderId  string    `json:"folderId,omitempty"`
	Model     string    `json:"model"`
	// PromptVersion is a short hash of the system prompt, so each call ties to the version of the prompt that made it.
	PromptVersion string `json:"promptVersion"`
	Prompt        string `json:"prompt"`
	// Files are the files the call could read, by name, so a replay gives the model the same ones.
	Files map[string]string `json:"files,omitempty"`
	// DenVersion is the commit den was built from, since the code builds most of the prompt.
	DenVersion string          `json:"denVersion,omitempty"`
	Answer     json.RawMessage `json:"answer,omitempty"`
	Error      string          `json:"error,omitempty"`
	// SlimFailure is why a slim codex call failed before den made it again the usual way.
	SlimFailure  string  `json:"slimFailure,omitempty"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CostDollars  float64 `json:"costDollars"`
	Milliseconds int64   `json:"milliseconds"`
}

// LogDecision appends to decisions.jsonl beside den.json.
func (repository Repository) LogDecision(decision Decision) error {
	if decision.At.IsZero() {
		decision.At = time.Now()
	}
	return repository.appendLine("decisions.jsonl", decision)
}

// LogCall appends to calls.jsonl beside den.json, and returns the id it gave the call.
func (repository Repository) LogCall(call Call) (string, error) {
	if call.At.IsZero() {
		call.At = time.Now()
	}
	call.Id, call.DenVersion = newId(), DenVersion()
	return call.Id, repository.appendLine("calls.jsonl", call)
}

// DenVersion is the commit den was built from, marked dirty when it had uncommitted changes, or "unknown".
func DenVersion() string {
	information, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	revision, modified := "", false
	for _, setting := range information.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "unknown"
	}
	if modified {
		return revision[:min(len(revision), 12)] + "-dirty"
	}
	return revision[:min(len(revision), 12)]
}

// KeepSystemPrompt saves a system prompt once per version, in prompts beside den.json, since calls only name the version.
func (repository Repository) KeepSystemPrompt(version string, system string) error {
	directory, err := repository.Directory()
	if err != nil {
		return err
	}
	promptPath := filepath.Join(directory, "prompts", version+".txt")
	if _, err := os.Stat(promptPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(promptPath, []byte(system), 0o644)
}

func (repository Repository) appendLine(fileName string, value any) error {
	directory, err := repository.Directory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, fileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	return err
}
