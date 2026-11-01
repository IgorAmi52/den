package brain

import (
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	minimumKeepConfidence   = 0.7
	maximumEvidence         = 3
	maximumFolderNameLength = 30
)

// Keep is what the model answers about keeping something from a session.
type Keep struct {
	InstructionIdentifier string `json:"instructionIdentifier"`
	InstructionRevision   string `json:"instructionRevision"`
	ReadVersion           string `json:"readVersion"`
	Destination           string `json:"destination"`
	Replaces              string `json:"replaces"`
	ReplacesText          string `json:"replacesText"`

	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Lines      []string `json:"lines"`
	Context    string   `json:"context"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
}

func keepSchema() map[string]any {
	return objectSchema(map[string]any{
		"destination":           stringSchema(),
		"instructionIdentifier": stringSchema(),
		"instructionRevision":   stringSchema(),
		"readVersion":           stringSchema(),
		"replaces":              stringSchema(),
		"replacesText":          stringSchema(),
		"kind":                  enumSchema("none", "notes", "folder", "rule"),
		"name":                  stringSchema(),
		"lines":                 arraySchema(stringSchema()),
		"context":               stringSchema(),
		"confidence":            map[string]any{"type": "number"},
		"reason":                stringSchema(),
		"evidence":              arraySchema(stringSchema()),
	})
}

// KeepSuggestion checks what the model wants to keep from a session and turns it into a suggestion.
// outcome says what happened, for the decision log: proposed, or why it was dropped.
func KeepSuggestion(keep Keep, session store.Session, repository *store.Repository, sourceEvidence string) (suggestion store.Suggestion, outcome string) {
	instructions, failure := repository.ApplicableInstructions(session.FolderId)
	if failure != nil {
		return store.Suggestion{}, "dropped: invalid instruction file"
	}
	instructionPosition := slices.IndexFunc(instructions, func(instruction store.Instruction) bool {
		return instruction.Identifier == keep.InstructionIdentifier && instruction.Revision() == keep.InstructionRevision
	})
	if instructionPosition < 0 {
		return store.Suggestion{}, "dropped: inactive, changed or unknown instruction"
	}
	reason := strings.TrimSpace(keep.Reason)
	switch {
	case keep.Kind == "" || keep.Kind == "none":
		return store.Suggestion{}, "nothing to keep"
	case keep.Confidence < minimumKeepConfidence:
		return store.Suggestion{}, "withheld: not sure enough"
	case reason == "":
		return store.Suggestion{}, "dropped: no reason"
	case len(keep.Evidence) == 0:
		return store.Suggestion{}, "dropped: no evidence"
	}
	var evidence []string
	for _, quote := range keep.Evidence[:min(len(keep.Evidence), maximumEvidence)] {
		if quote = strings.TrimSpace(quote); quote != "" {
			if !strings.Contains(strings.Join(strings.Fields(sourceEvidence), " "), strings.Join(strings.Fields(quote), " ")) {
				return store.Suggestion{}, "dropped: evidence must quote the supplied source exchanges"
			}
			evidence = append(evidence, quote)
		}
	}
	if len(evidence) == 0 {
		return store.Suggestion{}, "dropped: no evidence"
	}
	if keep.Kind == "rule" {
		if keep.Destination != "laptop" || len(keep.Lines) != 1 || strings.TrimSpace(keep.Lines[0]) == "" || strings.ContainsAny(keep.Lines[0], "\r\n") {
			return store.Suggestion{}, "dropped: a global rule needs one line and destination laptop"
		}
		suggestion := store.Suggestion{
			Kind: store.SuggestRule, Rule: strings.TrimSpace(keep.Lines[0]), FilePaths: GlobalRulePaths(),
			InstructionIdentifier: keep.InstructionIdentifier, InstructionRevision: keep.InstructionRevision,
			InstructionTitle: instructions[instructionPosition].Title, ContextReadVersion: keep.ReadVersion,
			SessionIds: []string{session.SessionId}, Source: session.Title, Reason: reason, Evidence: evidence,
		}
		if keep.ReadVersion != repository.SuggestionReadVersion(suggestion) {
			return store.Suggestion{}, "dropped: global rule files were not read or have changed"
		}
		if failure := repository.PrepareSuggestion(&suggestion); failure != nil {
			return store.Suggestion{}, "dropped: " + failure.Error()
		}
		return suggestion, "proposed"
	}
	if keep.Kind != "notes" && keep.Kind != "folder" {
		return store.Suggestion{}, "dropped: unknown proposal kind"
	}
	destination := strings.TrimSpace(keep.Destination)
	if destination == "repo" {
		destination = ""
	} else if repository.FindFolder(destination) == nil {
		return store.Suggestion{}, "dropped: unknown destination"
	}
	if keep.ReadVersion != repository.ContextReadVersion(destination) {
		return store.Suggestion{}, "dropped: destination and ancestors were not read or have changed"
	}
	base := store.Suggestion{InstructionIdentifier: keep.InstructionIdentifier, InstructionRevision: keep.InstructionRevision, InstructionTitle: instructions[instructionPosition].Title, ContextReadVersion: keep.ReadVersion, FolderId: destination, SessionIds: []string{session.SessionId}, Source: session.Title, Reason: reason, Evidence: evidence}
	if keep.Kind == "notes" {
		return notesSuggestion(base, keep, repository)
	}
	name := strings.Trim(strings.TrimSpace(keep.Name), "\"'.")
	context := strings.TrimSpace(keep.Context)
	parentFolderId := destination
	switch {
	case name == "" || len([]rune(name)) > maximumFolderNameLength:
		return store.Suggestion{}, "dropped: no usable name"
	case siblingNamed(repository, parentFolderId, name):
		return store.Suggestion{}, "dropped: a folder named " + name + " exists"
	case context == "":
		return store.Suggestion{}, "dropped: no context"
	}
	base.Kind, base.FolderName, base.ParentFolderId = store.SuggestFolder, name, parentFolderId
	base.ProposedText = context + "\n"
	return base, "proposed"
}

// notesSuggestion adds the kept lines to the destination's notes, under Findings.
func notesSuggestion(base store.Suggestion, keep Keep, repository *store.Repository) (store.Suggestion, string) {
	base.Kind = store.SuggestContext
	for _, line := range keep.Lines {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line != "" {
			base.AddedLines = append(base.AddedLines, line)
		}
	}
	if len(base.AddedLines) == 0 && keep.ReplacesText == "" {
		return store.Suggestion{}, "dropped: no notes"
	}
	if keep.ReplacesText != "" {
		current := repository.ReadContext(base.FolderId)
		if strings.Count(current, keep.ReplacesText) != 1 {
			return store.Suggestion{}, "dropped: correction must identify one exact destination passage"
		}
		base.ProposedText = strings.Replace(current, keep.ReplacesText, strings.Join(base.AddedLines, "\n"), 1)
		base.AddedLines = nil
		base.ContextVersion = store.ContextVersion(current)
	}
	if err := repository.PrepareSuggestion(&base); err != nil {
		return store.Suggestion{}, "dropped: " + err.Error()
	}
	return base, "proposed"
}

func siblingNamed(repository *store.Repository, parent string, name string) bool {
	for _, folder := range repository.Folders {
		if folder.ParentId == parent && strings.EqualFold(folder.Name, name) {
			return true
		}
	}
	return false
}

// folderNamed finds a folder anywhere in the tree by name, ignoring case.
func folderNamed(repository *store.Repository, name string) *store.Folder {
	for index := range repository.Folders {
		if strings.EqualFold(repository.Folders[index].Name, name) {
			return &repository.Folders[index]
		}
	}
	return nil
}
