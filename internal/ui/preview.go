package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/IgorAmi52/den/internal/store"
)

// previewColumnLines show the selected row in full beside the tree, when wide: a proposal first when there is one,
// then the session, the folder's context or the file.
func (currentModel model) previewColumnLines(width int, height int) []string {
	if currentModel.contextPane.open {
		return currentModel.contextPaneLines(width, height)
	}
	innerWidth := width - 2
	header, body := currentModel.rowPreview(innerWidth)
	offset := min(currentModel.previewOffset, max(len(body)-1, 0))
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

func (currentModel model) rowPreview(width int) (string, []string) {
	theme := currentModel.theme
	row, exists := currentModel.selectedRow()
	if !exists {
		return "", nil
	}
	var proposal []string
	if suggestion := currentModel.proposalFor(row); suggestion != nil {
		proposal = append([]string{theme.suggestionBold.Render("✦ " + currentModel.proposalTitle(*suggestion))}, currentModel.suggestionBody(*suggestion, width)...)
		proposal = append(proposal, theme.key.Render("⇥")+theme.action.Render(" answer it below"), "", theme.border.Render(strings.Repeat("─", width)), "")
	}
	switch row.kind {
	case repositoryRow:
		return "CONTEXT", append(proposal, currentModel.repositoryPreview(width)...)
	case contextFileRow:
		return "FILE", append(proposal, currentModel.contextFilePreview(row.laptopTool, width)...)
	case folderRow:
		return "CONTEXT", append(proposal, currentModel.folderContextPreview(row.folder, width)...)
	case inboxRow:
		return "INBOX", wrapAll([]string{
			theme.dim.Render("Sessions started outside a folder. They start with the repo's context only."),
			"",
			theme.dim.Render("When one turns out to start something that will go on, den offers a folder for it; the row says so in faint text, and ⇥ answers it."),
		}, width)
	case archiveRow:
		return "ARCHIVE", wrapAll([]string{
			theme.dim.Render(fmt.Sprintf("den archives a session %d days after you last used it; a archives one now.", archiveAfterDays())),
			"",
			theme.dim.Render(fmt.Sprintf("They stay resumable: ↵ opens one, a brings it back. den forgets a session %d days after it was archived; the agent keeps its own history.", archiveKeepDays())),
		}, width)
	case newAgentRow:
		return "SESSION", wrapAll([]string{
			theme.bold.Render("new " + row.open.tool),
			currentModel.layoutRow(append([]segment{{text: "pane ", style: theme.dim}}, currentModel.paneMarker(row.open)...), nil, width, false),
			"",
			theme.dim.Render("den saves it once you send the first prompt."),
		}, width)
	}
	return "SESSION", currentModel.sessionPreview(row.session, row.open, width)
}

// repositoryPreview shows the repo's context, lists the laptop files, and what den has noticed about how you work.
func (currentModel model) repositoryPreview(width int) []string {
	theme := currentModel.theme
	lines := currentModel.contextFilePreview("", width)
	if laptopFiles := currentModel.shownLaptopFiles(); len(laptopFiles) > 0 {
		lines = append(lines, "", theme.columnHeader.Render("LAPTOP · EVERY REPO"))
		for _, laptopFile := range laptopFiles {
			size := theme.faint.Render("none yet")
			if laptopFile.Exists {
				size = theme.dim.Render(fmt.Sprintf("~%d tokens", store.EstimatedTokens(laptopFile.Text)))
			}
			lines = append(lines, theme.text.Render(padRight(filepath.Base(laptopFile.Path), 12))+theme.faint.Render(padRight(laptopFile.Tool, 8))+size)
		}
	}
	lines = append(lines, "", theme.columnHeader.Render("MODEL"))
	if currentModel.helperCaller == nil {
		lines = append(lines, theme.dim.Render("No agent CLI can make model calls, so den only names sessions and links the plans agents write."))
	} else {
		usage := fmt.Sprintf("%s · %d calls this run", currentModel.helperCaller.Name(), currentModel.helperCalls)
		if currentModel.helperUsage.CostDollars > 0 {
			usage += fmt.Sprintf(" · $%.3f", currentModel.helperUsage.CostDollars)
		}
		lines = append(lines, theme.dim.Render(usage))
	}
	return append(lines, "", theme.dim.Render("↵ shows the files in the tree, where ↵ on one opens it in $EDITOR."))
}

func (currentModel model) folderContextPreview(folder *store.Folder, width int) []string {
	theme := currentModel.theme
	inherits := []string{currentModel.repositoryName()}
	for _, ancestor := range currentModel.repository.FolderPath(folder.ParentId) {
		inherits = append(inherits, ancestor.Name)
	}
	text := currentModel.repository.ReadContext(folder.Id)
	if strings.TrimSpace(text) == "" {
		lines := []string{theme.bold.Render(folder.Name), "", theme.dim.Render("No context yet. Sessions here start with the context of " + strings.Join(inherits, " and ") + "."), ""}
		lines = append(lines, theme.key.Render("→")+theme.action.Render(" opens it here: ask den to write it, or write it by hand"))
		return wrapAll(lines, width)
	}
	filePath, _ := currentModel.repository.FolderContextPath(folder.Id)
	owner := "den's file"
	if folder.LinkedPath != "" {
		owner = "your file " + filepath.Base(filePath)
	}
	lines := []string{currentModel.layoutRow([]segment{{text: "◆ " + folder.Name, style: theme.bold}, {text: " · " + owner, style: theme.dim}},
		[]segment{{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(text)), style: theme.dim}}, width, false), ""}
	lines = append(lines, currentModel.markdownLines(text)...)
	lines = append(lines, "", currentModel.fileLink(filePath, width), theme.dim.Render("loads after "+strings.Join(inherits, ", ")))
	lines = append(lines, theme.key.Render("→")+theme.action.Render(" edit it here, by hand or by asking den"))
	return wrapAll(lines, width)
}

