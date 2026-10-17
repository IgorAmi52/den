package ui

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	glamourstyle "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// maximumCachedRenders bounds the rendered markdown kept between frames; den redraws every second.
const maximumCachedRenders = 64

type markdownKey struct {
	text   string
	width  int
	isDark bool
}

// markdownCache holds rendered markdown by text, width and theme. View and Update run on one goroutine, so it needs no lock.
var markdownCache = map[markdownKey][]string{}

// renderedMarkdown lays markdown out for the terminal, wrapped to width: headings, lists, emphasis, links and code
// rendered rather than shown as raw markers. When rendering fails, the raw text shows instead.
func (currentModel model) renderedMarkdown(text string, width int) []string {
	cacheKey := markdownKey{text: text, width: width, isDark: currentModel.isDark}
	if lines, cached := markdownCache[cacheKey]; cached {
		return lines
	}
	if len(markdownCache) >= maximumCachedRenders {
		clear(markdownCache)
	}
	lines := renderMarkdown(text, width, currentModel.isDark)
	if lines == nil {
		lines = wrapAll(currentModel.markdownLines(text), width)
	}
	markdownCache[cacheKey] = lines
	return lines
}

// renderMarkdown renders two columns narrower than width, since Glamour wraps before adding a quote's bar,
// and gives the wrapped lines of a list item a hanging indent, which Glamour leaves out.
func renderMarkdown(text string, width int, isDark bool) []string {
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(markdownStyle(isDark)), glamour.WithWordWrap(max(width-2, 10)))
	if err != nil {
		return nil
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(strings.Trim(rendered, "\n"), "\n") {
		// Glamour leaves a blank line after a quote, so a line without the bar right under a quoted one is a word
		// its wrapping dropped there; it goes back on the quote line, and fittedLines wraps that line again.
		plain := strings.TrimSpace(ansi.Strip(line))
		if len(lines) > 0 && plain != "" && !strings.HasPrefix(plain, "│") && strings.HasPrefix(strings.TrimSpace(ansi.Strip(lines[len(lines)-1])), "│") {
			lines[len(lines)-1] = strings.TrimRight(lines[len(lines)-1], " ") + " " + strings.TrimLeft(line, " ")
			continue
		}
		lines = append(lines, line)
	}
	continuationIndent := ""
	for index, line := range lines {
		line = strings.TrimRight(line, " ")
		plain := ansi.Strip(line)
		trimmed := strings.TrimLeft(plain, " ")
		switch {
		case trimmed == "":
			continuationIndent = ""
		case strings.HasPrefix(trimmed, "• ") || listNumber.MatchString(trimmed):
			marker := listNumber.FindString(trimmed)
			if marker == "" {
				marker = "• "
			}
			continuationIndent = strings.Repeat(" ", len(plain)-len(trimmed)+ansi.StringWidth(marker))
		case continuationIndent != "" && !strings.HasPrefix(plain, continuationIndent):
			line = continuationIndent + line
		}
		lines[index] = line
	}
	return fittedLines(lines, width)
}

// fittedLines re-wraps lines Glamour left too wide, which happens inside quotes, carrying the line's quote bar
// or indent onto each continuation.
func fittedLines(lines []string, width int) []string {
	var fitted []string
	for _, line := range lines {
		if ansi.StringWidth(line) <= width {
			fitted = append(fitted, line)
			continue
		}
		plain := ansi.Strip(line)
		lead := plain[:len(plain)-len(strings.TrimLeft(plain, " │"))]
		pieces := wrapAll([]string{line}, max(width-ansi.StringWidth(lead), 10))
		fitted = append(fitted, pieces[0])
		for _, piece := range pieces[1:] {
			fitted = append(fitted, lead+piece)
		}
	}
	return fitted
}

// listNumber matches the marker of a numbered list item, like "2. ".
var listNumber = regexp.MustCompile(`^\d+\. `)

// markdownStyle is Glamour's dark or light style fitted to den: no outer margin, so it fills den's columns,
// the terminal's own text color, and headings in den's accent instead of a colored banner.
func markdownStyle(isDark bool) glamourstyle.StyleConfig {
	style := styles.DarkStyleConfig
	if !isDark {
		style = styles.LightStyleConfig
	}
	noMargin := uint(0)
	accent := "75"
	style.Document.Margin = &noMargin
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	style.Document.Color = nil
	bold := true
	style.Heading.Color = &accent
	style.H1.Prefix, style.H1.Suffix = "", ""
	style.H1.Color, style.H1.BackgroundColor = &accent, nil
	for _, heading := range []*glamourstyle.StyleBlock{&style.H2, &style.H3, &style.H4, &style.H5, &style.H6} {
		heading.Prefix, heading.Bold = "", &bold
	}
	style.Link.Color = &accent
	return style
}
