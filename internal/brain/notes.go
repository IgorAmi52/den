package brain

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	// minimumNotesFileSize keeps scratch files out: a file worth linking is a real plan or spec.
	minimumNotesFileSize = 800
)

// notNotesFiles are markdown files every repo has, and the agents' own skill and memory files,
// which are never a folder's notes.
var notNotesFiles = toSet("readme.md changelog.md claude.md agents.md license.md contributing.md skill.md memory.md")

func absoluteIn(directory string, filePath string) string {
	if filepath.IsAbs(filePath) {
		return filePath
	}
	return filepath.Join(directory, filePath)
}

func isNotesFile(repository *store.Repository, filePath string) bool {
	if !strings.EqualFold(filepath.Ext(filePath), ".md") || notNotesFiles[strings.ToLower(filepath.Base(filePath))] {
		return false
	}
	// Claude keeps its memories as markdown files in a memory directory of each project.
	if slices.Contains(strings.Split(filepath.Dir(filePath), string(filepath.Separator)), "memory") {
		return false
	}
	if dataDirectory, err := store.DataDirectory(); err == nil && strings.HasPrefix(filePath, dataDirectory) {
		return false
	}
	if slices.ContainsFunc(repository.Folders, func(folder store.Folder) bool { return folder.LinkedPath == filePath }) {
		return false
	}
	information, err := os.Stat(filePath)
	return err == nil && !information.IsDir() && information.Size() >= minimumNotesFileSize
}

func truncateText(text string, maximumLength int) string {
	runes := []rune(text)
	if len(runes) <= maximumLength {
		return text
	}
	return string(runes[:maximumLength-1]) + "…"
}
