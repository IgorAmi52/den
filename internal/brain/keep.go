package brain

import (
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	minimumKeepConfidence   = 0.7
	maximumEvidence         = 3
	maximumKeepLines        = 3
	maximumFolderNameLength = 30
)

// Rule is what the model answers about a rule for every repo.
type Rule struct {
	Rule       string  `json:"rule"`
	Evidence   string  `json:"evidence"`
	Confidence float64 `json:"confidence"`
}

func ruleSchema() map[string]any {
	return objectSchema(map[string]any{"rule": stringSchema(), "evidence": stringSchema(), "confidence": map[string]any{"type": "number"}})
}

// RuleSuggestion checks a rule the model found and offers it for the laptop files that don't say it yet,
// given as their path and text. outcome says what happened, for the decision log.
func RuleSuggestion(rule Rule, session store.Session, laptopTexts map[string]string) (suggestion store.Suggestion, outcome string) {
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rule.Rule), "- "))
	switch {
	case line == "":
		return store.Suggestion{}, "no rule"
	case strings.TrimSpace(rule.Evidence) == "":
		return store.Suggestion{}, "dropped: no evidence"
	case rule.Confidence < minimumKeepConfidence:
		return store.Suggestion{}, "withheld: not sure enough"
	}
	var filePaths []string
	for filePath, text := range laptopTexts {
		if !store.HasRule(text, line) {
			filePaths = append(filePaths, filePath)
		}
	}
	if len(filePaths) == 0 {
		return store.Suggestion{}, "dropped: the laptop files already say it"
	}
	slices.Sort(filePaths)
	reason := "You said: \"" + strings.Trim(strings.TrimSpace(rule.Evidence), "\"'“”") + "\""
	return store.Suggestion{Kind: store.SuggestRule, Rule: line, FilePaths: filePaths, SessionIds: []string{session.SessionId}, Source: session.Title, Reason: reason,
		FutureUse: "Future agents follow this explicit preference across repositories.", Evidence: []string{strings.TrimSpace(rule.Evidence)}}, "proposed"
}

