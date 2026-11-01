package store

import (
	"slices"
	"strings"
)

func StarterInstructions() []Instruction {
	instructions := []Instruction{
		{
			Identifier: "starter-issues", Title: "Track issues",
			Text: "Catch reported or suspected problems mentioned in my sessions and keep their status current. Merge repeated reports and update existing findings when a problem is fixed or ruled out. Choose a folder whose purpose fits the issue; do not assume it belongs in shared repo context.",
			Examples: []string{
				"Concurrent writes sometimes overwrite each other and lose updates.",
				"The previously reported overwrite issue is now fixed.",
				"The suspected memory leak was a false alarm; the measurements included the cache.",
			},
		},
		{
			Identifier: "starter-deferred-work", Title: "Track postponed work",
			Text: "Keep track of work we explicitly decide to leave for later, and update it when we finish, cancel, or change it. Distinguish an agreed postponement from an ordinary next step or an unaccepted suggestion.",
			Examples: []string{
				"Let's leave keyboard navigation for later; ship the mouse controls first.",
				"We finished the migration we had postponed last week.",
			},
		},
		{
			Identifier: "starter-workflows", Title: "Preserve reusable workflows",
			Text: "Notice workflows I may want to repeat and propose preserving their agreed steps, inputs, constraints, and checks in a reusable folder. Check existing context, omit duplicates, and update saved workflows when we change them. Keep the workflow usable by a future session, without performing the work or moving the current session.",
			Examples: []string{
				"When I ask for the weekly customer review, collect new feedback, group repeated problems, and prepare a summary with source links.",
				"We changed our vendor-comparison workflow: include a security review before shortlisting candidates.",
			},
		},
		{
			Identifier: "starter-global-agent-rules", Title: "Remember global agent preferences",
			Text: "When I explicitly state a coding-agent preference that should apply across projects, propose adding it to my global CLAUDE.md and AGENTS.md files. Check the existing rules for duplicates and conflicts. Keep project-specific instructions in their appropriate repo or folder. Show the exact addition and affected files for approval before writing.",
			Examples: []string{
				"Across all my projects, do not create a git commit unless I explicitly ask for one.",
				"Whenever an agent changes code in any repo, I want it to explain how the change was verified.",
			},
		},
	}
	for position := range instructions {
		instructions[position].Scope = "repo"
		instructions[position].ExamplesRevision = instructions[position].BehaviorVersion()
	}
	return instructions
}

func (repository Repository) AvailableStarterInstructions() ([]Instruction, error) {
	instructions, failure := repository.Instructions()
	if failure != nil {
		return nil, failure
	}
	return slices.DeleteFunc(StarterInstructions(), func(starter Instruction) bool {
		return slices.ContainsFunc(instructions, func(instruction Instruction) bool {
			return instruction.Identifier == starter.Identifier || strings.EqualFold(instruction.Title, starter.Title) || normalizedWords(instruction.Text) == normalizedWords(starter.Text)
		})
	}), nil
}
