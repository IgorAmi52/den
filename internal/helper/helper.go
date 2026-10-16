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

// claudeModel is small and fast; den's calls are short and need no thinking.
const claudeModel = "haiku"

// Caller runs calls through one CLI: claude when installed, since its calls cost far fewer tokens, otherwise codex.
type Caller struct {
	Tool string
}

// Request is one call: den's instructions, the input, and the JSON schema the answer must follow.
// Every object in the schema lists all its properties as required and allows no others, which both CLIs accept.
type Request struct {
	System string
	Prompt string
	Schema map[string]any
}

// Usage is what a call cost, when the CLI says.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CostDollars  float64
	Duration     time.Duration
}

func Find() (Caller, bool) {
	for _, tool := range []string{"claude", "codex"} {
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
		raw, err = callCodex(deadline, directory, request)
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
	usage := Usage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CostDollars: result.TotalCostDollars}
	if result.IsError || len(result.StructuredOutput) == 0 || string(result.StructuredOutput) == "null" {
		return nil, usage, fmt.Errorf("claude failed: %s", firstLine(result.Result))
	}
	return result.StructuredOutput, usage, nil
}

func callCodex(deadline context.Context, directory string, request Request) ([]byte, error) {
	callDirectory, err := os.MkdirTemp(directory, "call-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(callDirectory)
	schema, err := json.Marshal(request.Schema)
	if err != nil {
		return nil, err
	}
	schemaPath, answerPath := filepath.Join(callDirectory, "schema.json"), filepath.Join(callDirectory, "answer.json")
	if err := os.WriteFile(schemaPath, schema, 0o644); err != nil {
		return nil, err
	}
	command := exec.CommandContext(deadline, "codex", "exec", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check",
		"-C", callDirectory, "--output-schema", schemaPath, "-o", answerPath, "-")
	command.Dir = callDirectory
	command.Stdin = strings.NewReader(request.System + "\n\n" + request.Prompt)
	if _, err := runCommand(command); err != nil {
		return nil, err
	}
	answer, err := os.ReadFile(answerPath)
	if err != nil {
		return nil, errors.New("codex wrote no answer")
	}
	return answer, nil
}

// runCommand runs a call. claude exits with an error code on a failed call but still prints its JSON result,
// which says why, so output wins over the exit code.
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
