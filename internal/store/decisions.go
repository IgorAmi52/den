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

// LogDecision appends to decisions.jsonl beside den.json.
func (repository Repository) LogDecision(decision Decision) error {
	directory, err := repository.Directory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if decision.At.IsZero() {
		decision.At = time.Now()
	}
	line, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, "decisions.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	return err
}
