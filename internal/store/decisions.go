package store

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

// Call is one model call den made, kept whole so den's judgment can be evaluated and the call replayed later,
// on another model or another version of the prompt.
type Call struct {
	At        time.Time `json:"at"`
	Job       string    `json:"job"`
	SessionId string    `json:"sessionId,omitempty"`
	FolderId  string    `json:"folderId,omitempty"`
	Model     string    `json:"model"`
	// PromptVersion is a short hash of the system prompt, so each call ties to the version of the prompt that made it.
	PromptVersion string          `json:"promptVersion"`
	Prompt        string          `json:"prompt"`
	Answer        json.RawMessage `json:"answer,omitempty"`
	Error         string          `json:"error,omitempty"`
	InputTokens   int             `json:"inputTokens"`
	OutputTokens  int             `json:"outputTokens"`
	CostDollars   float64         `json:"costDollars"`
	Milliseconds  int64           `json:"milliseconds"`
}

// LogDecision appends to decisions.jsonl beside den.json.
func (repository Repository) LogDecision(decision Decision) error {
	if decision.At.IsZero() {
		decision.At = time.Now()
	}
	return repository.appendLine("decisions.jsonl", decision)
}

// LogCall appends to calls.jsonl beside den.json.
func (repository Repository) LogCall(call Call) error {
	if call.At.IsZero() {
		call.At = time.Now()
	}
	return repository.appendLine("calls.jsonl", call)
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