// contextFilePreview shows the repo's context.md, or a laptop file.
func (currentModel model) contextFilePreview(laptopTool string, width int) []string {
	theme := currentModel.theme
	name, _, tokens := currentModel.contextFileDetails(laptopTool)
	var filePath, text, use string
	if laptopTool == "" {
		filePath, _ = currentModel.repository.ContextPath("")
		text = currentModel.repository.ReadContext("")
		use = "Your private rules and notes for " + currentModel.repositoryName() + ". Every session here starts with them, before the folder's context."
	} else {
		laptopFile, _ := currentModel.laptopFile(laptopTool)
		filePath, text = laptopFile.Path, laptopFile.Text
		use = laptopTool + " loads it in every repo, so den never adds it to a session. It's yours; den only shows it."
	}
	size := segment{text: "empty", style: theme.faint}
	if tokens > 0 {
		size = segment{text: fmt.Sprintf("~%d tokens", tokens), style: theme.dim}
	}
	lines := []string{currentModel.layoutRow([]segment{{text: name, style: theme.bold}}, []segment{size}, width, false)}
	lines = append(lines, theme.dim.Render(use), "")
	if strings.TrimSpace(text) == "" {
		lines = append(lines, theme.faint.Render("Empty. e writes it."))
		if laptopTool == "" {
			lines = append(lines, "", theme.dim.Render("This is the read-this-first file you would paste into every new session."))
		}
	} else {
		lines = append(lines, currentModel.markdownLines(text)...)
	}
	lines = append(lines, "", currentModel.fileLink(filePath, width))
	return wrapAll(lines, width)
}

