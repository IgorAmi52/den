package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

type Claude struct{}

func (Claude) Name() string {
	return "claude"
}

func (Claude) NewSessionCommand() ([]string, string) {
	sessionId := newUuid()
	return []string{"claude", "--session-id", sessionId}, sessionId
}

func (Claude) ResumeCommand(sessionId string) []string {
	return []string{"claude", "--resume", sessionId}
}

func (Claude) FindSession(sessionId string, _ string, _ time.Time) (FoundSession, bool, error) {
	configurationDirectory, err := homeDirectory("CLAUDE_CONFIG_DIR", ".claude")
	if err != nil {
		return FoundSession{}, false, err
	}
	transcriptPaths, err := filepath.Glob(filepath.Join(configurationDirectory, "projects", "*", sessionId+".jsonl"))
	if err != nil || len(transcriptPaths) == 0 {
		return FoundSession{}, false, err
	}
	firstPrompt := ""
	err = eachJsonLine(transcriptPaths[0], func(line json.RawMessage) bool {
		var event struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil || event.Type != "user" || event.IsMeta {
			return true
		}
		firstPrompt = firstTypedText(event.Message.Content)
		return firstPrompt == ""
	})
	return FoundSession{SessionId: sessionId, FirstPrompt: firstPrompt}, firstPrompt != "", err
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

func newUuid() string {
	var bytes [16]byte
	rand.Read(bytes[:])
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
