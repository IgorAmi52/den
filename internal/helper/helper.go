// Package helper makes den's one-shot model calls through an agent CLI the user already has, with its own login.
// A call has no tools, loads none of the user's customizations, saves no session, and answers in JSON.
package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/store"
)

const callTimeout = 3 * time.Minute

// claudeModel follows den's instructions on what not to do, which matters most where den acts without asking.
// Its calls are short and need no thinking. Claude is den's fallback when codex isn't installed.
const claudeModel = "sonnet"

// Tier says how much thinking a call needs.
type Tier int

const (
	// Light calls are frequent and routine: naming a session, a first look at what it found.
	Light Tier = iota
	// Reasoning calls weigh a whole session, or do what the user asked: a review once it went quiet, the chat, the first run.
	Reasoning
)

// codexModel is the OpenAI model codex runs for a tier, how hard it thinks, and its price per million tokens,
// since codex doesn't report what a call cost.
type codexModel struct {
	name            string
	reasoningEffort string
	inputPrice      float64
	cachedPrice     float64
	cacheWritePrice float64
	outputPrice     float64
}

var codexModels = map[Tier]codexModel{
	Light:     {name: "gpt-6-luna", reasoningEffort: "none", inputPrice: 0.10, cachedPrice: 0.01, cacheWritePrice: 0.125, outputPrice: 0.50},
	Reasoning: {name: "gpt-6.1-sol", reasoningEffort: "low", inputPrice: 2, cachedPrice: 0.20, cacheWritePrice: 2.50, outputPrice: 10},
}

// Caller runs calls through one CLI: codex when installed, with OpenAI's GPT-6 models picked per tier, otherwise claude.
type Caller struct {
	Tool string
}

// Request is one call: den's instructions, the input, the JSON schema the answer must follow, and how much thinking it needs.
// Every object in the schema lists all its properties as required and allows no others, which both CLIs accept.
type Request struct {
	System string
	Prompt string
	Schema map[string]any
	Tier   Tier
}

// Usage is what a call cost and which model made it.
type Usage struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CostDollars  float64
	Duration     time.Duration
}

// Name says which CLI and models make the calls, like "codex gpt-6-luna · gpt-6.1-sol".
func (caller Caller) Name() string {
	if caller.Tool == "claude" {
		return "claude " + claudeModel
	}
	return "codex " + codexModels[Light].name + " · " + codexModels[Reasoning].name
}

func Find() (Caller, bool) {
	for _, tool := range []string{"codex", "claude"} {
		if _, err := exec.LookPath(tool); err == nil {
			return Caller{Tool: tool}, true
		}
	}
	return Caller{}, false
}

// Call runs the request and decodes the answer into answer. Calls run from den's own directory,
// never from the repo, so they never show up as the repo's sessions.
func (caller Caller) Call(request Request, answer any) (Usage, error) {
	deadline, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	directory, err := neutralDirectory()
	if err != nil {
		return Usage{}, err
	}
	started := time.Now()
	var raw []byte
	var usage Usage
	if caller.Tool == "codex" {
		raw, usage, err = callCodex(deadline, directory, request)
	} else {
		raw, usage, err = callClaude(deadline, directory, request)
	}
	usage.Duration = time.Since(started)
	if err != nil {
		return usage, err
	}
	if err := json.Unmarshal(raw, answer); err != nil {
		return usage, fmt.Errorf("%s answered something that isn't the expected JSON: %w", caller.Tool, err)
	}
	return usage, nil
}

