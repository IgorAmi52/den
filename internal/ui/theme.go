package ui

import (
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// paletteColor is a palette entry as hex, with the 256-color number used when the terminal or tmux has no truecolor.
// The numbers are picked by hand, since the nearest automatic match of a dark tint is often a loud color;
// the tmux options use the same numbers.
type paletteColor struct {
	hex     string
	ansi256 int
}

var (
	accentColor     = paletteColor{"#5fafff", 75}
	dimColor        = paletteColor{"#8a8a8a", 245}
	faintColor      = paletteColor{"#585858", 240}
	borderColor     = paletteColor{"#444444", 238}
	runningColor    = paletteColor{"#87d787", 114}
	waitingColor    = paletteColor{"#ffaf00", 214}
	suggestionColor = paletteColor{"#d7afff", 183}
	removedColor    = paletteColor{"#ff8787", 210}
	// folderColor is a manila tan, so folders never read as sessions and don't compete with the accent or the amber of waiting.
	folderColor = paletteColor{"#d7af87", 180}
)

// Backgrounds only work on one kind of terminal background, so each has a dark and a light variant.
var (
	selectionDark   = paletteColor{"#22324a", 236}
	selectionLight  = paletteColor{"#d7e5f7", 189}
	addedDark       = paletteColor{"#1e2b1e", 235}
	addedLight      = paletteColor{"#e4f4e4", 194}
	removedDark     = paletteColor{"#2f1e1e", 235}
	removedLight    = paletteColor{"#f8e4e4", 224}
	waitingBarDark  = paletteColor{"#3a2c00", 236}
	waitingBarLight = paletteColor{"#fff0d0", 230}
	overlayDark     = paletteColor{"#121315", 233}
	overlayLight    = paletteColor{"#f4f4f4", 255}
)

// theme holds every style the views use; views never build styles themselves.
type theme struct {
	text           lipgloss.Style
	bold           lipgloss.Style
	folder         lipgloss.Style
	accent         lipgloss.Style
	accentBold     lipgloss.Style
	dim            lipgloss.Style
	faint          lipgloss.Style
	border         lipgloss.Style
	running        lipgloss.Style
	waiting        lipgloss.Style
	waitingBar     lipgloss.Style
	suggestion     lipgloss.Style
	suggestionBold lipgloss.Style
	ghost          lipgloss.Style
	errorText      lipgloss.Style
	link           lipgloss.Style
	// selectedRow paints the full-width bar under the selected row.
	selectedRow  lipgloss.Style
	dropTarget   lipgloss.Style
	columnHeader lipgloss.Style
	key          lipgloss.Style
	action       lipgloss.Style
	card         lipgloss.Style
	overlay      lipgloss.Style
	diffAdded    lipgloss.Style
	diffRemoved  lipgloss.Style
	diffContext  lipgloss.Style
	youLabel     lipgloss.Style
	denLabel     lipgloss.Style
	pinned       lipgloss.Style
}

// newTheme builds the styles for the terminal's background and color support.
func newTheme(isDark bool, profile colorprofile.Profile) theme {
	pick := func(entry paletteColor) color.Color {
		if profile == colorprofile.TrueColor {
			return lipgloss.Color(entry.hex)
		}
		return lipgloss.Color(strconv.Itoa(entry.ansi256))
	}
	pickBackground := func(light paletteColor, dark paletteColor) color.Color {
		if isDark {
			return pick(dark)
		}
		return pick(light)
	}
	accent, dim, faint, suggestion := pick(accentColor), pick(dimColor), pick(faintColor), pick(suggestionColor)
	base := lipgloss.NewStyle()
	return theme{
		text:           base,
		bold:           base.Bold(true),
		folder:         base.Foreground(pick(folderColor)).Bold(true),
		accent:         base.Foreground(accent),
		accentBold:     base.Foreground(accent).Bold(true),
		dim:            base.Foreground(dim),
		faint:          base.Foreground(faint),
		border:         base.Foreground(pick(borderColor)),
		running:        base.Foreground(pick(runningColor)),
		waiting:        base.Foreground(pick(waitingColor)).Bold(true),
		waitingBar:     base.Foreground(pick(waitingColor)).Background(pickBackground(waitingBarLight, waitingBarDark)).Bold(true),
		suggestion:     base.Foreground(suggestion),
		suggestionBold: base.Foreground(suggestion).Bold(true),
		ghost:          base.Foreground(suggestion).Faint(true),
		errorText:      base.Foreground(pick(removedColor)),
		link:           base.Foreground(accent).Underline(true),
		selectedRow:    base.Background(pickBackground(selectionLight, selectionDark)),
		dropTarget:     base.Foreground(accent).Bold(true),
		columnHeader:   base.Foreground(dim),
		key:            base.Foreground(accent),
		action:         base.Foreground(dim),
		card:           base.Border(lipgloss.RoundedBorder()).BorderForeground(suggestion).Padding(0, 2),
		overlay:        base.Border(lipgloss.RoundedBorder()).BorderForeground(accent).Background(pickBackground(overlayLight, overlayDark)).Padding(0, 1),
		diffAdded:      base.Foreground(pick(runningColor)).Background(pickBackground(addedLight, addedDark)),
		diffRemoved:    base.Foreground(pick(removedColor)).Background(pickBackground(removedLight, removedDark)),
		diffContext:    base.Foreground(dim),
		youLabel:       base.Foreground(accent).Bold(true),
		denLabel:       base.Foreground(suggestion).Bold(true),
		pinned:         base.Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(suggestion).PaddingLeft(1),
	}
}
