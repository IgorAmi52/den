package brain

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// minimumSkillUses is how often a skill of the user's has to run in this repo's sessions before den offers it a folder.
const minimumSkillUses = 3

// skillFolders offers a skill folder for a skill or slash command the user keeps running, with the skill's own file
// copied as its context, so den never has to write the instructions itself.
func skillFolders(observation Observation) []store.Suggestion {
	repository := observation.Repository
	uses := map[string]int{}
	for _, digest := range observation.Digests {
		for _, name := range digest.UsedSkills {
			uses[name]++
		}
	}
	var names []string
	for name, count := range uses {
		if count >= minimumSkillUses {
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
			Source:       "/" + name,
			Reason:       fmt.Sprintf("You've run /%s %d times here; a folder would start every session in it.", name, uses[name]),
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
