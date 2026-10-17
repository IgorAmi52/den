package brain

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// minimumSkillSessions is in how many of this repo's sessions a skill of the user's has to run before den offers it a folder.
const minimumSkillSessions = 3

// skillFolders offers a skill folder for a skill or slash command the user keeps running, with the skill's own file
// copied as its context, so den never has to write the instructions itself.
func skillFolders(observation Observation) []store.Suggestion {
	repository := observation.Repository
	sessionsUsing := map[string]int{}
	for _, digest := range observation.Digests {
		for _, name := range slices.Compact(slices.Sorted(slices.Values(digest.UsedSkills))) {
			sessionsUsing[name]++
		}
	}
	var names []string
	for name, count := range sessionsUsing {
		if count >= minimumSkillSessions {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var suggestions []store.Suggestion
	for _, name := range names {
		folderName := skillFolderName(name)
		if slices.ContainsFunc(repository.Folders, func(folder store.Folder) bool { return strings.EqualFold(folder.Name, folderName) }) {
			continue
		}
		skillPath, found := agent.SkillFile(name, repository.RepositoryPath)
		if !found {
			continue
		}
		text, err := os.ReadFile(skillPath)
		if err != nil || strings.TrimSpace(string(text)) == "" {
			continue
		}
		suggestion := store.Suggestion{
			Kind:         store.SuggestFolder,
			FolderName:   folderName,
			Skill:        true,
			ProposedText: string(text),
			Source:       "your " + name + " skill",
			Reason:       fmt.Sprintf("You've used %s in %d sessions here; a folder starts each new one with it.", name, sessionsUsing[name]),
		}
		if !repository.IsRejected(suggestion) {
			suggestions = append(suggestions, suggestion)
		}
	}
	return suggestions
}

// skillFolderName turns a skill's name, like paper-search, into a folder's, like Paper Search.
func skillFolderName(name string) string {
	parts := strings.FieldsFunc(name, func(character rune) bool { return character == '-' || character == '_' })
	for index, part := range parts {
		parts[index] = capitalizedWord(part)
	}
	return truncateText(strings.Join(parts, " "), maximumFolderNameLength)
}

func capitalizedWord(word string) string {
	if word == "" {
		return word
	}
	return strings.ToUpper(word[:1]) + word[1:]
}
