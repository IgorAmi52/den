package brain

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/helper"
	"github.com/IgorAmi52/den/internal/store"
)

//go:embed classifier.py
var classifierProgram string

const classifierPolicy = "modernbert-setfit-v1: one training-format input per completed exchange; review if any score >= 0.625; always review approved plans, markdown writes, pending cards, and inference failures"

type ClassifierInput struct {
	Texts        []string  `json:"texts"`
	Exchanges    []int     `json:"exchanges"`
	InputHashes  []string  `json:"inputHashes"`
	ReviewReason string    `json:"reviewReason,omitempty"`
	Context      GateInput `json:"context"`
}

type ClassifierAnswer struct {
	GateAnswer
	Scores          []float64 `json:"scores,omitempty"`
	Threshold       float64   `json:"threshold"`
	ModelVersion    string    `json:"modelVersion,omitempty"`
	Device          string    `json:"device,omitempty"`
	PredictedReview bool      `json:"predictedReview"`
	InferenceError  string    `json:"inferenceError,omitempty"`
}

func ClassifierRequest(input GateInput, goal string, reviewReason string) helper.Request {
	classification := ClassifierInput{Context: input, ReviewReason: reviewReason}
	if len(input.WaitingCards) > 0 {
		classification.ReviewReason = "pending cards may need correction or withdrawal"
	}
	for _, exchange := range input.Exchanges {
		text := fmt.Sprintf("SESSION\ntitle: (historical session)\nfolder: (historical assignment unavailable)\ngoal: %s\n\nWAITING CARDS\n(none)\n\nNEW EXCHANGES\nexchange 1\nuser: %s\nagent: %s\n",
			truncateText(strings.TrimSpace(goal), 300), truncateText(strings.TrimSpace(exchange.Prompt), 700), endOfText(exchange.Reply, 700))
		classification.Texts = append(classification.Texts, text)
		classification.Exchanges = append(classification.Exchanges, exchange.Number)
		classification.InputHashes = append(classification.InputHashes, fmt.Sprintf("%x", sha256.Sum256([]byte(text))))
	}
	prompt, _ := json.Marshal(classification)
	return helper.Request{System: classifierPolicy, Prompt: string(prompt)}
}

func Classify(request helper.Request) (answer ClassifierAnswer, usage helper.Usage) {
	started := time.Now()
	usage.Model = "local/modernbert-setfit"
	answer.Threshold = 0.625
	var input ClassifierInput
	inferenceError := json.Unmarshal([]byte(request.Prompt), &input)
	if inferenceError == nil {
		inferenceError = runClassifier(request.Prompt, &answer)
	}
	if inferenceError == nil && (len(answer.Scores) != len(input.Texts) || len(answer.Scores) == 0 || answer.Threshold != 0.625 || answer.ModelVersion == "") {
		inferenceError = fmt.Errorf("invalid classifier output")
	}
	for _, score := range answer.Scores {
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			inferenceError = fmt.Errorf("invalid classifier score")
		}
		answer.PredictedReview = answer.PredictedReview || score >= answer.Threshold
	}
	answer.WorthReview = answer.PredictedReview
	answer.Kind, answer.Reason = "none", "all exchanges below the classifier threshold"
	if answer.WorthReview {
		answer.Kind, answer.Reason = "review", "an exchange reached the classifier threshold"
	}
	if input.ReviewReason != "" {
		answer.WorthReview, answer.Kind, answer.Reason = true, "review", input.ReviewReason
	}
	if inferenceError != nil {
		answer.WorthReview, answer.Kind, answer.Reason = true, "review", "classifier unavailable; allow Sol to review"
		answer.InferenceError = inferenceError.Error()
	}
	usage.Duration = time.Since(started)
	return answer, usage
}

func runClassifier(prompt string, answer *ClassifierAnswer) error {
	directory, err := store.DataDirectory()
	if err != nil {
		return err
	}
	directory = filepath.Join(directory, "gate")
	deadline, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(deadline, filepath.Join(directory, "venv", "bin", "python"), "-c", classifierProgram, filepath.Join(directory, "model"))
	command.Stdin = bytes.NewBufferString(prompt)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("classifier: %w: %s", err, endOfText(diagnostics.String(), 2000))
	}
	return json.Unmarshal(output, answer)
}
