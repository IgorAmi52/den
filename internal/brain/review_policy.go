package brain

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

//go:embed card_review.md
var cardReviewSystem string

func ReviewContexts(repository *store.Repository) string {
	var prompt strings.Builder
	prompt.WriteString("\nDESTINATIONS (children inherit ancestors; identifiers are stable)\n")
	fmt.Fprintf(&prompt, "destination: repo\ncontext:\n%s\n", orNone(repository.ReadContext("")))
	for _, folder := range repository.Folders {
		parent := folder.ParentId
		if parent == "" {
			parent = "repo"
		}
		fmt.Fprintf(&prompt, "\ndestination: %s\npath: %s\nparent: %s\ncontext:\n%s\n", folder.Id, repository.ContextDestination(folder.Id), parent, orNone(repository.ReadContext(folder.Id)))
	}
	prompt.WriteString("\nREJECTION FEEDBACK (information-specific, never a session ban)\n")
	for _, feedback := range repository.CardFeedback {
		destination := feedback.Suggestion.FolderId
		if feedback.Suggestion.Kind == store.SuggestFolder {
			destination = feedback.Suggestion.ParentFolderId
		}
		fmt.Fprintf(&prompt, "destination: %s\nproposal: %s\ncontent: %s\nreason: %s\nexplanation: %s\n",
			repository.ContextDestination(destination), store.CardSummary(feedback.Suggestion), feedback.Suggestion.ProposedText, feedback.Reason, feedback.Explanation)
	}
	return prompt.String()
}
