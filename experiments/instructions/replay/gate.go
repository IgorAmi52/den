package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/store"
)

func gateChecks(directory string, runtimeHome string) {
	absolute, failure := filepath.Abs(runtimeHome)
	must(failure)
	must(os.Setenv("DEN_HOME", absolute))
	var instructions []store.Instruction
	content, failure := os.ReadFile("experiments/instructions/instructions.json")
	must(failure)
	must(json.Unmarshal(content, &instructions))
	for position := range instructions {
		instructions[position].Scope = "repo"
		instructions[position].ExamplesRevision = instructions[position].BehaviorVersion()
	}
	measurements := []map[string]any{}
	for position := 0; position < 3; position++ {
		passage := "The sync job loses updates when two writers save at once."
		if position == 2 {
			passage = strings.Repeat("Routine formatting completed. ", 300) + passage + strings.Repeat("Other work is unchanged. ", 300)
		}
		request := brain.ClassifierRequest(brain.GateInput{Instructions: instructions, Exchanges: []brain.Exchange{{Number: 1, Prompt: "Check the current report.", Reply: passage}}}, "", "")
		started := time.Now()
		answer, usage := brain.Classify(request)
		if !answer.WorthReview || answer.InferenceError != "" || answer.ModelVersion == "" {
			panic(fmt.Sprintf("runtime check failed: %+v", answer))
		}
		measurements = append(measurements, map[string]any{"check": position, "seconds": time.Since(started).Seconds(), "answer": answer, "usage": usage})
	}
	var scenarios []scenario
	content, failure = os.ReadFile("experiments/instructions/scenarios.json")
	must(failure)
	must(json.Unmarshal(content, &scenarios))
	scenarioResults := []map[string]any{}
	for _, specification := range scenarios {
		var selected []store.Instruction
		for _, instruction := range instructions {
			if instruction.Identifier == specification.Instruction {
				selected = append(selected, instruction)
			}
		}
		answer, usage := brain.Classify(brain.ClassifierRequest(brain.GateInput{Instructions: selected, Exchanges: []brain.Exchange{{Number: 1, Prompt: specification.Prompt, Reply: specification.Reply}}}, "", ""))
		if answer.InferenceError != "" {
			panic(answer.InferenceError)
		}
		scenarioResults = append(scenarioResults, map[string]any{"name": specification.Name, "worthReview": answer.WorthReview, "expectedChange": specification.Change, "scores": answer.Scores, "usage": usage})
	}
	manifestPath := filepath.Join(absolute, "instruction-gate", "manifest.json")
	original, failure := os.ReadFile(manifestPath)
	must(failure)
	var manifest map[string]any
	must(json.Unmarshal(original, &manifest))
	manifest["files"].(map[string]any)["model/config.json"] = "invalid-artifact-hash"
	modified, failure := json.Marshal(manifest)
	must(failure)
	must(os.WriteFile(manifestPath, modified, 0600))
	defer os.WriteFile(manifestPath, original, 0600)
	fallback, _ := brain.Classify(brain.ClassifierRequest(brain.GateInput{Instructions: instructions, Exchanges: []brain.Exchange{{Number: 1, Prompt: "Check", Reply: "Report"}}}, "", ""))
	if !fallback.WorthReview || fallback.InferenceError == "" {
		panic("corrupt embedding artifact did not allow direct review")
	}
	writeJSON(filepath.Join(directory, "gate-results.json"), map[string]any{"measurements": measurements, "scenarios": scenarioResults, "corruptArtifactFallback": fallback})
	fmt.Println("Local runtime, warm reuse, long-message detection, scenario replay and corrupt-artifact fallback passed")
}
