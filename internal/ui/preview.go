package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/IgorAmi52/den/internal/store"
)

// guideUses says when agents read each guide.
var guideUses = map[string]string{
	"commit":       "Agents read it before they write a commit message.",
	"pull-request": "Agents read it before they open or describe a pull request.",
	"issue":        "Agents read it before they write an issue.",
}

func (currentModel model) previewColumnLines(width int, height int) []string {
	innerWidth := width - 2
	var header string
	var body []string
	if currentModel.organize.focus == itemsColumn {
		if item, exists := currentModel.selectedItem(); exists {
			header, body = currentModel.itemPreview(item, innerWidth)
		}
	} else {
		header, body = currentModel.entryPreview(currentModel.selectedEntry(), innerWidth)
	}
	offset := min(currentModel.organize.previewOffset, max(len(body)-1, 0))
	lines := []string{currentModel.theme.columnHeader.Render(" " + header)}
	for _, line := range body[offset:] {
		lines = append(lines, " "+line)
	}
	if offset > 0 {
		lines[0] = currentModel.layoutRow([]segment{{text: " " + header, style: currentModel.theme.columnHeader}},
			[]segment{{text: "↑ more ", style: currentModel.theme.faint}}, width, false)
	}
	fitted := fitLines(strings.Join(lines, "\n"), width, height)
	if len(lines) > height {
		fitted[height-1] = currentModel.theme.faint.Render(padRight(" ↓ more · J/K scroll", width))
	}
	return fitted
}

func (currentModel model) entryPreview(entry organizeEntry, width int) (string, []string) {
	theme := currentModel.theme
	switch entry.kind {
	case reviewEntry:
		count := len(currentModel.repository.Suggestions)
		if count == 0 {
			return "REVIEW", wrapAll([]string{
				theme.dim.Render("Nothing waiting."),
				"",
				theme.dim.Render("den suggests where sessions go, ⎇ subfolders for branches, notes for context.md, and cleanups. Every suggestion waits here until you say yes or no."),
			}, width)
		}
		lines := []string{theme.suggestion.Render(fmt.Sprintf("✦ %d waiting", count)), ""}
		for _, suggestion := range currentModel.repository.Suggestions {
			label, summary := currentModel.suggestionSummary(suggestion)
			lines = append(lines, theme.suggestion.Render(padRight(label, 10))+truncate(summary, width-10))
		}
		lines = append(lines, "", theme.dim.Render("↵ goes through them one at a time, like git add -p: y yes, n no, e edit, s skip."))
		return "REVIEW", wrapAll(lines, width)
	case repositoryEntry:
		return "CONTEXT", currentModel.contextFilePreview("", width)
	case inboxEntry:
		return "INBOX", wrapAll([]string{
			theme.dim.Render("Sessions that aren't filed yet. When den knows where one belongs, the row shows it in faint text, and ⇥ files it."),
			"",
			theme.dim.Render("Sessions here start with the repo's context only."),
		}, width)
	case archiveEntry:
		return "ARCHIVE", wrapAll([]string{theme.dim.Render("Archived sessions stay resumable: ↵ opens one, a brings it back.")}, width)
	}
	return "CONTEXT", currentModel.folderContextPreview(entry.folder, width)
}

func (currentModel model) folderContextPreview(folder *store.Folder, width int) []string {
	theme := currentModel.theme
	contextPath, _ := currentModel.repository.ContextPath(folder.Id)
	text := currentModel.repository.ReadContext(folder.Id)
	var lines []string
	inherits := []string{currentModel.repositoryName()}
	for _, ancestor := range currentModel.repository.FolderPath(folder.ParentId) {
		inherits = append(inherits, ancestor.Name)
	}
	if strings.TrimSpace(text) == "" {
		lines = append(lines, theme.bold.Render(folder.Name), "", theme.dim.Render("No context yet. Sessions here start with the context of "+strings.Join(inherits, " and ")+"."), "")
		lines = append(lines, theme.key.Render("e")+theme.action.Render(" writes it, or tell an agent in this folder:"), theme.text.Render("  remember for "+folder.Name+": …"))
		return wrapAll(lines, width)
	}
	lines = append(lines, currentModel.layoutRow([]segment{{text: "◆ " + folder.Name, style: theme.bold}, {text: " · context.md", style: theme.dim}},
		[]segment{{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(text)), style: theme.dim}}, width, false), "")
	lines = append(lines, currentModel.markdownLines(text)...)
	lines = append(lines, "", currentModel.fileLink(contextPath, width))
	lines = append(lines, theme.dim.Render("loads after "+strings.Join(inherits, ", ")))
	return wrapAll(lines, width)
}

