package brain

import "github.com/IgorAmi52/den/internal/store"

type ChatMessage struct {
	FromDen bool
	Text    string
}

// Reply answers the user in the chat. den has no model connected yet, so it says so
// and points at what works by hand. A model-backed reply will answer from the sessions and folder notes,
// and turn requests like "put it in Agent API instead" into suggestions for the review queue.
func Reply(conversation []ChatMessage, about *store.Suggestion) ChatMessage {
	text := "I can't answer yet: my model isn't connected. Once it is, I'll answer from your sessions and folder notes, " +
		"and turn what you ask for into suggestions you confirm. I never change anything on my own."
	if about != nil {
		text += "\n\nFor this suggestion, press esc and then e to change it by hand, or n to reject it so I don't offer it again."
	}
	return ChatMessage{FromDen: true, Text: text}
}
