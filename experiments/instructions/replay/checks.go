package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

func localChecks(directory string) {
	repository := store.Repository{RepositoryPath: filepath.Join(directory, "local-checks"), Folders: []store.Folder{{Id: "parent", Name: "Parent"}, {Id: "child", ParentId: "parent", Name: "Child"}, {Id: "other", Name: "Other"}}}
	text, failure := repository.ReadInstructions()
	must(failure)
	instruction := store.Instruction{Identifier: "issues", Title: "Track issues", Scope: "parent", Text: "Catch issues and keep their status current.", Examples: []string{"The sync writes lose updates.", "The sync race was fixed."}}
	instruction.ExamplesRevision = instruction.BehaviorVersion()
	must(repository.SaveInstructions(store.FormatInstructions([]store.Instruction{instruction}), store.ContextVersion(text)))
	passed := []string{}
	check := func(name string, condition bool) {
		if !condition {
			panic("check failed: " + name)
		}
		passed = append(passed, name)
	}
	applicable, failure := repository.ApplicableInstructions("child")
	must(failure)
	check("folder scope includes descendants", len(applicable) == 1)
	applicable, failure = repository.ApplicableInstructions("other")
	must(failure)
	check("folder scope excludes siblings", len(applicable) == 0)
	stored, failure := repository.ReadInstructions()
	must(failure)
	parsed, failure := store.ParseInstructions(stored)
	must(failure)
	check("canonical markdown roundtrip", store.FormatInstructions(parsed) == stored)
	check("approved examples available", len(parsed[0].ApprovedExamples()) == 2)
	altered := strings.Replace(stored, instruction.Text, "Only catch performance issues.", 1)
	parsed, failure = store.ParseInstructions(altered)
	must(failure)
	check("changed behavior invalidates old examples", len(parsed[0].ApprovedExamples()) == 0)
	check("stale instruction write refused", repository.SaveInstructions(stored, "obsolete") != nil)
	_, failure = store.ParseInstructions("Catch bugs.\n\nTrack decisions.")
	check("ambiguous hand-edited grouping refused", failure != nil)
	_, failure = store.ParseInstructions(stored + "\n## Unknown\nStatus: maybe\nSome text\n")
	check("unknown status refused", failure != nil)
	must(repository.WriteContext("", "# Repo\nShared convention.\n"))
	must(repository.WriteContext("parent", "# Parent\nParent convention.\n"))
	must(repository.WriteContext("child", "# Child\n\n## Findings\n- Open: sync writes lose updates.\n- Keep this unrelated entry.\n"))
	inherited, _, _ := repository.MergedContext("child")
	check("Den instructions excluded from coding context", !strings.Contains(inherited, instruction.Text))
	gate, _ := brain.Classify(brain.ClassifierRequest(brain.GateInput{}, "", ""))
	check("no instructions skips review", !gate.WorthReview)
	gate, _ = brain.Classify(brain.ClassifierRequest(brain.GateInput{WaitingCards: []string{"pending"}}, "", ""))
	check("pending legacy cards still reviewed", gate.WorthReview)
	withoutExamples := instruction
	withoutExamples.Examples = nil
	gate, _ = brain.Classify(brain.ClassifierRequest(brain.GateInput{Instructions: []store.Instruction{withoutExamples}, Exchanges: []brain.Exchange{{Prompt: "test", Reply: "test"}}}, "", ""))
	check("missing examples allows direct review", gate.WorthReview && gate.InferenceError == "")
	gate, _ = brain.Classify(brain.ClassifierRequest(brain.GateInput{Instructions: []store.Instruction{instruction}, Exchanges: []brain.Exchange{{Prompt: "test", Reply: "test"}}}, "", ""))
	check("missing runtime allows direct review", gate.WorthReview && strings.Contains(gate.Reason, "not installed"))
	session := store.Session{SessionId: "source", FolderId: "child"}
	keep := brain.Keep{InstructionIdentifier: instruction.Identifier, InstructionRevision: instruction.Revision(), ReadVersion: repository.ContextReadVersion("child"), Destination: "child", Kind: "notes", Confidence: 1, Reason: "Track the issue resolution.", Evidence: []string{"The race is fixed."}, ReplacesText: "- Open: sync writes lose updates.", Lines: []string{"- Resolved: version checks prevent lost updates."}}
	suggestion, outcome := brain.KeepSuggestion(keep, session, &repository, "The race is fixed.")
	check("lifecycle correction accepted without future-use requirement", outcome == "proposed" && strings.Contains(suggestion.ProposedText, "Keep this unrelated entry."))
	keep.Lines = nil
	suggestion, outcome = brain.KeepSuggestion(keep, session, &repository, "The race is fixed.")
	check("obsolete entry can be removed without deleting unrelated text", outcome == "proposed" && !strings.Contains(suggestion.ProposedText, "Open:") && strings.Contains(suggestion.ProposedText, "Keep this unrelated entry."))
	keep.ReplacesText = ""
	keep.Lines = []string{"First finding.", "Second finding.", "Third finding.", "Fourth finding.", "Fifth finding."}
	suggestion, outcome = brain.KeepSuggestion(keep, session, &repository, "The race is fixed.")
	check("more than three requested findings preserved", outcome == "proposed" && len(suggestion.AddedLines) == 5)
	must(repository.WriteContext("parent", "# Parent\nChanged convention.\n"))
	_, outcome = brain.KeepSuggestion(keep, session, &repository, "The race is fixed.")
	check("stale inherited context refused", strings.Contains(outcome, "have changed"))
	keep.ReadVersion = repository.ContextReadVersion("child")
	keep.InstructionRevision = "obsolete"
	_, outcome = brain.KeepSuggestion(keep, session, &repository, "The race is fixed.")
	check("stale instruction proposal refused", strings.Contains(outcome, "changed or unknown"))
	answer := brain.ChatAnswer{}
	answer.Actions = append(answer.Actions, brain.ChatProposedAction{Type: "pauseInstruction", From: "issues"})
	actions := brain.ResolveChatActions(answer, brain.ChatInput{Repository: &repository, EditingInstructions: true}, directory)
	check("chat pause produces a confirmation action", len(actions) == 1 && actions[0].Kind == "instructions")
	before, _ := repository.ApplicableInstructions("child")
	check("chat draft does not mutate instruction", len(before) == 1)
	must(repository.SaveInstructions(actions[0].InstructionsText, actions[0].InstructionVersion))
	after, _ := repository.ApplicableInstructions("child")
	check("accepted pause stops monitoring", len(after) == 0)
	badAnswer := brain.ChatAnswer{Actions: []brain.ChatProposedAction{{Type: "saveInstruction", Name: "API constraints", Text: "Track API constraints.\n\n### Illustrative trigger examples\n- A failure.", Examples: []string{"x"}}}}
	invalidActions := brain.ResolveChatActions(badAnswer, brain.ChatInput{Repository: &repository}, directory)
	check("malformed draft is rejected with a usable error state", len(invalidActions) == 0 && brain.InvalidInstructionDraft(badAnswer, invalidActions))
	badAnswer.Actions[0].Text = "Track external API constraints."
	badAnswer.Actions[0].Examples = []string{"First line.\nSecond line."}
	invalidActions = brain.ResolveChatActions(badAnswer, brain.ChatInput{Repository: &repository}, directory)
	check("malformed examples are not silently discarded", len(invalidActions) == 0)
	previousRevision := instruction.Revision()
	instruction.Examples = []string{"A different approved trigger."}
	check("example changes tracked in instruction provenance", instruction.Revision() != previousRevision)
	fmt.Printf("Passed %d local checks\n", len(passed))
	writeJSON(filepath.Join(directory, "checks.json"), passed)
}
