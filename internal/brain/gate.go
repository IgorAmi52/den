package brain

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

// ReplyVerdict is what den's local check decides about an agent's reply, with no model call.
type ReplyVerdict int

const (
	// AskGate leaves it to the gate check.
	AskGate ReplyVerdict = iota
	ReviewNow
)

// ReplyCheck is the exchange that just ended: the user's prompt, the end of the agent's reply, and what the agent did in between.
type ReplyCheck struct {
	PlanApproved    bool
	MarkdownWritten bool
}

// CheckReply is the local check every agent reply gets. The first rule that matches decides; rule names what decided, for the log.
func CheckReply(check ReplyCheck) (verdict ReplyVerdict, rule string) {
	switch {
	case check.PlanApproved:
		return ReviewNow, "plan approved"
	case check.MarkdownWritten:
		return ReviewNow, "markdown written"
	}
	return AskGate, "for the gate check"
}

// Exchange is one prompt and the agent's reply to it, numbered from the start of the session.
type Exchange struct {
	Number int
	Prompt string
	Reply  string
}

// GateInput is what the gate check reads: the exchanges since the last check, and what the session already has.
type GateInput struct {
	Session    store.Session
	FolderName string
	Exchanges  []Exchange
	// WaitingCards are the session's cards still waiting for the user, one line each.
	WaitingCards []string
}

// GateAnswer says whether the card review should look at the session now.
type GateAnswer struct {
	WorthReview bool   `json:"worthReview"`
	Kind        string `json:"kind"`
	Reason      string `json:"reason"`
}

// gateSystem is preserved for experiments/gate/label.py to replay the original Luna baseline.
const gateSystem = reviewIntroduction + "After an agent replies, you decide whether the exchanges marked new are worth a closer look by den's " +
	"card review, which may then propose keeping something. Ask: would a future session make a worse decision or repeat meaningful " +
	"work without this information, and is it missing from the context already available? Look for concrete evidence of future use " +
	"by a person or agent, not whether the user will approve a card. The card review checks the existing context for duplication. " +
	"What can be worth keeping:\n" +
	"- notes: in a session in a folder, a finding with its cause, a decision the user made or confirmed, or a problem left open, " +
	"that whoever continues the work needs; useful rationale, tradeoffs, rejected alternatives and constraints still count after " +
	"implementation, even when the code already shows the resulting behavior\n" +
	"- folder: work that will clearly go on in more sessions, like a feature, an investigation or a plan\n" +
	"- skill: a way of working the user set up and will want again, like research without code changes or a review checklist\n" +
	"- rule: a clear correction or preference about how agents should work, in any repo, like \"never add comments\"\n" +
	"- withdraw: the new exchanges undo, fix or contradict a waiting card, so the card review can take it back\n" +
	"Most exchanges are routine work, a question answered or a small fix, and nothing in them outlives the session: answer no. " +
	"Answer no as well when a waiting card already covers it. " +
	"worthReview: true only when one of these happened in the new exchanges. kind: the one it is, or none. " +
	"reason: at most 12 words on what happened, or \"\"."

// endOfText keeps the end of a text, where a reply says what it concluded.
func endOfText(text string, length int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= length {
		return string(runes)
	}
	return "…" + string(runes[len(runes)-length+1:])
}

// WroteMarkdown tells whether a reply wrote a markdown file of the user's: not den's own notes, an agent's memories or its plan,
// which plan approval already covers. Edited files are as the transcript names them, relative to workingDirectory or absolute.
func WroteMarkdown(workingDirectory string, editedFiles []string) bool {
	dataDirectory, _ := store.DataDirectory()
	for _, editedFile := range editedFiles {
		filePath := absoluteIn(workingDirectory, editedFile)
		directories := strings.Split(filepath.Dir(filePath), string(filepath.Separator))
		switch {
		case !strings.EqualFold(filepath.Ext(filePath), ".md"):
		case slices.Contains(directories, "memory"), slices.Contains(directories, "plans"):
		case dataDirectory != "" && strings.HasPrefix(filePath, dataDirectory):
		default:
			return true
		}
	}
	return false
}
