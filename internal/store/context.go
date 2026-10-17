package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// repositoryContextFolder holds the context for the whole repo; folder ids are hex, so it can't clash with one.
const repositoryContextFolder = "repo"

const (
	// fullContextTokens is the most of one file a session starts with; past it, the agent gets the start and the path.
	fullContextTokens = 2000
	// firstSectionLines caps the start of a long file.
	firstSectionLines = 40
)

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

// FolderContextPath is the one file a folder's context lives in: the file linked to it, or else den's context.md,
// which den creates when it first writes context for the folder. The top level's is the repo's context.md.
func (repository Repository) FolderContextPath(folderId string) (string, error) {
	if folder := repository.FindFolder(folderId); folder != nil && folder.LinkedPath != "" {
		return folder.LinkedPath, nil
	}
	return repository.ContextPath(folderId)
}

// IsLinked tells whether a folder's context is a file of the user's rather than den's own.
func (repository Repository) IsLinked(folderId string) bool {
	folder := repository.FindFolder(folderId)
	return folder != nil && folder.LinkedPath != ""
}

// ReadContext reads a folder's context, from whichever file it lives in.
func (repository Repository) ReadContext(folderId string) string {
	filePath, err := repository.FolderContextPath(folderId)
	if err != nil {
		return ""
	}
	return ReadText(filePath)
}

// WriteContext writes a folder's context to whichever file it lives in.
func (repository Repository) WriteContext(folderId string, text string) error {
	filePath, err := repository.FolderContextPath(folderId)
	if err != nil {
		return err
	}
	return WriteText(filePath, text)
}

// contextLayer is one file a session starts with, under a heading that names it.
type contextLayer struct {
	heading  string
	filePath string
	text     string
	// isDens marks den's own files, which the agent may edit without a permission prompt.
	isDens bool
}

func (repository *Repository) contextLayers(folderId string) []contextLayer {
	var layers []contextLayer
	addLayer := func(heading string, filePath string, isDens bool) {
		if text := strings.TrimSpace(ReadText(filePath)); text != "" {
			layers = append(layers, contextLayer{heading: heading, filePath: filePath, text: text, isDens: isDens})
		}
	}
	if contextPath, err := repository.ContextPath(""); err == nil {
		addLayer(filepath.Base(repository.RepositoryPath)+", whole repo", contextPath, true)
	}
	for _, folder := range repository.FolderPath(folderId) {
		if contextPath, err := repository.FolderContextPath(folder.Id); err == nil {
			addLayer(folder.Name, contextPath, folder.LinkedPath == "")
		}
	}
	return layers
}

// MergedContext is what a session launched in the folder starts with: the repo's context, then each folder's
// from the top down, each under a heading naming its file. With no context anywhere, it is empty, so nothing is added.
// The agent is never told about den; it only gets the notes. writableDirectories are the directories it may edit
// without asking: den's own files, and linked files outside the repo.
func (repository *Repository) MergedContext(folderId string) (text string, writableDirectories []string, hasNotes bool) {
	layers := repository.contextLayers(folderId)
	if len(layers) == 0 {
		return "", nil, false
	}
	sections := []string{"# Notes for this work"}
	for _, layer := range layers {
		body := layer.text
		if EstimatedTokens(body) > fullContextTokens {
			body = firstSection(body) + "\n\n(This is the start. Read the rest of " + layer.filePath + " before you begin.)"
		}
		sections = append(sections, "## "+layer.heading+" · "+layer.filePath+"\n\n"+body)
		directory := filepath.Dir(layer.filePath)
		if (layer.isDens || !isInside(repository.RepositoryPath, layer.filePath)) && !slices.Contains(writableDirectories, directory) {
			writableDirectories = append(writableDirectories, directory)
		}
	}
	sections = append(sections, "If the user asks to update these notes, edit the file named in its heading. Keep it short and factual.")
	return strings.Join(sections, "\n\n"), writableDirectories, true
}

// firstSection is the start of a long file: up to its second heading, at most firstSectionLines lines.
func firstSection(text string) string {
	var kept []string
	hasContent := false
	for _, line := range strings.Split(text, "\n") {
		isHeading := strings.HasPrefix(line, "#")
		if isHeading && hasContent {
			break
		}
		if !isHeading && strings.TrimSpace(line) != "" {
			hasContent = true
		}
		kept = append(kept, line)
		if len(kept) == firstSectionLines {
			break
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// AppendRule adds a rule as a bullet at the end of a file, creating the file when it's missing,
// unless the file already says it. It reports whether it added the rule.
func AppendRule(filePath string, rule string) (bool, error) {
	text := ReadText(filePath)
	if HasRule(text, rule) {
		return false, nil
	}
	if trimmedText := strings.TrimRight(text, "\n"); trimmedText != "" {
		lines := strings.Split(trimmedText, "\n")
		separator := "\n\n"
		if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "- ") {
			separator = "\n"
		}
		text = trimmedText + separator
	}
	return true, WriteText(filePath, text+"- "+rule)
}

// AddToSection adds a bullet under a "## section" heading, at the end of that section,
// and adds the heading at the end of the text when it's missing.
func AddToSection(text string, section string, line string) string {
	heading, bullet := "## "+section, "- "+line
	trimmedText := strings.TrimRight(text, "\n")
	if strings.TrimSpace(trimmedText) == "" {
		return heading + "\n" + bullet + "\n"
	}
	lines := strings.Split(trimmedText, "\n")
	headingIndex := slices.IndexFunc(lines, func(candidate string) bool {
		return strings.EqualFold(strings.TrimSpace(candidate), heading)
	})
	if headingIndex < 0 {
		return trimmedText + "\n\n" + heading + "\n" + bullet + "\n"
	}
	sectionEnd := headingIndex + 1
	for sectionEnd < len(lines) && !strings.HasPrefix(lines[sectionEnd], "#") {
		sectionEnd++
	}
	for sectionEnd > headingIndex+1 && strings.TrimSpace(lines[sectionEnd-1]) == "" {
		sectionEnd--
	}
	return strings.Join(slices.Insert(lines, sectionEnd, bullet), "\n") + "\n"
}

// HasRule tells whether text already says rule, ignoring case and spacing.
func HasRule(text string, rule string) bool {
	return strings.Contains(normalizedWords(text), normalizedWords(rule))
}

func normalizedWords(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

// EstimatedTokens is a rough count for showing what a context costs; about four characters make a token.
func EstimatedTokens(text string) int {
	return (len(text) + 3) / 4
}

func isInside(directory string, filePath string) bool {
	relativePath, err := filepath.Rel(directory, filePath)
	return err == nil && !strings.HasPrefix(relativePath, "..")
}

// ReadText reads a file, or returns nothing when it's missing.
func ReadText(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return string(content)
}

// WriteText writes a file through a temporary one, so a crash never leaves it half written.
func WriteText(filePath string, text string) error {
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
