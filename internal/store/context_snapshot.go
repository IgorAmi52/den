package store

import (
	"fmt"
	"slices"
)

func RuleFilesSnapshot(filePaths []string) string {
	paths := slices.Clone(filePaths)
	slices.Sort(paths)
	text := "GLOBAL CODING-AGENT RULES\n"
	for _, path := range paths {
		text += fmt.Sprintf("\nFILE %s\n%s", path, ReadText(path))
	}
	return "readVersion: " + ContextVersion(text) + "\n" + text
}

func (repository Repository) SuggestionReadVersion(suggestion Suggestion) string {
	if suggestion.Kind == SuggestRule {
		var version string
		fmt.Sscanf(RuleFilesSnapshot(suggestion.FilePaths), "readVersion: %s", &version)
		return version
	}
	destination := suggestion.FolderId
	if suggestion.Kind == SuggestFolder {
		destination = suggestion.ParentFolderId
	}
	return repository.ContextReadVersion(destination)
}

func (repository Repository) ContextSnapshot(folderIdentifier string) string {
	text := "REPO CONTEXT\n" + repository.ReadContext("")
	for _, folder := range repository.FolderPath(folderIdentifier) {
		text += fmt.Sprintf("\nFOLDER %s (%s)\n%s", folder.Id, repository.ContextDestination(folder.Id), repository.ReadContext(folder.Id))
	}
	return "readVersion: " + ContextVersion(text) + "\n" + text
}

func (repository Repository) ContextReadVersion(folderIdentifier string) string {
	var version string
	fmt.Sscanf(repository.ContextSnapshot(folderIdentifier), "readVersion: %s", &version)
	return version
}
