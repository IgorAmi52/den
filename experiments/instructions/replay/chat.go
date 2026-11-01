package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/IgorAmi52/den/internal/brain"
	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

func chatChecks(directory string) {
	repository := store.Repository{RepositoryPath: filepath.Join(directory, "chat-checks"), Folders: []store.Folder{{Id: "operations", Name: "Operations"}}}
	current, failure := repository.ReadInstructions()
	must(failure)
	must(repository.SaveInstructions(store.InstructionsTemplate, store.ContextVersion(current)))
	caller, available := helper.Find()
	if !available {
		panic("model CLI unavailable")
	}
	conversation := []brain.ChatMessage{}
	messages := []string{
		"I'd like my Den to catch issues mentioned in my sessions and keep their status current. Give me a draft and a couple of illustrative examples.",
		"Change that draft to only watch Operations and its subfolders. Include an example of a suspected issue being disproved.",
		"Pause that instruction.",
		"Resume that instruction.",
		"Delete that instruction.",
	}
	results := []map[string]any{}
	for position, message := range messages {
		conversation = append(conversation, brain.ChatMessage{Text: message})
		input := brain.ChatInput{Repository: &repository, Conversation: conversation, EditingInstructions: true, Now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
		request := brain.ChatRequest(input)
		signature, failure := json.Marshal(request)
		must(failure)
		callDirectory := filepath.Join(directory, "chat-calls", store.ContextVersion(string(signature)))
		must(os.MkdirAll(callDirectory, 0700))
		writeJSON(filepath.Join(callDirectory, "request.json"), request)
		var answer brain.ChatAnswer
		var usage helper.Usage
		cached, failure := os.ReadFile(filepath.Join(callDirectory, "answer.json"))
		if failure == nil {
			must(json.Unmarshal(cached, &answer))
		} else {
			usage, failure = caller.Call(request, &answer)
			must(failure)
			writeJSON(filepath.Join(callDirectory, "answer.json"), answer)
			writeJSON(filepath.Join(callDirectory, "usage.json"), usage)
		}
		actions := brain.ResolveChatActions(answer, input, directory)
		if len(actions) != 1 || actions[0].Kind != "instructions" {
			panic(fmt.Sprintf("chat step %d produced no valid instruction action: %+v", position, answer))
		}
		conversation = append(conversation, brain.ChatMessage{FromDen: true, Text: answer.Reply, Actions: actions})
		proposed, failure := store.ParseInstructions(actions[0].InstructionsText)
		must(failure)
		if position == 0 {
			if len(proposed) != 1 || proposed[0].Scope != "repo" || len(proposed[0].ApprovedExamples()) < 2 {
				panic("draft missing scope or examples")
			}
			current, _ := repository.Instructions()
			if len(current) != 0 {
				panic("unapproved draft was saved")
			}
		} else {
			must(repository.SaveInstructions(actions[0].InstructionsText, actions[0].InstructionVersion))
			conversation[len(conversation)-1].Outcome = "done"
			saved, failure := repository.Instructions()
			must(failure)
			switch position {
			case 1:
				if len(saved) != 1 || saved[0].Scope != "operations" || len(saved[0].ApprovedExamples()) < 2 {
					panic("revised scope/examples not saved")
				}
			case 2:
				if !saved[0].Paused {
					panic("pause failed")
				}
			case 3:
				if saved[0].Paused {
					panic("resume failed")
				}
			case 4:
				if len(saved) != 0 {
					panic("delete failed")
				}
			}
		}
		results = append(results, map[string]any{"step": position + 1, "message": message, "answer": answer, "actions": actions, "passed": true})
		fmt.Printf("Chat step %d passed\n", position+1)
	}
	writeJSON(filepath.Join(directory, "chat-results.json"), results)
}
