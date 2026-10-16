package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

// editorClosedMessage arrives when the editor popup closes. For a draft, onSaved gets the edited text.
type editorClosedMessage struct {
	err       error
	filePath  string
	onSaved   func(currentModel *model, text string)
	isStarter bool
}

// editContext opens a folder's context in $EDITOR, in a popup over the grid: its linked file when it has one,
// otherwise den's context.md, which starts with the folder's name as its heading.
func (currentModel *model) editContext(folderId string) tea.Cmd {
	name := currentModel.contextHeading(folderId)
	if folder := currentModel.repository.FindFolder(folderId); folder != nil && folder.LinkedPath != "" {
		return currentModel.editFile("◆ "+name+" · "+filepath.Base(folder.LinkedPath), folder.LinkedPath, "", nil)
	}
	contextPath, err := currentModel.repository.ContextPath(folderId)
	if currentModel.reportError(err) {
		return nil
	}
	return currentModel.editFile("◆ "+name+" · context.md", contextPath, "# "+name+"\n\n", nil)
}

// editLaptopFile opens one of the user's own rule files, which the tool loads in every repo.
func (currentModel *model) editLaptopFile(tool string) tea.Cmd {
	laptopFile, _ := currentModel.laptopFile(tool)
	return currentModel.editFile(filepath.Base(laptopFile.Path)+" · every repo", laptopFile.Path, "# My rules for every repo\n\n", nil)
}

// editDraft opens text that isn't saved anywhere yet, like a proposed context, and hands back what the user saved.
func (currentModel *model) editDraft(title string, text string, onSaved func(currentModel *model, text string)) tea.Cmd {
	dataDirectory, err := store.DataDirectory()
	if currentModel.reportError(err) {
		return nil
	}
	draftPath := filepath.Join(dataDirectory, "tmp", "draft.md")
	if currentModel.reportError(os.MkdirAll(filepath.Dir(draftPath), 0o755)) || currentModel.reportError(os.WriteFile(draftPath, []byte(text), 0o644)) {
		return nil
	}
	return currentModel.editFile(title, draftPath, "", onSaved)
}

// editFile runs $VISUAL or $EDITOR in a tmux popup and waits for it to exit. Without either, the file opens
// in the default text editor instead, and den waits until that editor quits. starter is written first when the file doesn't exist,
// and the file is removed again if it is left unchanged.
func (currentModel *model) editFile(title string, filePath string, starter string, onSaved func(currentModel *model, text string)) tea.Cmd {
	isStarter := false
	if _, err := os.Stat(filePath); os.IsNotExist(err) && starter != "" {
		if currentModel.reportError(os.MkdirAll(filepath.Dir(filePath), 0o755)) || currentModel.reportError(os.WriteFile(filePath, []byte(starter), 0o644)) {
			return nil
		}
		isStarter = true
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	workingDirectory := currentModel.workingDirectory
	return func() tea.Msg {
		closed := editorClosedMessage{filePath: filePath, onSaved: onSaved, isStarter: isStarter}
		if editor == "" {
			closed.err = exec.Command("open", "-W", "-t", filePath).Run()
			return closed
		}
		closed.err = tmux.DisplayPopup(title, workingDirectory, []string{"sh", "-c", editor + ` "$1"`, "sh", filePath})
		return closed
	}
}

func (currentModel model) afterEditorClosed(closed editorClosedMessage) (tea.Model, tea.Cmd) {
	if currentModel.reportError(closed.err) {
		return currentModel, nil
	}
	content, _ := os.ReadFile(closed.filePath)
	text := string(content)
	switch {
	case closed.isStarter && !hasNotesBeyondHeading(text):
		os.Remove(closed.filePath)
		currentModel.setStatus("nothing written, so no context")
	case closed.onSaved != nil:
		closed.onSaved(&currentModel, text)
	default:
		currentModel.setStatus("saved " + filepath.Base(closed.filePath))
	}
	currentModel.refreshContextTokens()
	return currentModel, nil
}

func hasNotesBeyondHeading(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") {
			return true
		}
	}
	return false
}

// contextHeading names whose context a folder id holds; the top level's is the repo's.
func (currentModel model) contextHeading(folderId string) string {
	if folderId == "" {
		return currentModel.repositoryName()
	}
	return currentModel.repository.FolderName(folderId)
}

// contextFileName names the file a context suggestion changes.
func (currentModel model) contextFileName(suggestion store.Suggestion) string {
	return currentModel.contextHeading(suggestion.FolderId) + " context"
}

// currentContextText is what a context suggestion would replace.
func (currentModel model) currentContextText(suggestion store.Suggestion) string {
	return currentModel.repository.ReadContext(suggestion.FolderId)
}

// displayPath shortens a path under the home directory to ~/…
func displayPath(path string) string {
	if homeDirectory, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, homeDirectory) {
		return "~" + strings.TrimPrefix(path, homeDirectory)
	}
	return path
}