// sessionPreview shows a session at a glance: its name, den's summary set apart, whether it's open, where its context
// comes from, then the last exchange, the files it edited and how it started.
func (currentModel model) sessionPreview(session *store.Session, open *openAgent, width int) []string {
	theme := currentModel.theme
	digest := currentModel.digests[session.SessionId]
	lines := wrapAll([]string{theme.accentBold.Render(session.Title)}, width)
	summary := theme.faint.Render("No summary yet: den writes one after the first prompt.")
	if session.Summary != "" {
		summary = theme.text.Render(session.Summary)
	}
	lines = append(lines, strings.Split(theme.summaryBlock.Width(width).Render(summary), "\n")...)
	lines = append(lines, "")
	switch {
	case open != nil:
		statusText := map[string]string{"running": "running", "waiting": "waiting for you"}[open.status]
		if statusText == "" {
			statusText = "open"
		}
		glyph := currentModel.statusGlyph(open.status)
		lines = append(lines, glyph.style.Render(glyph.text+" "+statusText)+theme.dim.Render(fmt.Sprintf(" · pane %d · ⌥%d jumps there", open.number, open.number)))
	case session.Archived:
		lines = append(lines, theme.archive.Render("archived")+theme.dim.Render(fmt.Sprintf(" · %dd until den forgets it · ↵ resumes it", int(session.ArchiveTimeLeft(time.Now()).Hours()/24)+1)))
	default:
		lines = append(lines, theme.faint.Render("· not open · ↵ resumes it"))
	}
	meta := session.Tool + " · " + ageText(session.LastActivityAt)
	if digest.PromptCount > 0 {
		meta += fmt.Sprintf(" · %d %s", digest.PromptCount, plural(digest.PromptCount, "prompt"))
	}
	lines = append(lines, theme.dim.Render(meta))
	contextLine := theme.dim.Render("started without context (N)")
	if !session.WithoutContext {
		layers := []string{currentModel.repositoryName()}
		for _, folder := range currentModel.repository.FolderPath(session.FolderId) {
			layers = append(layers, folder.Name)
		}
		contextLine = theme.accent.Render("◆ ") + theme.text.Render(strings.Join(layers, " → "))
	}
	if session.Branch != "" {
		contextLine += theme.dim.Render("   ⎇ " + session.Branch)
	}
	lines = append(lines, contextLine)
	section := func(title string, text string, style lipgloss.Style, maximumLines int) {
		if strings.TrimSpace(text) == "" {
			return
		}
		lines = append(lines, "", theme.sectionHeader.Render(title))
		for _, line := range firstWrappedLines(oneLine(text), width, maximumLines) {
			lines = append(lines, style.Render(line))
		}
	}
	section("YOU ASKED LAST", digest.LastPrompt, theme.text, 3)
	section(strings.ToUpper(session.Tool)+" REPLIED", digest.LastAgentMessage, theme.dim, 4)
	if len(digest.TouchedFiles) > 0 {
		lines = append(lines, "", theme.sectionHeader.Render(fmt.Sprintf("EDITED · %d %s", len(digest.TouchedFiles), strings.ToUpper(plural(len(digest.TouchedFiles), "file")))))
		for _, filePath := range digest.TouchedFiles[:min(8, len(digest.TouchedFiles))] {
			lines = append(lines, theme.text.Render(truncate(currentModel.relativeToRepository(filePath, session.WorkingDirectory), width)))
		}
		if len(digest.TouchedFiles) > 8 {
			lines = append(lines, theme.faint.Render(fmt.Sprintf("and %d more", len(digest.TouchedFiles)-8)))
		}
	}
	if digest.FirstPrompt != digest.LastPrompt {
		section("IT STARTED WITH", digest.FirstPrompt, theme.dim, 3)
	}
	if session.WorkingDirectory != "" {
		lines = append(lines, "", theme.faint.Render(truncate(displayPath(session.WorkingDirectory), width)))
	}
	return lines
}

// ageText says how long ago something happened, as a phrase: "just now", or "2h ago".
func ageText(moment time.Time) string {
	if age := relativeAge(moment); age != "now" {
		return age + " ago"
	}
	return "just now"
}

// firstWrappedLines wraps text to width and keeps at most maximumLines, ending in … when it cut some.
func firstWrappedLines(text string, width int, maximumLines int) []string {
	lines := wrapText(text, width)
	if len(lines) <= maximumLines {
		return lines
	}
	kept := lines[:maximumLines]
	kept[maximumLines-1] = strings.TrimRight(ansi.Truncate(kept[maximumLines-1], max(width-1, 1), ""), " ") + "…"
	return kept
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