// contextFilePreview shows the repo's context.md or one of its guides.
func (currentModel model) contextFilePreview(guide string, width int) []string {
	theme := currentModel.theme
	name, _, tokens := currentModel.contextFileDetails(guide)
	var filePath, text, use string
	if guide == "" {
		filePath, _ = currentModel.repository.ContextPath("")
		text = currentModel.repository.ReadContext("")
		use = "Loads into every session in " + currentModel.repositoryName() + ", before the folder's own context."
	} else {
		filePath, _ = currentModel.repository.GuidePath(guide)
		text = currentModel.repository.ReadGuide(guide)
		use = guideUses[guide]
	}
	lines := []string{currentModel.layoutRow([]segment{{text: name, style: theme.bold}}, []segment{{text: fmt.Sprintf("~%d tokens", tokens), style: theme.dim}}, width, false)}
	lines = append(lines, theme.dim.Render(use), "")
	if strings.TrimSpace(text) == "" {
		lines = append(lines, theme.faint.Render("Empty. e writes it."))
		if guide == "" {
			lines = append(lines, "", theme.dim.Render("This is the read-this-first file you would paste into every new session."))
		}
	} else {
		lines = append(lines, currentModel.markdownLines(text)...)
	}
	lines = append(lines, "", currentModel.fileLink(filePath, width))
	return wrapAll(lines, width)
}

func (currentModel model) itemPreview(item organizeItem, width int) (string, []string) {
	theme := currentModel.theme
	switch item.kind {
	case suggestionItem:
		return "SUGGESTION", currentModel.suggestionBody(*item.suggestion, width)
	case contextFileItem:
		return "FILE", currentModel.contextFilePreview(item.guide, width)
	case newAgentItem:
		return "SESSION", wrapAll([]string{
			theme.bold.Render("new " + item.open.tool),
			currentModel.layoutRow(append([]segment{{text: "pane ", style: theme.dim}}, currentModel.paneMarker(item.open)...), nil, width, false),
			"",
			theme.dim.Render("den saves it once you send the first prompt."),
		}, width)
	}
	return "SESSION", currentModel.sessionPreview(item.session, item.open, width)
}

func (currentModel model) sessionPreview(session *store.Session, open *openAgent, width int) []string {
	theme := currentModel.theme
	digest := currentModel.digests[session.SessionId]
	lines := []string{theme.bold.Render(session.Title)}
	details := session.Tool + " · " + relativeAge(session.LastActivityAt) + " ago · " + currentModel.repository.FolderName(session.FolderId)
	if session.Archived {
		details += " · archived"
	}
	lines = append(lines, theme.dim.Render(details))
	if session.Branch != "" {
		branchLine := "⎇ " + session.Branch
		if currentModel.mergedBranches[session.Branch] {
			branchLine += " · merged"
		}
		lines = append(lines, theme.dim.Render(branchLine))
	}
	if open != nil {
		statusText := map[string]string{"running": "running", "waiting": "waiting for you"}[open.status]
		if statusText == "" {
			statusText = "open"
		}
		glyph := currentModel.statusGlyph(open.status)
		lines = append(lines, glyph.style.Render(glyph.text)+theme.text.Render(fmt.Sprintf(" %s in pane %d", statusText, open.number)))
	} else {
		lines = append(lines, theme.faint.Render("· not open · ↵ resumes it"))
	}
	if suggestion := currentModel.repository.PendingFor(session.SessionId); suggestion != nil {
		lines = append(lines, "", theme.suggestion.Render("✦ "+strings.TrimPrefix(currentModel.ghostText(session.SessionId), "→ ")))
		lines = append(lines, theme.dim.Render(suggestion.Reason))
		lines = append(lines, theme.key.Render("⇥")+theme.action.Render(" accept  ")+theme.key.Render("x")+theme.action.Render(" reject"))
	}
	if digest.FirstPrompt != "" {
		lines = append(lines, "", theme.columnHeader.Render("FIRST PROMPT"), truncate(digest.FirstPrompt, width*4))
	}
	if digest.LastPrompt != "" && digest.LastPrompt != digest.FirstPrompt {
		lines = append(lines, "", theme.columnHeader.Render(fmt.Sprintf("LATEST OF %d PROMPTS", digest.PromptCount)), truncate(digest.LastPrompt, width*3))
	}
	if len(digest.TouchedFiles) > 0 {
		lines = append(lines, "", theme.columnHeader.Render(fmt.Sprintf("EDITED · %d FILES", len(digest.TouchedFiles))))
		for _, filePath := range digest.TouchedFiles[:min(8, len(digest.TouchedFiles))] {
			lines = append(lines, theme.text.Render(truncate(currentModel.relativeToRepository(filePath, session.WorkingDirectory), width)))
		}
		if len(digest.TouchedFiles) > 8 {
			lines = append(lines, theme.faint.Render(fmt.Sprintf("and %d more", len(digest.TouchedFiles)-8)))
		}
	}
	lines = append(lines, "", theme.columnHeader.Render("CONTEXT"))
	if session.WithoutContext {
		lines = append(lines, theme.dim.Render("started without context (N)"))
	} else {
		layers := []string{currentModel.repositoryName()}
		for _, folder := range currentModel.repository.FolderPath(session.FolderId) {
			layers = append(layers, folder.Name)
		}
		lines = append(lines, theme.accent.Render("◆ ")+theme.text.Render(strings.Join(layers, " → ")))
	}
	if session.WorkingDirectory != "" {
		lines = append(lines, theme.dim.Render(displayPath(session.WorkingDirectory)))
	}
	return wrapAll(lines, width)
}

