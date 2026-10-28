package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

func (currentModel *model) queueDeferredProposal() {
	for _, suggestion := range currentModel.repository.Suggestions {
		if suggestion.Deferred && time.Now().After(suggestion.RevalidateAfter) && currentModel.repository.CanShow(suggestion) {
			currentModel.queueJob(job{kind: proposalReviewJob, suggestionId: suggestion.Id, proposalRevision: suggestion.Revision})
			return
		}
	}
}

func (currentModel model) proposalEnvironment(suggestion store.Suggestion) string {
	text := brain.ReviewContexts(&currentModel.repository)
	for _, identifier := range suggestion.SessionIds {
		if session := currentModel.repository.FindSession(identifier); session != nil {
			text += fmt.Sprintf("%s:%d:%s", identifier, session.CheckedAtPrompt, session.LastActivityAt)
		}
	}
	return store.ContextVersion(text)
}

func (currentModel model) proposalSource(suggestion store.Suggestion) string {
	var source strings.Builder
	for _, identifier := range suggestion.SessionIds {
		session := currentModel.repository.FindSession(identifier)
		if session == nil {
			fmt.Fprintf(&source, "Source %s no longer tracked; judge only preserved evidence.\n", identifier)
			continue
		}
		fmt.Fprintf(&source, "Session %s: %s\n", identifier, session.Title)
		if adapter, found := agent.ByName(session.Tool); found {
			if turns, err := adapter.Turns(identifier); err == nil {
				for _, turn := range turns[max(len(turns)-reviewTurns, 0):] {
					fmt.Fprintf(&source, "user: %s\nagent preceding user: %s\n", turn.User, turn.Agent)
				}
			} else {
				fmt.Fprintf(&source, "Latest transcript unavailable: %s. Judge preserved evidence and the latest available reply.\n", err)
			}
		}
		fmt.Fprintf(&source, "Latest agent reply: %s\n", currentModel.digests[identifier].LastAgentMessage)
	}
	return source.String()
}

func (currentModel *model) applyProposalReview(done jobDoneMessage, answer brain.ProposalReviewAnswer, callIdentifier string) {
	suggestion := currentModel.repository.FindSuggestion(done.job.suggestionId)
	if suggestion == nil || suggestion.Revision != done.job.proposalRevision || currentModel.proposalEnvironment(*suggestion) != done.proposalEnvironment {
		return
	}
	suggestion.RevalidateAfter = time.Now().Add(time.Minute)
	outcome := "withdrawn after revalidation"
	if answer.Valid {
		if suggestion.Kind == store.SuggestContext && len(suggestion.AddedLines) == 0 {
			if strings.TrimSpace(answer.Text) == "" {
				currentModel.setError("Proposal revalidation returned no refreshed context")
				currentModel.saveRepository()
				return
			}
			suggestion.ProposedText = answer.Text
			suggestion.ContextVersion = store.ContextVersion(currentModel.repository.ReadContext(suggestion.FolderId))
		}
		if err := currentModel.repository.PrepareSuggestion(suggestion); err != nil {
			answer.Valid, answer.Reason = false, err.Error()
		} else if currentModel.repository.CanShow(*suggestion) {
			suggestion.Deferred = false
			suggestion.ReviewConcern = ""
			suggestion.Revision++
			outcome = "promoted from backlog"
		} else {
			return
		}
	}
	if !answer.Valid && len(suggestion.Conversation) > 0 {
		if !currentModel.repository.CanShow(*suggestion) {
			currentModel.saveRepository()
			return
		}
		suggestion.Deferred, suggestion.UnreadReply = false, true
		suggestion.ReviewConcern = answer.Reason
		if strings.TrimSpace(suggestion.ReviewConcern) == "" {
			suggestion.ReviewConcern = "This proposal is no longer supported by the current context."
		}
		suggestion.Revision++
		suggestion.Conversation = append(suggestion.Conversation, store.CardMessage{FromDen: true, Text: "This proposal needs revision: " + suggestion.ReviewConcern, CallIdentifier: callIdentifier})
		outcome = "needs revision; discussion preserved"
	}
	currentModel.reportError(currentModel.repository.LogDecision(store.Decision{Job: "proposal revalidation", Action: "revalidate", Detail: answer.Reason, Outcome: outcome, CallId: callIdentifier, Suggestion: suggestion}))
	if !answer.Valid && suggestion.ReviewConcern == "" {
		currentModel.repository.Withdraw(suggestion.Id)
	}
	currentModel.saveRepository()
}
