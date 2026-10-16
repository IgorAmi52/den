package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// keyMap holds every binding. The same bindings drive the handlers and the key hints, so the two can't drift apart.
// One meaning per key everywhere: enter says yes, backspace says no, esc steps back, tab moves on, space ticks.
type keyMap struct {
	up, down                   key.Binding
	open, accept, reject       key.Binding
	newSession, newWithout     key.Binding
	newFromAgent               key.Binding
	newFolder, rename, delete  key.Binding
	move, archive, editContext key.Binding
	wide, review, chat         key.Binding
	help, jump, quit, filter   key.Binding
	selectRow, scrollPreview   key.Binding
	back, yes, no, edit, skip  key.Binding
	toggle, ask, done          key.Binding
	send, scrollChat           key.Binding
	pickPrevious, pickNext     key.Binding
	confirmYes, confirmNo      key.Binding
	save                       key.Binding
}

func newKeyMap() keyMap {
	binding := func(keys []string, keyLabel string, description string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keyLabel, description))
	}
	return keyMap{
		up:            binding([]string{"up", "k"}, "↑", "up"),
		down:          binding([]string{"down", "j"}, "↓", "down"),
		open:          binding([]string{"enter"}, "↵", "open"),
		accept:        binding([]string{"tab"}, "⇥", "yes"),
		reject:        binding([]string{"backspace"}, "⌫", "no"),
		newSession:    binding([]string{"n"}, "n", "new"),
		newWithout:    binding([]string{"N"}, "N", "new without context"),
		newFromAgent:  binding([]string{"alt+n"}, "⌥N", "new agent"),
		newFolder:     binding([]string{"f"}, "f", "folder"),
		rename:        binding([]string{"r"}, "r", "rename"),
		delete:        binding([]string{"d"}, "d", "delete"),
		move:          binding([]string{"m"}, "m", "move"),
		archive:       binding([]string{"a"}, "a", "archive"),
		editContext:   binding([]string{"e"}, "e", "context"),
		wide:          binding([]string{"z"}, "z", "wide"),
		review:        binding([]string{"v"}, "v", "review"),
		chat:          binding([]string{"c"}, "c", "ask den"),
		help:          binding([]string{"?"}, "?", "keys"),
		jump:          binding([]string{"1", "2", "3", "4"}, "1-4", "jump to agent"),
		quit:          binding([]string{"q", "ctrl+c"}, "q", "quit"),
		filter:        binding([]string{"/"}, "/", "filter"),
		selectRow:     binding([]string{"space"}, "space", "select"),
		scrollPreview: binding([]string{"pgdown", "pgup", "J", "K"}, "J/K", "scroll preview"),
		back:          binding([]string{"esc"}, "esc", "back"),
		yes:           binding([]string{"enter"}, "↵", "yes"),
		no:            binding([]string{"backspace"}, "⌫", "no"),
		edit:          binding([]string{"e"}, "e", "edit"),
		skip:          binding([]string{"tab"}, "⇥", "skip"),
		toggle:        binding([]string{"space"}, "space", "tick"),
		ask:           binding([]string{"c"}, "c", "ask den"),
		done:          binding([]string{"esc", "q"}, "esc", "done"),
		send:          binding([]string{"enter"}, "↵", "send"),
		scrollChat:    binding([]string{"pgup", "pgdown"}, "pgup/pgdn", "scroll"),
		pickPrevious:  binding([]string{"left", "h", "up", "k"}, "←", "pick"),
		pickNext:      binding([]string{"right", "l", "down", "j"}, "→", "pick"),
		confirmYes:    binding([]string{"enter"}, "↵", "yes"),
		confirmNo:     binding([]string{"esc"}, "esc", "no"),
		save:          binding([]string{"enter"}, "↵", "save"),
	}
}

// relabeled returns a copy of the binding with another description, for hints that name what the key does right now.
func relabeled(binding key.Binding, description string) key.Binding {
	binding.SetHelp(binding.Help().Key, description)
	return binding
}

// hintLinesEndingWith lays out the hints like hintLines, keeping room for tail at the end of the last line,
// so a key like "? keys" stays visible however many hints come before it.
func (currentModel model) hintLinesEndingWith(bindings []key.Binding, tail key.Binding, width int, maximumLines int) []string {
	tailHelp := tail.Help()
	tailWidth := len([]rune(tailHelp.Key)) + 1 + len([]rune(tailHelp.Desc))
	lines := currentModel.hintLines(bindings, width, maximumLines)
	for len(lines) > 0 && ansi.StringWidth(lines[len(lines)-1])+2+tailWidth > width {
		lastLine := lines[len(lines)-1]
		cut := strings.LastIndex(lastLine, "  ")
		if cut < 0 {
			lines = lines[:len(lines)-1]
			break
		}
		lines[len(lines)-1] = lastLine[:cut]
	}
	tailHint := currentModel.theme.key.Render(tailHelp.Key) + " " + currentModel.theme.action.Render(tailHelp.Desc)
	if len(lines) == 0 {
		return []string{tailHint}
	}
	lines[len(lines)-1] += "  " + tailHint
	return lines
}

// hintLines lays out key hints as "key action" pairs over as many lines as needed, each at most width wide,
// keeping at most maximumLines and dropping the hints that don't fit.
func (currentModel model) hintLines(bindings []key.Binding, width int, maximumLines int) []string {
	var lines []string
	var line strings.Builder
	lineWidth := 0
	for _, binding := range bindings {
		help := binding.Help()
		hintWidth := len([]rune(help.Key)) + 1 + len([]rune(help.Desc))
		separatorWidth := 0
		if lineWidth > 0 {
			separatorWidth = 2
		}
		if lineWidth > 0 && lineWidth+separatorWidth+hintWidth > width {
			if len(lines)+1 == maximumLines {
				break
			}
			lines = append(lines, line.String())
			line.Reset()
			lineWidth, separatorWidth = 0, 0
		}
		if separatorWidth > 0 {
			line.WriteString("  ")
		}
		line.WriteString(currentModel.theme.key.Render(help.Key) + " " + currentModel.theme.action.Render(help.Desc))
		lineWidth += separatorWidth + hintWidth
	}
	if lineWidth > 0 {
		lines = append(lines, line.String())
	}
	return lines
}
