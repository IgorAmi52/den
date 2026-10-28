package brain

import (
	"encoding/json"

	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

type ProposalReviewAnswer struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason"`
	Text   string `json:"text"`
}

func ProposalReviewRequest(repository *store.Repository, suggestion store.Suggestion, source string) helper.Request {
	proposal, _ := json.Marshal(suggestion)
	return helper.Request{
		System: cardReviewSystem + "\nRevalidate this one deferred proposal before it becomes visible. Check whether its information still holds given the latest source exchanges, current destination and inherited context, and rejection feedback. Answer valid, reason, text. valid is false for obsolete, unsupported or already-covered information. For a whole-context rewrite (kind context with no addedLines), text must be the complete refreshed destination context preserving all unrelated current content. For every other kind, text is empty. Do not change its destination or invent new information.",
		Prompt: "DEFERRED PROPOSAL\n" + string(proposal) + ReviewContexts(repository) + "\nLATEST SOURCE EXCHANGES\n" + source,
		Schema: objectSchema(map[string]any{"valid": map[string]any{"type": "boolean"}, "reason": stringSchema(), "text": stringSchema()}),
		Tier:   helper.Reasoning,
	}
}
