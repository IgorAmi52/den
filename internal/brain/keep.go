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
	// findingsSection is where kept findings go in a folder's notes.
	findingsSection = "Findings"
)

// keepRules tell the model what is worth keeping from a session. Sessions are archived a few days after their last use,
// so only what den keeps outlives them. The bar is high: every suggestion costs the user's attention.
const keepRules = "keep: whether this session produced something worth keeping after it ends. Sessions are archived a few days " +
	"after their last use, and only what you keep survives. The bar is high: every suggestion interrupts the user. kind is one of:\n" +
	"- none: the normal answer. Routine work, small fixes, a question answered, anything finished that nobody needs next week.\n" +
	"- notes: only for a session in a folder. It found or decided something whoever continues that folder's work needs: " +
	"a finding with its cause, a decision the user made or confirmed, a problem left open. lines: at most 3 short factual lines, " +
	"each clear on its own. Never implementation steps, file edits, or what the notes already say. Never a decision about how the " +
	"code or a tool should behave that the session itself went on to build: once built, the code says it, and a note would only go stale.\n" +
	"- folder: it started one piece of work that will clearly go on in more sessions: a feature, an investigation, a plan. " +
	"The work must be narrower than the repo itself and than the session's whole range: a long session that touched many parts of the repo " +
	"is not a folder, and neither is a topic named after where the session happened to start. " +
	"name: 1-3 words in title case naming the work. context: what a new session on this work needs that will still be true next week, " +
	"markdown starting with \"# <name>\", only sections you have facts for (## Goal, ## Findings, ## Decisions, ## Pointers), " +
	"under 150 words. Never progress reports, to-dos, what is or isn't implemented yet, or questions the user asked the agent. " +
	"file: the path of a plan or spec markdown file the session wrote that should be the context instead, from the files edited, or \"\".\n" +
	"- skill: the user set up a way of working they'll want again in other sessions, not a one-off instruction: " +
	"a mode like research without code changes, a review checklist, which sources to search and how to report. " +
	"The strongest sign is the user giving the same kind of setup instructions again, in this session or as an existing folder's sessions show. " +
	"name: 1-3 words naming the mode, like Exploration or Paper search. context: the instructions an agent follows in this mode, " +
	"markdown starting with \"# <name>\", under 120 words, only from what the user asked for.\n" +
	"Never propose a folder or skill an existing folder already covers. Use only facts from the session; never invent. " +
	"confidence: 0 to 1 that the user will say yes. reason: one sentence of at most 14 words the user reads first, " +
	"concise with a light, friendly touch but never cute, " +
	"naming the concrete thing worth keeping, like \"Pagination breaks past 500 issues; the fix is still open.\" Never generic. " +
	"evidence: for anything but none, 1-3 short quotes from the session that show it, each starting with its turn and speaker, " +
	"like \"turn 12, user: let's keep cursor paging\". No evidence in the session means kind none."

// ruleRules tell the model when a correction is a rule for every repo. The user can still move it into a folder instead.
const ruleRules = "rule: a rule for the user's own files that agents load in every repo (CLAUDE.md, AGENTS.md). Only from a clear " +
	"correction or preference the user stated about how agents should work that holds in any repo, like \"don't add comments\" or " +
	"\"never use abbreviations in names\". Not about this repo's code, this task, or a one-off \"not that file\". " +
	"rule: one short imperative sentence, or \"\" for none, the normal answer. evidence: the user's words, quoted. " +
	"confidence: 0 to 1 that it holds in every repo. Never a rule the laptop files already have."

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
		Evidence: []string{strings.TrimSpace(rule.Evidence)}}, "proposed"
}

// Keep is what the model answers about keeping something from a session.
type Keep struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Lines      []string `json:"lines"`
	Context    string   `json:"context"`
	File       string   `json:"file"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
}

func keepSchema() map[string]any {
	return objectSchema(map[string]any{
		"kind":       enumSchema("none", "notes", "folder", "skill"),
		"name":       stringSchema(),
		"lines":      arraySchema(stringSchema()),
		"context":    stringSchema(),
		"file":       stringSchema(),
		"confidence": map[string]any{"type": "number"},
		"reason":     stringSchema(),
		"evidence":   arraySchema(stringSchema()),
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
	base := store.Suggestion{SessionIds: []string{session.SessionId}, Source: session.Title, Reason: reason, Evidence: evidence}
	if keep.Kind == "notes" {
		return notesSuggestion(base, keep, session, repository)
	}
	name := strings.Trim(strings.TrimSpace(keep.Name), "\"'.")
	context := strings.TrimSpace(keep.Context)
	linkedPath := ""
	if file := strings.TrimSpace(keep.File); file != "" && keep.Kind == "folder" {
		for _, touched := range digest.TouchedFiles {
			candidate := absoluteIn(digest.WorkingDirectory, touched)
			if (candidate == file || strings.HasSuffix(candidate, "/"+strings.TrimPrefix(file, "./"))) && isNotesFile(repository, candidate) {
				linkedPath = candidate
			}
		}
	}
	switch {
	case name == "" || len([]rune(name)) > maximumFolderNameLength:
		return store.Suggestion{}, "dropped: no usable name"
	case folderNamed(repository, name) != nil:
		return store.Suggestion{}, "dropped: a folder named " + name + " exists"
	case context == "" && linkedPath == "":
		return store.Suggestion{}, "dropped: no context"
	}
	base.Kind, base.FolderName, base.ParentFolderId, base.Skill = store.SuggestFolder, name, session.FolderId, keep.Kind == "skill"
	base.LinkedPath = linkedPath
	if linkedPath == "" {
		base.ProposedText = context + "\n"
	}
	return base, "proposed"
}

// notesSuggestion adds the kept lines to den's notes of the session's folder, under Findings.
func notesSuggestion(base store.Suggestion, keep Keep, session store.Session, repository *store.Repository) (store.Suggestion, string) {
	folder := repository.FindFolder(session.FolderId)
	if folder == nil {
		return store.Suggestion{}, "dropped: notes need a folder"
	}
	current := repository.ReadContext(folder.Id)
	text := current
	if strings.TrimSpace(text) == "" {
		text = "# " + folder.Name + "\n"
	}
	added := 0
	for _, line := range keep.Lines[:min(len(keep.Lines), maximumKeepLines)] {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if line == "" || store.HasRule(text, line) || store.HasRule(store.ReadText(folder.LinkedPath), line) {
			continue
		}
		text = store.AddToSection(text, findingsSection, line)
		added++
	}
	if added == 0 {
		return store.Suggestion{}, "dropped: the notes already say it"
	}
	base.Kind, base.FolderId, base.ProposedText = store.SuggestContext, folder.Id, text
	return base, "proposed"
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
