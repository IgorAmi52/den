package brain

import (
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

const (
	minimumKeepConfidence   = 0.8
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
	"each clear on its own. Never implementation steps, file edits, or what the notes already say.\n" +
	"- folder: it started a piece of work that will clearly go on in more sessions: a feature, an investigation, a plan. " +
	"name: 1-3 words in title case naming the work. context: what a new session on this work needs, markdown starting " +
	"with \"# <name>\", only sections you have facts for (## Goal, ## Findings, ## Decisions, ## Open questions, ## Pointers), " +
	"under 150 words. file: the path of a plan or spec markdown file the session wrote that should be the context instead, from the files edited, or \"\".\n" +
	"- skill: the user set up a way of working they'll want again in other sessions, not a one-off instruction: " +
	"a mode like research without code changes, a review checklist, which sources to search and how to report. " +
	"name: 1-3 words naming the mode, like Exploration or Paper search. context: the instructions an agent follows in this mode, " +
	"markdown starting with \"# <name>\", under 120 words, only from what the user asked for.\n" +
	"Never propose a folder or skill an existing folder already covers. Use only facts from the session; never invent. " +
	"confidence: 0 to 1 that the user will say yes. reason: one sentence of at most 14 words the user reads first, " +
	"naming the concrete thing worth keeping, like \"Pagination breaks past 500 issues; the fix is still open.\" Never generic."

// Keep is what the model answers about keeping something from a session.
type Keep struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Lines      []string `json:"lines"`
	Context    string   `json:"context"`
	File       string   `json:"file"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
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
	}
	base := store.Suggestion{SessionIds: []string{session.SessionId}, Source: session.Title, Reason: reason}
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
