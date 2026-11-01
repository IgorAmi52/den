package brain

import (
	"encoding/json"

	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

type ProposalReviewAnswer struct {
	ReadVersion string `json:"readVersion"`
	Valid       bool   `json:"valid"`
	Reason      string `json:"reason"`
	Text        string `json:"text"`
}

func ProposalReviewRequest(repository *store.Repository, suggestion store.Suggestion, source string) helper.Request {
	proposal, _ := json.Marshal(suggestion)
	files := ContextFiles(repository)
	if suggestion.Kind == store.SuggestRule {
		files["global-rules.md"] = store.RuleFilesSnapshot(suggestion.FilePaths)
	}
	return helper.Request{
		System: cardReviewSystem + "\nRevalidate this one deferred proposal before it becomes visible. Check whether its information still holds given the latest source exchanges, current destination and inherited context, and rejection feedback. Read the destination snapshot (or the parent for a new folder); for a rule, read global-rules.md instead. Answer readVersion copied from that file, valid, reason, text. valid is false for obsolete, unsupported or already-covered information. For a whole-context rewrite (kind context with no addedLines), text must be the complete refreshed destination context preserving all unrelated current content. For every other kind, text is empty. Do not change its destination or invent new information.",
		Files:  files,
		Prompt: "DEFERRED PROPOSAL\n" + string(proposal) + ReviewContexts(repository) + "\nSAVED DEN INSTRUCTIONS\n" + instructionText(repository) + "\nLATEST SOURCE EXCHANGES\n" + source,
		Schema: objectSchema(map[string]any{"readVersion": stringSchema(), "valid": map[string]any{"type": "boolean"}, "reason": stringSchema(), "text": stringSchema()}),
		Tier:   helper.Reasoning,
	}
}

func instructionText(repository *store.Repository) string {
	text, _ := repository.ReadInstructions()
	return text
}
