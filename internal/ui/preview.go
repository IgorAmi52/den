package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/IgorAmi52/den/internal/store"
)

// previewColumnLines show the selected row in full beside the tree, when wide: a proposal first when there is one,
// then the session, the folder's context or the file.
func (currentModel model) previewColumnLines(width int, height int) []string {
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
	if suggestion := currentModel.proposalFor(row); suggestion != nil && row.kind != sessionRow {
		proposal = append([]string{theme.suggestionBold.Render("✦ " + currentModel.proposalTitle(*suggestion))}, currentModel.suggestionBody(*suggestion, width)...)
		proposal = append(proposal, theme.key.Render("⇥")+theme.action.Render(" yes  ")+theme.key.Render("⌫")+theme.action.Render(" no"), "", theme.border.Render(strings.Repeat("─", width)), "")
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
			theme.dim.Render("Sessions that aren't filed yet. When den knows where one belongs, the row says so in faint text, and ⇥ files it."),
			"",
			theme.dim.Render("Sessions here start with the repo's context only."),
		}, width)
	case archiveRow:
		return "ARCHIVE", wrapAll([]string{
			theme.dim.Render("Sessions that are done. den proposes the ones that look finished; a or m puts any session here yourself."),
			"",
			theme.dim.Render(fmt.Sprintf("They stay resumable: ↵ opens one, a or m brings it back. den forgets a session %d days after it was archived; the agent keeps its own history.", archiveKeepDays())),
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
	if habits := currentModel.repository.Habits; len(habits) > 0 {
		lines = append(lines, "", theme.columnHeader.Render("HABITS"))
		for _, habit := range habits {
			counts := fmt.Sprintf("seen %d", habit.Seen)
			if habit.Accepted+habit.Rejected > 0 {
				counts += fmt.Sprintf(" · yes %d · no %d", habit.Accepted, habit.Rejected)
			}
			description := habit.Description
			if description == "" {
				description = habit.Name
			}
			lines = append(lines, theme.text.Render(description), theme.dim.Render("  "+string(habit.State)+" · "+counts))
		}
	}
	lines = append(lines, "", theme.columnHeader.Render("MODEL"))
	if currentModel.helperCaller == nil {
		lines = append(lines, theme.dim.Render("No agent CLI can make model calls, so den uses its local rules only."))
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
	contextPath, _ := currentModel.repository.ContextPath(folder.Id)
	notes := currentModel.repository.ReadContext(folder.Id)
	linked := ""
	if folder.LinkedPath != "" {
		linked = store.ReadText(folder.LinkedPath)
	}
	var lines []string
	if strings.TrimSpace(notes) == "" && folder.LinkedPath == "" {
		lines = append(lines, theme.bold.Render(folder.Name), "", theme.dim.Render("No context yet. Sessions here start with the context of "+strings.Join(inherits, " and ")+"."), "")
		lines = append(lines, theme.dim.Render("When an agent here writes a plan or a spec, den offers to link it. To link one now, tell den in the chat:"))
		lines = append(lines, theme.text.Render("  link docs/PLAN.md to "+folder.Name), "")
		lines = append(lines, theme.key.Render("e")+theme.action.Render(" writes den's notes for it instead"))
		return wrapAll(lines, width)
	}
	if folder.LinkedPath != "" {
		size := segment{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(linked)), style: theme.dim}
		if strings.TrimSpace(linked) == "" {
			size = segment{text: "missing", style: theme.errorText}
		}
		lines = append(lines, currentModel.layoutRow([]segment{{text: "◆ " + folder.Name, style: theme.bold}, {text: " · " + filepath.Base(folder.LinkedPath) + " · linked", style: theme.dim}},
			[]segment{size}, width, false), "")
		lines = append(lines, currentModel.markdownLines(linked)...)
		lines = append(lines, "", currentModel.fileLink(folder.LinkedPath, width), theme.faint.Render("yours: den only proposes changes · \"unlink "+folder.Name+"\" in the chat takes it off"))
	}
	if strings.TrimSpace(notes) != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, currentModel.layoutRow([]segment{{text: "◆ " + folder.Name, style: theme.bold}, {text: " · den's notes", style: theme.dim}},
			[]segment{{text: fmt.Sprintf("~%d tokens", store.EstimatedTokens(notes)), style: theme.dim}}, width, false), "")
		lines = append(lines, currentModel.markdownLines(notes)...)
		lines = append(lines, "", currentModel.fileLink(contextPath, width))
	}
	lines = append(lines, theme.dim.Render("loads after "+strings.Join(inherits, ", ")))
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
		use = laptopTool + " loads it in every repo, so den never adds it to a session. den only proposes rules for it, ones that hold in more than one repo."
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

func (currentModel model) sessionPreview(session *store.Session, open *openAgent, width int) []string {
	theme := currentModel.theme
	digest := currentModel.digests[session.SessionId]
	lines := []string{theme.bold.Render(session.Title)}
	details := session.Tool + " · " + relativeAge(session.LastActivityAt) + " ago · " + currentModel.repository.FolderName(session.FolderId)
	if session.Archived {
		details += fmt.Sprintf(" · archived, %dd left", int(session.ArchiveTimeLeft(time.Now()).Hours()/24)+1)
	}
	lines = append(lines, theme.dim.Render(details))
	if session.Branch != "" {
		branchLine := "⎇ " + session.Branch
		if currentModel.mergedBranches[session.Branch] {
			branchLine += " · merged"
		}
		lines = append(lines, theme.dim.Render(branchLine))
	}
	if digest.IssueKey != "" {
		lines = append(lines, theme.dim.Render("issue "+digest.IssueKey))
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
	if len(digest.PullRequests) > 0 {
		lines = append(lines, "", theme.columnHeader.Render("PULL REQUESTS"))
		for _, link := range digest.PullRequests {
			lines = append(lines, ansi.SetHyperlink(link)+theme.link.Render(truncate(link, width))+ansi.ResetHyperlink())
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
