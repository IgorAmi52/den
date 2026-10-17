package brain

import (
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// retitleEvery is how many user prompts pass between two looks at a session's name.
const retitleEvery = 3

// Retitle tells whether a session's name is due for a look: after its first prompt, then every few prompts.
// title is the better name, or empty to keep the current one, which is the normal answer.
// Until a model is connected, the better name is the one the agent itself gave the session.
func Retitle(session store.Session, digest agent.Digest) (title string, due bool) {
	if session.TitlePinned || session.Archived || digest.PromptCount == 0 {
		return "", false
	}
	if session.TitledAtPrompt > 0 && digest.PromptCount-session.TitledAtPrompt < retitleEvery {
		return "", false
	}
	candidate := strings.TrimSpace(digest.AgentTitle)
	if candidate == "" {
		return "", false
	}
	if strings.EqualFold(candidate, session.Title) {
		return "", true
	}
	return candidate, true
}