// Keep is what the model answers about keeping something from a session.
type Keep struct {
	Destination  string `json:"destination"`
	FutureUse    string `json:"futureUse"`
	Replaces     string `json:"replaces"`
	ReplacesText string `json:"replacesText"`

	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Lines      []string `json:"lines"`
	Context    string   `json:"context"`
	File       string   `json:"file"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
	// Place is where a skill goes: here in the destination folder, inside it as a new folder, or a new top-level folder.
	Place string `json:"place"`
}

func keepSchema() map[string]any {
	return objectSchema(map[string]any{
		"destination":  stringSchema(),
		"futureUse":    stringSchema(),
		"replaces":     stringSchema(),
		"replacesText": stringSchema(),
		"kind":         enumSchema("none", "notes", "folder", "skill"),
		"name":         stringSchema(),
		"lines":        arraySchema(stringSchema()),
		"context":      stringSchema(),
		"file":         stringSchema(),
		"place":        enumSchema("", "here", "inside", "top"),
		"confidence":   map[string]any{"type": "number"},
		"reason":       stringSchema(),
		"evidence":     arraySchema(stringSchema()),
	})
}

// KeepSuggestion checks what the model wants to keep from a session and turns it into a suggestion.
// outcome says what happened, for the decision log: proposed, or why it was dropped.
func KeepSuggestion(keep Keep, session store.Session, digest agent.Digest, repository *store.Repository) (suggestion store.Suggestion, outcome string) {
	reason := strings.TrimSpace(keep.Reason)
	switch {
	case keep.Kind == "" || keep.Kind == "none":
		return store.Suggestion{}, "nothing to keep"
	case keep.Confidence < minimumKeepConfidence:
		return store.Suggestion{}, "withheld: not sure enough"
	case strings.TrimSpace(keep.FutureUse) == "":
		return store.Suggestion{}, "dropped: no concrete future use"
	case reason == "":
		return store.Suggestion{}, "dropped: no reason"
	case len(keep.Evidence) == 0:
		return store.Suggestion{}, "dropped: no evidence"
	}
	var evidence []string
	for _, quote := range keep.Evidence[:min(len(keep.Evidence), maximumEvidence)] {
		if quote = strings.TrimSpace(quote); quote != "" {
			evidence = append(evidence, quote)
		}
	}
	if len(evidence) == 0 {
		return store.Suggestion{}, "dropped: no evidence"
	}
	destination := strings.TrimSpace(keep.Destination)
	if destination == "repo" {
		destination = ""
	} else if repository.FindFolder(destination) == nil {
		return store.Suggestion{}, "dropped: unknown destination"
	}
	base := store.Suggestion{FolderId: destination, FutureUse: strings.TrimSpace(keep.FutureUse), SessionIds: []string{session.SessionId}, Source: session.Title, Reason: reason, Evidence: evidence}
	if keep.Kind == "notes" {
		return notesSuggestion(base, keep, repository)
	}
	name := strings.Trim(strings.TrimSpace(keep.Name), "\"'.")
	context := strings.TrimSpace(keep.Context)
	linkedPath := ""
	if file := strings.TrimSpace(keep.File); file != "" && keep.Kind == "folder" {
		for _, touched := range digest.TouchedFiles {
			candidate := absoluteIn(digest.WorkingDirectory, touched)
			if isSamePath(candidate, file) && isNotesFile(repository, candidate) {
				linkedPath = candidate
			}
		}
	}
	if file := strings.TrimSpace(keep.File); file != "" && keep.Kind == "skill" {
		for _, skillFile := range writtenSkillFiles(digest) {
			if isSamePath(skillFile, file) && !slices.ContainsFunc(repository.Folders, func(folder store.Folder) bool { return folder.LinkedPath == skillFile }) {
				linkedPath = skillFile
			}
		}
	}
	parentFolderId := destination
	if keep.Kind == "skill" {
		switch keep.Place {
		case "here":
			return skillHere(base, context, linkedPath, repository)
		case "top":
			parentFolderId = ""
		}
	}
	switch {
	case name == "" || len([]rune(name)) > maximumFolderNameLength:
		return store.Suggestion{}, "dropped: no usable name"
	case siblingNamed(repository, parentFolderId, name):
		return store.Suggestion{}, "dropped: a folder named " + name + " exists"
	case context == "" && linkedPath == "":
		return store.Suggestion{}, "dropped: no context"
	}
	base.Kind, base.FolderName, base.ParentFolderId, base.Skill = store.SuggestFolder, name, parentFolderId, keep.Kind == "skill"
	base.LinkedPath = linkedPath
	if linkedPath == "" {
		base.ProposedText = context + "\n"
	}
	return base, "proposed"
}

// skillHere makes a skill the context of the destination folder: its skill file linked, or the instructions den wrote.
func skillHere(base store.Suggestion, context string, linkedPath string, repository *store.Repository) (store.Suggestion, string) {
	folder := repository.FindFolder(base.FolderId)
	switch {
	case folder == nil:
		return store.Suggestion{}, "dropped: no folder to put the skill in"
	case strings.TrimSpace(repository.ReadContext(folder.Id)) != "":
		return store.Suggestion{}, "dropped: the folder already has context"
	case linkedPath != "":
		base.Kind, base.FolderId, base.LinkedPath = store.SuggestLink, folder.Id, linkedPath
	case context != "":
		base.Kind, base.FolderId, base.ProposedText = store.SuggestContext, folder.Id, context+"\n"
	default:
		return store.Suggestion{}, "dropped: no context"
	}
	return base, "proposed"
}

// isSamePath tells whether a file the model named, maybe relative, is this absolute path.
func isSamePath(absolutePath string, named string) bool {
	return absolutePath == named || strings.HasSuffix(absolutePath, "/"+strings.TrimPrefix(named, "./"))
}

// notesSuggestion adds the kept lines to the destination's notes, under Findings.
func notesSuggestion(base store.Suggestion, keep Keep, repository *store.Repository) (store.Suggestion, string) {
	base.Kind = store.SuggestContext
	for _, line := range keep.Lines[:min(len(keep.Lines), maximumKeepLines)] {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line != "" {
			base.AddedLines = append(base.AddedLines, line)
		}
	}
	if len(base.AddedLines) == 0 {
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
