package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type InstructionDraft struct {
	Instruction    Instruction `json:"instruction"`
	SourceRevision string      `json:"sourceRevision"`
}

func (repository Repository) instructionDraftPath(identifier string) (string, error) {
	directory, failure := repository.Directory()
	return filepath.Join(directory, "instruction-drafts", ContextVersion(identifier)+".json"), failure
}

func (repository Repository) SaveInstructionDraft(draft InstructionDraft) error {
	path, failure := repository.instructionDraftPath(draft.Instruction.Identifier)
	if failure != nil {
		return failure
	}
	content, failure := json.Marshal(draft)
	if failure != nil {
		return failure
	}
	return WriteText(path, string(content))
}

func (repository Repository) RemoveInstructionDraft(identifier string) error {
	path, failure := repository.instructionDraftPath(identifier)
	if failure != nil {
		return failure
	}
	failure = os.Remove(path)
	if os.IsNotExist(failure) {
		return nil
	}
	return failure
}

func (repository Repository) InstructionDrafts() (map[string]InstructionDraft, error) {
	directory, failure := repository.Directory()
	if failure != nil {
		return nil, failure
	}
	paths, failure := filepath.Glob(filepath.Join(directory, "instruction-drafts", "*.json"))
	if failure != nil {
		return nil, failure
	}
	drafts := map[string]InstructionDraft{}
	for _, path := range paths {
		content, failure := os.ReadFile(path)
		if failure != nil {
			return nil, failure
		}
		var draft InstructionDraft
		if failure := json.Unmarshal(content, &draft); failure != nil {
			return nil, failure
		}
		drafts[draft.Instruction.Identifier] = draft
	}
	return drafts, nil
}
