package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const InstructionsTemplate = "# Den instructions\n\nDescribe what Den should notice in your sessions. Ask Den to draft an instruction and illustrative examples, or add a ## heading and your instruction below.\n"

type Instruction struct {
	Identifier       string   `json:"identifier"`
	Title            string   `json:"title"`
	Scope            string   `json:"scope"`
	Paused           bool     `json:"paused"`
	Text             string   `json:"text"`
	Examples         []string `json:"examples"`
	ExamplesRevision string   `json:"examplesRevision"`
}

func (instruction Instruction) BehaviorVersion() string {
	return ContextVersion(instruction.Text + "\n" + instruction.Scope)
}

func (instruction Instruction) Revision() string {
	content, _ := json.Marshal([]any{instruction.Title, instruction.Scope, instruction.Text, instruction.Examples})
	return ContextVersion(string(content))
}

func (instruction Instruction) ApprovedExamples() []string {
	if instruction.ExamplesRevision != instruction.BehaviorVersion() {
		return nil
	}
	return instruction.Examples
}

func (repository Repository) InstructionsPath() (string, error) {
	directory, failure := repository.Directory()
	return filepath.Join(directory, "instructions.md"), failure
}

func (repository Repository) ReadInstructions() (string, error) {
	path, failure := repository.InstructionsPath()
	if failure != nil {
		return "", failure
	}
	content, failure := os.ReadFile(path)
	if os.IsNotExist(failure) {
		return "", nil
	}
	return string(content), failure
}

func ParseInstructions(text string) ([]Instruction, error) {
	var instructions []Instruction
	var current *Instruction
	section := ""
	inMetadata := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			instructions = append(instructions, Instruction{Title: strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")), Scope: "repo"})
			current = &instructions[len(instructions)-1]
			section, inMetadata = "instruction", true
			continue
		}
		if current == nil {
			if trimmed == "" || strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "Describe what Den should notice") {
				continue
			}
			return nil, fmt.Errorf("add a ## title before each instruction; your text has not been changed")
		}
		switch {
		case trimmed == "---":
			current = nil
		case inMetadata && strings.HasPrefix(trimmed, "Identifier:"):
			current.Identifier = strings.TrimSpace(strings.TrimPrefix(trimmed, "Identifier:"))
		case inMetadata && strings.HasPrefix(trimmed, "Scope:"):
			current.Scope = strings.TrimSpace(strings.TrimPrefix(trimmed, "Scope:"))
			if current.Scope == "this repo" {
				current.Scope = "repo"
			}
		case inMetadata && strings.HasPrefix(trimmed, "Status:"):
			status := strings.TrimSpace(strings.TrimPrefix(trimmed, "Status:"))
			if status != "active" && status != "paused" {
				return nil, fmt.Errorf("%s: status must be active or paused", current.Title)
			}
			current.Paused = status == "paused"
		case strings.HasPrefix(trimmed, "<!-- examples-for: ") && strings.HasSuffix(trimmed, " -->"):
			current.ExamplesRevision = strings.TrimSuffix(strings.TrimPrefix(trimmed, "<!-- examples-for: "), " -->")
		case trimmed == "### Instruction":
			section, inMetadata = "instruction", false
		case trimmed == "### Approved trigger examples":
			section = "examples"
		case strings.HasPrefix(trimmed, "### "):
			return nil, fmt.Errorf("%s: use ### Instruction or ### Approved trigger examples; unknown section %q", current.Title, trimmed)
		case section == "examples" && trimmed != "":
			if !strings.HasPrefix(trimmed, "- ") {
				return nil, fmt.Errorf("%s: each example must start with -", current.Title)
			}
			current.Examples = append(current.Examples, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		case section == "instruction":
			if trimmed != "" {
				inMetadata = false
			}
			current.Text += line + "\n"
		}
	}
	identifiers := map[string]bool{}
	for position := range instructions {
		instruction := &instructions[position]
		instruction.Text = strings.TrimSpace(instruction.Text)
		if instruction.Title == "" || instruction.Text == "" || instruction.Scope == "" {
			return nil, fmt.Errorf("each instruction needs a title, scope and instruction text")
		}
		if instruction.Identifier == "" {
			instruction.Identifier = ContextVersion(instruction.Title)[:12]
		}
		if identifiers[instruction.Identifier] || strings.ContainsAny(instruction.Identifier, " \t\n/\\") {
			return nil, fmt.Errorf("instruction identifiers must be unique and contain no spaces or slashes")
		}
		identifiers[instruction.Identifier] = true
		if instruction.ExamplesRevision == "" {
			instruction.ExamplesRevision = instruction.BehaviorVersion()
		}
	}
	return instructions, nil
}

func FormatInstructions(instructions []Instruction) string {
	if len(instructions) == 0 {
		return InstructionsTemplate
	}
	var formatted strings.Builder
	formatted.WriteString("# Den instructions\n")
	for position, instruction := range instructions {
		if position > 0 {
			formatted.WriteString("\n---\n")
		}
		status := "active"
		if instruction.Paused {
			status = "paused"
		}
		fmt.Fprintf(&formatted, "\n## %s\n\nIdentifier: %s\nScope: %s\nStatus: %s\n\n### Instruction\n\n%s\n", instruction.Title, instruction.Identifier, instruction.Scope, status, instruction.Text)
		if len(instruction.Examples) > 0 {
			fmt.Fprintf(&formatted, "\n### Approved trigger examples\n\n<!-- examples-for: %s -->\n", instruction.ExamplesRevision)
			for _, example := range instruction.Examples {
				formatted.WriteString("\n- " + example + "\n")
			}
		}
	}
	return formatted.String()
}

func (repository Repository) Instructions() ([]Instruction, error) {
	text, failure := repository.ReadInstructions()
	if failure != nil {
		return nil, failure
	}
	instructions, failure := ParseInstructions(text)
	if failure != nil {
		return nil, failure
	}
	for _, instruction := range instructions {
		if instruction.Scope != "repo" && repository.FindFolder(instruction.Scope) == nil {
			return nil, fmt.Errorf("%s: scope %q is not an existing folder; edit its scope", instruction.Title, instruction.Scope)
		}
	}
	return instructions, nil
}

func (repository Repository) SaveInstructions(text string, expectedVersion string) error {
	current, failure := repository.ReadInstructions()
	if failure != nil {
		return failure
	}
	if ContextVersion(current) != expectedVersion {
		return fmt.Errorf("instructions changed; review the latest file before saving")
	}
	instructions, failure := ParseInstructions(text)
	if failure != nil {
		return failure
	}
	for _, instruction := range instructions {
		if instruction.Scope != "repo" && repository.FindFolder(instruction.Scope) == nil {
			return fmt.Errorf("%s: scope must be repo or an existing folder identifier", instruction.Title)
		}
	}
	path, failure := repository.InstructionsPath()
	if failure != nil {
		return failure
	}
	return WriteText(path, FormatInstructions(instructions))
}

func (repository Repository) ApplicableInstructions(folderIdentifier string) ([]Instruction, error) {
	instructions, failure := repository.Instructions()
	if failure != nil {
		return nil, failure
	}
	ancestors := repository.FolderPath(folderIdentifier)
	return slices.DeleteFunc(instructions, func(instruction Instruction) bool {
		return instruction.Paused || (instruction.Scope != "repo" && !slices.ContainsFunc(ancestors, func(folder Folder) bool { return folder.Id == instruction.Scope }))
	}), nil
}