func callClaude(deadline context.Context, directory string, request Request) ([]byte, Usage, error) {
	schema, err := json.Marshal(request.Schema)
	if err != nil {
		return nil, Usage{}, err
	}
	command := exec.CommandContext(deadline, "claude", "-p", "--safe-mode", "--model", claudeModel,
		"--settings", `{"alwaysThinkingEnabled":false}`, "--system-prompt", request.System, "--tools", "", "--strict-mcp-config",
		"--no-session-persistence", "--output-format", "json", "--json-schema", string(schema))
	command.Dir = directory
	command.Stdin = strings.NewReader(request.Prompt)
	output, err := runCommand(command)
	if err != nil {
		return nil, Usage{}, err
	}
	var result struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		TotalCostDollars float64         `json:"total_cost_usd"`
		Usage            struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, Usage{}, fmt.Errorf("claude gave no result: %s", firstLine(string(output)))
	}
	usage := Usage{Model: claudeModel, InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CostDollars: result.TotalCostDollars}
	if result.IsError || len(result.StructuredOutput) == 0 || string(result.StructuredOutput) == "null" {
		return nil, usage, fmt.Errorf("claude failed: %s", firstLine(result.Result))
	}
	return result.StructuredOutput, usage, nil
}

func callCodex(deadline context.Context, directory string, request Request) ([]byte, Usage, error) {
	model := codexModels[request.Tier]
	usage := Usage{Model: model.name}
	callDirectory, err := os.MkdirTemp(directory, "call-")
	if err != nil {
		return nil, usage, err
	}
	defer os.RemoveAll(callDirectory)
	schema, err := json.Marshal(request.Schema)
	if err != nil {
		return nil, usage, err
	}
	schemaPath, answerPath := filepath.Join(callDirectory, "schema.json"), filepath.Join(callDirectory, "answer.json")
	if err := os.WriteFile(schemaPath, schema, 0o644); err != nil {
		return nil, usage, err
	}
	command := exec.CommandContext(deadline, "codex", "exec", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check",
		"-m", model.name, "-c", "model_reasoning_effort="+model.reasoningEffort, "--json",
		"-C", callDirectory, "--output-schema", schemaPath, "-o", answerPath, "-")
	command.Dir = callDirectory
	command.Stdin = strings.NewReader(request.System + "\n\n" + request.Prompt)
	events, err := runCommand(command)
	if err != nil {
		return nil, usage, err
	}
	usage = codexUsage(events, model)
	answer, err := os.ReadFile(answerPath)
	if err != nil {
		return nil, usage, errors.New("codex wrote no answer")
	}
	return answer, usage, nil
}

// codexUsage reads the tokens a call used from codex's JSON events and prices them, cached and cache-written input apart.
func codexUsage(events []byte, model codexModel) Usage {
	usage := Usage{Model: model.name}
	for _, line := range bytes.Split(events, []byte("\n")) {
		var event struct {
			Usage *struct {
				InputTokens           int `json:"input_tokens"`
				CachedInputTokens     int `json:"cached_input_tokens"`
				CacheWriteInputTokens int `json:"cache_write_input_tokens"`
				OutputTokens          int `json:"output_tokens"`
				ReasoningOutputTokens int `json:"reasoning_output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &event) != nil || event.Usage == nil {
			continue
		}
		tokens := event.Usage
		usage.InputTokens, usage.OutputTokens = tokens.InputTokens, tokens.OutputTokens+tokens.ReasoningOutputTokens
		freshInput := max(tokens.InputTokens-tokens.CachedInputTokens-tokens.CacheWriteInputTokens, 0)
		usage.CostDollars = (float64(freshInput)*model.inputPrice + float64(tokens.CachedInputTokens)*model.cachedPrice +
			float64(tokens.CacheWriteInputTokens)*model.cacheWritePrice + float64(usage.OutputTokens)*model.outputPrice) / 1_000_000
	}
	return usage
}

func runCommand(command *exec.Cmd) ([]byte, error) {
	var errorOutput bytes.Buffer
	command.Stderr = &errorOutput
	output, err := command.Output()
	if err != nil && len(output) == 0 {
		message := firstLine(errorOutput.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", filepath.Base(command.Path), message)
	}
	return output, nil
}

// neutralDirectory is where calls run: den's own tmp directory, outside every repo.
func neutralDirectory() (string, error) {
	dataDirectory, err := store.DataDirectory()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(dataDirectory, "tmp")
	return directory, os.MkdirAll(directory, 0o755)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}
