package brain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	// FirstRunSessions is how many of the newest past sessions the first run looks at.
	FirstRunSessions = 10
	// workedPrompts is how many prompts make a session real work when there's no model to judge it.
	workedPrompts = 3
)

const (
	VerdictContinue = "continue"
	VerdictKeep     = "keep"
	VerdictDone     = "done"
)

// Arrival is one past session as the first run sees it: whether to bring it in, under which name, and why.
type Arrival struct {
	Digest  agent.Digest
	Verdict string
	Title   string
	Why     string
}

// NewestSessions are the most recently used sessions, newest first, at most FirstRunSessions.
func NewestSessions(digests []agent.Digest) []agent.Digest {
	newest := slices.Clone(digests)
	slices.SortFunc(newest, func(first agent.Digest, second agent.Digest) int {
		return second.LastActivityAt.Compare(first.LastActivityAt)
	})
	return newest[:min(len(newest), FirstRunSessions)]
}

// ArrivalsWithoutModel brings in the sessions that did real work: a few prompts, or edited files.
func ArrivalsWithoutModel(digests []agent.Digest) []Arrival {
	var arrivals []Arrival
	for _, digest := range digests {
		arrival := Arrival{Digest: digest, Verdict: VerdictDone, Title: fallbackTitle(digest)}
		if digest.PromptCount >= workedPrompts || len(digest.TouchedFiles) > 0 {
			arrival.Verdict, arrival.Why = VerdictContinue, fmt.Sprintf("%d prompts, %d files edited", digest.PromptCount, len(digest.TouchedFiles))
		}
		arrivals = append(arrivals, arrival)
	}
	return arrivals
}

// ArrivalsFromModel reads the model's verdicts on the digests, sent in order as s1, s2 and so on.
// A session the model skipped counts as done; what it wants to keep is checked like any review's.
func ArrivalsFromModel(answer FirstRunAnswer, digests []agent.Digest, repository *store.Repository) []Arrival {
	verdicts := map[int]FirstRunVerdict{}
	for _, verdict := range answer.Sessions {
		var number int
		if _, err := fmt.Sscanf(strings.TrimSpace(verdict.Session), "s%d", &number); err == nil && number >= 1 && number <= len(digests) {
			verdicts[number-1] = verdict
		}
	}
	var arrivals []Arrival
	for index, digest := range digests {
		verdict := verdicts[index]
		arrival := Arrival{Digest: digest, Verdict: VerdictDone, Title: strings.TrimSpace(verdict.Title), Why: strings.TrimSpace(verdict.Why)}
		if arrival.Title == "" {
			arrival.Title = fallbackTitle(digest)
		}
		if verdict.Verdict == VerdictContinue || verdict.Verdict == VerdictKeep {
			arrival.Verdict = verdict.Verdict
		}
		arrivals = append(arrivals, arrival)
	}
	return arrivals
}

// fallbackTitle is the agent's own name for the session, or else its first prompt.
func fallbackTitle(digest agent.Digest) string {
	if digest.AgentTitle != "" {
		return digest.AgentTitle
	}
	return truncateText(digest.FirstPrompt, 120)
}
