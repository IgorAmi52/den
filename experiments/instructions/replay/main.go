package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

type scenario struct {
	Name             string
	Instruction      string
	Prompt           string
	Reply            string
	Destination      string
	Kind             string
	Change           bool
	Nested           bool
	NoMatchingFolder bool
	Context          string
	Replacement      bool
	Contains         string
}

type result struct {
	Name        string             `json:"name"`
	Passed      bool               `json:"passed"`
	Failures    []string           `json:"failures"`
	Answer      brain.ReviewAnswer `json:"answer"`
	Suggestions []store.Suggestion `json:"suggestions"`
	Usage       helper.Usage       `json:"usage"`
}

func main() {
	directory := flag.String("output", ".local/instruction-replay", "isolated evaluation artifacts")
	checks := flag.Bool("checks", false, "run local behavior checks without model calls")
	chat := flag.Bool("chat", false, "evaluate instruction drafting and management through chat")
	gate := flag.Bool("gate", false, "verify and replay through the installed experimental gate")
	runtimeHome := flag.String("runtime-home", ".local/instruction-runtime", "isolated runtime home")
	flag.Parse()
	absolute, failure := filepath.Abs(*directory)
	must(failure)
	must(os.MkdirAll(absolute, 0700))
	must(os.Setenv("DEN_HOME", filepath.Join(absolute, "home")))
	if *gate {
		gateChecks(absolute, *runtimeHome)
		return
	}
	if *chat {
		chatChecks(absolute)
		return
	}
	if *checks {
		localChecks(absolute)
		return
	}
	var scenarios []scenario
	content, failure := os.ReadFile("experiments/instructions/scenarios.json")
	must(failure)
	must(json.Unmarshal(content, &scenarios))
	var instructions []store.Instruction
	content, failure = os.ReadFile("experiments/instructions/instructions.json")
	must(failure)
	must(json.Unmarshal(content, &instructions))
	results := make([]result, len(scenarios))
	limiter := make(chan struct{}, 2)
	var workers sync.WaitGroup
	for position, specification := range scenarios {
		workers.Add(1)
		go func() {
			defer workers.Done()
			limiter <- struct{}{}
			defer func() { <-limiter }()
			results[position] = runScenario(absolute, specification, instructions)
			fmt.Printf("%s: %t %v\n", specification.Name, results[position].Passed, results[position].Failures)
		}()
	}
	workers.Wait()
	writeJSON(filepath.Join(absolute, "results.json"), results)
	passed := 0
	for _, result := range results {
		if result.Passed {
			passed++
		}
	}
	fmt.Printf("Passed %d/%d reviewer scenarios\n", passed, len(results))
}

