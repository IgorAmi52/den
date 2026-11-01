package brain

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

//go:embed classifier.py
var classifierProgram string

const classifierPolicy = "instruction-gate-v2: complete new exchanges only, 256-token passages with 64-token overlap; local instruction and approved-example matching; pending cards and unavailable matching allow direct review"

type ClassifierInput struct {
	Texts        []string            `json:"texts"`
	Instructions []store.Instruction `json:"instructions"`
	Exchanges    []int               `json:"exchanges"`
	InputHashes  []string            `json:"inputHashes"`
	ReviewReason string              `json:"reviewReason,omitempty"`
}

type ClassifierAnswer struct {
	GateAnswer
	Scores              []float64            `json:"scores,omitempty"`
	InstructionScores   map[string][]float64 `json:"instructionScores,omitempty"`
	MatchedInstructions []string             `json:"matchedInstructions,omitempty"`
	Threshold           float64              `json:"threshold"`
	ModelVersion        string               `json:"modelVersion,omitempty"`
	Device              string               `json:"device,omitempty"`
	PredictedReview     bool                 `json:"predictedReview"`
	InferenceError      string               `json:"inferenceError,omitempty"`
}

func ClassifierRequest(input GateInput, goal string, reviewReason string) helper.Request {
	classification := ClassifierInput{}
	if len(input.WaitingCards) > 0 {
		classification.ReviewReason = "pending cards may need correction or withdrawal"
	}
	for _, instruction := range input.Instructions {
		instruction.Examples = instruction.ApprovedExamples()
		classification.Instructions = append(classification.Instructions, instruction)
		if len(instruction.Examples) == 0 {
			classification.ReviewReason = "an applicable instruction has no usable approved examples"
		}
	}
	for _, exchange := range input.Exchanges {
		text := "User: " + exchange.Prompt + "\nAssistant: " + exchange.Reply
		classification.Texts = append(classification.Texts, text)
		classification.Exchanges = append(classification.Exchanges, exchange.Number)
		classification.InputHashes = append(classification.InputHashes, fmt.Sprintf("%x", sha256.Sum256([]byte(text))))
	}
	prompt, _ := json.Marshal(classification)
	return helper.Request{System: classifierPolicy, Prompt: string(prompt)}
}

func Classify(request helper.Request) (answer ClassifierAnswer, usage helper.Usage) {
	started := time.Now()
	usage.Model = "local/instruction-gate"
	defer func() { usage.Duration = time.Since(started) }()
	var input ClassifierInput
	failure := json.Unmarshal([]byte(request.Prompt), &input)
	if failure == nil && input.ReviewReason != "" {
		answer.WorthReview, answer.Kind, answer.Reason = true, "review", input.ReviewReason
		return
	}
	if failure == nil && (len(input.Instructions) == 0 || len(input.Texts) == 0) {
		answer.Kind, answer.Reason = "none", "no applicable active instructions or new exchanges"
		return
	}
	if failure == nil {
		failure = runClassifier(request.Prompt, &answer)
	}
	if failure == nil && (len(answer.Scores) != len(input.Texts) || answer.ModelVersion == "" || math.IsNaN(answer.Threshold) || answer.Threshold < -1 || answer.Threshold > 1) {
		failure = fmt.Errorf("invalid local gate output")
	}
	if failure == nil {
		for _, instruction := range input.Instructions {
			scores := answer.InstructionScores[instruction.Identifier]
			if len(scores) != len(input.Texts) {
				failure = fmt.Errorf("missing instruction scores")
				break
			}
			matched := false
			for _, score := range scores {
				if math.IsNaN(score) || math.IsInf(score, 0) || score < -1.001 || score > 1.001 {
					failure = fmt.Errorf("invalid similarity score")
					break
				}
				matched = matched || score >= answer.Threshold
			}
			if matched {
				answer.MatchedInstructions = append(answer.MatchedInstructions, instruction.Identifier)
			}
		}
	}
	answer.PredictedReview = len(answer.MatchedInstructions) > 0
	answer.WorthReview, answer.Kind, answer.Reason = answer.PredictedReview, "none", "no active instruction matched the new exchange"
	if answer.WorthReview {
		answer.Kind, answer.Reason = "review", "the new exchange matched an active instruction"
	}
	if failure != nil {
		answer.WorthReview, answer.Kind, answer.Reason = true, "review", "local gate unavailable; allow direct review"
		if !errors.Is(failure, os.ErrNotExist) {
			answer.InferenceError = failure.Error()
		} else {
			answer.Reason = "local gate not installed; use direct review"
		}
	}
	return
}

var classifierWorker struct {
	sync.Mutex
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	version string
}

func runClassifier(prompt string, answer *ClassifierAnswer) error {
	classifierWorker.Lock()
	defer classifierWorker.Unlock()
	directory, failure := store.DataDirectory()
	if failure != nil {
		return failure
	}
	directory = filepath.Join(directory, "instruction-gate")
	manifest, failure := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if failure != nil {
		return failure
	}
	version := fmt.Sprintf("%x", sha256.Sum256(manifest))
	stop := func() {
		if classifierWorker.command != nil {
			classifierWorker.input.Close()
			classifierWorker.command.Process.Kill()
			classifierWorker.command.Wait()
			classifierWorker.command = nil
		}
	}
	if classifierWorker.command != nil && classifierWorker.version != version {
		stop()
	}
	if classifierWorker.command == nil {
		command := exec.Command(filepath.Join(directory, "venv", "bin", "python"), "-u", "-c", classifierProgram, directory)
		input, failure := command.StdinPipe()
		if failure != nil {
			return failure
		}
		output, failure := command.StdoutPipe()
		if failure != nil {
			input.Close()
			return failure
		}
		if failure := command.Start(); failure != nil {
			input.Close()
			output.Close()
			return failure
		}
		classifierWorker.command, classifierWorker.input, classifierWorker.output, classifierWorker.version = command, input, bufio.NewReader(output), version
	}
	process := classifierWorker.command.Process
	timer := time.AfterFunc(90*time.Second, func() { process.Kill() })
	_, failure = io.WriteString(classifierWorker.input, prompt+"\n")
	var output []byte
	if failure == nil {
		output, failure = classifierWorker.output.ReadBytes('\n')
	}
	timer.Stop()
	if failure == nil {
		failure = json.Unmarshal(output, answer)
	}
	if failure == nil && answer.ModelVersion != version {
		failure = fmt.Errorf("embedding model version mismatch")
	}
	if failure != nil {
		stop()
		return fmt.Errorf("local embedding worker: %w", failure)
	}
	return nil
}
