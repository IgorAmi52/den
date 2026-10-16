package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Guides describe how this user writes commits, pull requests and issues in this repo.
// Agents read the matching guide before writing one, and den suggests edits to them.
var Guides = []string{"commit", "pull-request", "issue"}

// repositoryContextFolder holds the context for the whole repo; folder ids are hex, so it can't clash with one.
const repositoryContextFolder = "repo"

// ContextPath is the private context.md of a folder; the top level holds the context for the whole repo.
// Each lives in its own directory, so an agent allowed to edit one can't touch den's other files.
func (repository Repository) ContextPath(folderId string) (string, error) {
	directory, err := repository.Directory()
	if err != nil {
		return "", err
	}
	if folderId == "" {
		folderId = repositoryContextFolder
	}
	return filepath.Join(directory, "folders", folderId, "context.md"), nil
}

func (repository Repository) ReadContext(folderId string) string {
	filePath, err := repository.ContextPath(folderId)
	if err != nil {
		return ""
	}
	return readText(filePath)
}

func (repository Repository) WriteContext(folderId string, text string) error {
	filePath, err := repository.ContextPath(folderId)
	if err != nil {
		return err
	}
	return writeText(filePath, text)
}

func (repository Repository) GuidePath(guide string) (string, error) {
	directory, err := repository.Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "guides", guide+".md"), nil
}

func (repository Repository) ReadGuide(guide string) string {
	filePath, err := repository.GuidePath(guide)
	if err != nil {
		return ""
	}
	return readText(filePath)
}

func (repository Repository) WriteGuide(guide string, text string) error {
	filePath, err := repository.GuidePath(guide)
	if err != nil {
		return err
	}
	return writeText(filePath, text)
}

// MergedContext is what a session launched in the folder starts with: the repo's context,
// then each folder's from the top down, then where those files and the guides live, so the agent can edit them.
// writableDirectories lists the directories the agent may write to without asking,
// and hasNotes tells whether any layer has notes yet.
func (repository *Repository) MergedContext(folderId string) (text string, writableDirectories []string, hasNotes bool) {
	var notes, locations []string
	addLayer := func(heading string, layerFolderId string) {
		contextPath, err := repository.ContextPath(layerFolderId)
		if err != nil {
			return
		}
		os.MkdirAll(filepath.Dir(contextPath), 0o755)
		writableDirectories = append(writableDirectories, filepath.Dir(contextPath))
		locations = append(locations, fmt.Sprintf("- %s: %s", heading, contextPath))
		if content := strings.TrimSpace(readText(contextPath)); content != "" {
			notes = append(notes, "## "+heading+"\n\n"+content)
		}
	}
	addLayer(filepath.Base(repository.RepositoryPath)+" (whole repo)", "")
	for _, folder := range repository.FolderPath(folderId) {
		addLayer(folder.Name, folder.Id)
	}
	var guideLocations []string
	for _, guide := range Guides {
		if guidePath, err := repository.GuidePath(guide); err == nil && readText(guidePath) != "" {
			guideLocations = append(guideLocations, fmt.Sprintf("- %s: %s", guide, guidePath))
			if !slices.Contains(writableDirectories, filepath.Dir(guidePath)) {
				writableDirectories = append(writableDirectories, filepath.Dir(guidePath))
			}
		}
	}
	var sections []string
	sections = append(sections, "# Private notes from den\n\nden organizes this user's agent sessions and keeps private notes outside the repo. The user wrote or approved everything below.")
	sections = append(sections, notes...)
	sections = append(sections, "## Where the notes live\n\n"+strings.Join(locations, "\n")+
		"\n\nWhen the user asks you to remember something for one of these (\"remember for "+lastHeading(repository, folderId)+": ...\"), edit that file. Keep it short and factual.")
	if len(guideLocations) > 0 {
		sections = append(sections, "## Guides\n\nBefore you write a commit message, a pull request or an issue, read the matching guide and follow it:\n"+
			strings.Join(guideLocations, "\n")+"\n\nWhen the user asks to change how one of these is written, edit the guide.")
	}
	return strings.Join(sections, "\n\n"), writableDirectories, len(notes) > 0
}

// EstimatedTokens is a rough count for showing what a context costs; about four characters make a token.
func EstimatedTokens(text string) int {
	return (len(text) + 3) / 4
}

func lastHeading(repository *Repository, folderId string) string {
	if folder := repository.FindFolder(folderId); folder != nil {
		return folder.Name
	}
	return filepath.Base(repository.RepositoryPath)
}

func readText(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return string(content)
}

func writeText(filePath string, text string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	temporaryFilePath := filePath + ".tmp"
	if err := os.WriteFile(temporaryFilePath, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryFilePath, filePath)
}