func runScenario(directory string, specification scenario, instructions []store.Instruction) result {
	result := result{Name: specification.Name}
	repository := store.Repository{RepositoryPath: filepath.Join(directory, specification.Name)}
	repository.Folders = []store.Folder{{Id: "design", Name: "Design"}, {Id: "integrations", Name: "Integrations"}}
	if !specification.NoMatchingFolder {
		repository.Folders = append(repository.Folders, store.Folder{Id: "followup", Name: "Follow-up"})
	}
	if specification.Nested {
		repository.Folders = append(repository.Folders, store.Folder{Id: "operations", Name: "Operations"}, store.Folder{Id: "incidents", Name: "Incidents", ParentId: "operations"})
	}
	must(repository.WriteContext("", "# Workspace\n\n## Purpose\nShared brief inherited by every coding session: project conventions and stable project-wide constraints. Active bugs and deferred tasks belong in their workspaces.\n\n## Conventions\n- Support macOS only.\n"))
	purposes := map[string]string{"design": "Architectural decisions and their rationale.", "integrations": "External API limitations and integration workarounds.", "followup": "Track current issues and explicitly deferred work, keeping their status current.", "operations": "Operational responsibilities and runbooks. Incidents belong in the child folder.", "incidents": "Track suspected and confirmed operational issues, including resolutions."}
	for _, folder := range repository.Folders {
		context := "# " + folder.Name + "\n\n## Purpose\n" + purposes[folder.Id] + "\n\n## Findings\n- Unrelated reference: keep the team handbook link.\n"
		if folder.Id == specification.Destination && specification.Context != "" {
			context += specification.Context + "\n"
		}
		must(repository.WriteContext(folder.Id, context))
	}
	instruction := instructions[slices.IndexFunc(instructions, func(candidate store.Instruction) bool { return candidate.Identifier == specification.Instruction })]
	instruction.Scope = "repo"
	instruction.ExamplesRevision = instruction.BehaviorVersion()
	currentInstructions, failure := repository.ReadInstructions()
	must(failure)
	must(repository.SaveInstructions(store.FormatInstructions([]store.Instruction{instruction}), store.ContextVersion(currentInstructions)))
	session := store.Session{SessionId: "source", Title: specification.Name, FolderId: "design"}
	repository.Sessions = []store.Session{session}
	input := brain.ReviewInput{Repository: &repository, Session: session, CardReview: true,
		Turns: []agent.Turn{{User: specification.Prompt, Reply: specification.Reply}}, NewFromTurn: 0}
	request := brain.ReviewRequest(input)
	signature, failure := json.Marshal(request)
	must(failure)
	callDirectory := filepath.Join(directory, "calls", store.ContextVersion(string(signature)))
	must(os.MkdirAll(callDirectory, 0700))
	writeJSON(filepath.Join(callDirectory, "request.json"), request)
	cached, failure := os.ReadFile(filepath.Join(callDirectory, "result.json"))
	if failure == nil {
		must(json.Unmarshal(cached, &result))
		result.Failures, result.Suggestions = nil, nil
	} else {
		caller, available := helper.Find()
		if !available {
			panic("No model CLI available")
		}
		result.Usage, failure = caller.Call(request, &result.Answer)
		if failure != nil {
			result.Failures = append(result.Failures, failure.Error())
		}
	}
	for _, keep := range result.Answer.Keeps {
		suggestion, outcome := brain.KeepSuggestion(keep, session, &repository, specification.Prompt+"\n"+specification.Reply)
		if outcome != "proposed" {
			result.Failures = append(result.Failures, outcome)
			continue
		}
		result.Suggestions = append(result.Suggestions, suggestion)
		destination := keep.Destination
		if destination != specification.Destination {
			result.Failures = append(result.Failures, "wrong destination: "+destination)
		}
		if specification.Kind != "" && keep.Kind != specification.Kind {
			result.Failures = append(result.Failures, "wrong proposal kind")
		}
		if specification.Replacement && keep.ReplacesText == "" {
			result.Failures = append(result.Failures, "expected lifecycle correction")
		}
		if specification.Contains != "" && !strings.Contains(strings.ToLower(suggestion.ProposedText), specification.Contains) {
			result.Failures = append(result.Failures, "missing required qualification")
		}
		if keep.Kind == "notes" && !strings.Contains(suggestion.ProposedText, "Unrelated reference: keep the team handbook link.") {
			result.Failures = append(result.Failures, "unrelated context lost")
		}
		for _, evidence := range keep.Evidence {
			if !strings.Contains(specification.Prompt+"\n"+specification.Reply, evidence) {
				result.Failures = append(result.Failures, "non-verbatim evidence")
			}
		}
		if suggestion.Kind == store.SuggestFolder {
			suggestion.Id = "proposal"
			repository.Suggestions = append(repository.Suggestions, suggestion)
			if _, failure := repository.Accept(suggestion.Id); failure != nil {
				result.Failures = append(result.Failures, "accept: "+failure.Error())
			}
			if repository.FindSession("source").FolderId != "design" {
				result.Failures = append(result.Failures, "source session moved")
			}
		}
	}
	if (len(result.Suggestions) > 0) != specification.Change {
		result.Failures = append(result.Failures, "incorrect proposal presence")
	}
	result.Passed = len(result.Failures) == 0
	writeJSON(filepath.Join(callDirectory, "result.json"), result)
	return result
}

func writeJSON(path string, value any) {
	content, failure := json.MarshalIndent(value, "", "  ")
	must(failure)
	must(os.WriteFile(path, append(content, '\n'), 0600))
}

func must(failure error) {
	if failure != nil {
		panic(fmt.Sprintf("%s: %v", time.Now().Format(time.RFC3339), failure))
	}
}