func (currentModel model) relativeToRepository(filePath string, workingDirectory string) string {
	for _, base := range []string{workingDirectory, currentModel.repository.RepositoryPath} {
		if base == "" {
			continue
		}
		if relativePath, err := filepath.Rel(base, filePath); err == nil && !strings.HasPrefix(relativePath, "..") {
			return relativePath
		}
	}
	return displayPath(filePath)
}

// markdownLines shows a markdown file with its headings in bold.
func (currentModel model) markdownLines(text string) []string {
	var lines []string
	for _, line := range splitLines(text) {
		if strings.HasPrefix(line, "#") {
			lines = append(lines, currentModel.theme.bold.Render(line))
		} else {
			lines = append(lines, currentModel.theme.text.Render(line))
		}
	}
	return lines
}

// fileLink shows a path as a clickable OSC 8 link, so it opens in the IDE or Finder.
func (currentModel model) fileLink(filePath string, width int) string {
	return ansi.SetHyperlink("file://"+filePath) + currentModel.theme.link.Render(truncate(displayPath(filePath), width)) + ansi.ResetHyperlink()
}

// withoutLeadingSpace drops the space a wrap leaves at the start of a line, behind any escape sequences.
func withoutLeadingSpace(piece string) string {
	sequences := styleSequence.FindAllStringIndex(piece, -1)
	textStart := 0
	for _, sequence := range sequences {
		if sequence[0] != textStart {
			break
		}
		textStart = sequence[1]
	}
	if strings.HasPrefix(piece[textStart:], " ") {
		return piece[:textStart] + piece[textStart+1:]
	}
	return piece
}

// styleSequence matches an SGR escape sequence, which sets colors and text attributes.
var styleSequence = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// wrapAll wraps every line to width. The styling active where a line breaks is closed at the break
// and opened again on the next line, so each line renders on its own, as the columns need.
func wrapAll(lines []string, width int) []string {
	var wrapped []string
	for _, line := range lines {
		if ansi.StringWidth(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}
		pieces := strings.Split(ansi.Wrap(line, width, ""), "\n")
		activeStyle := ""
		for pieceIndex, piece := range pieces {
			if pieceIndex > 0 {
				piece = withoutLeadingSpace(piece)
			}
			styledPiece := activeStyle + piece
			for _, sequence := range styleSequence.FindAllString(piece, -1) {
				if sequence == "\x1b[m" || sequence == "\x1b[0m" {
					activeStyle = ""
				} else {
					activeStyle += sequence
				}
			}
			if activeStyle != "" && pieceIndex < len(pieces)-1 {
				styledPiece += "\x1b[m"
			}
			wrapped = append(wrapped, styledPiece)
		}
	}
	return wrapped
}
