package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type helpGroup struct {
	title    string
	bindings []key.Binding
}

func (currentModel model) helpGroups() []helpGroup {
	keys := currentModel.keys
	optionKey := func(keyLabel string, description string) key.Binding {
		return key.NewBinding(key.WithKeys(), key.WithHelp(keyLabel, description))
	}
	return []helpGroup{
		{title: "FROM ANYWHERE", bindings: []key.Binding{
			optionKey("⌥1-4", "jump to that agent"), optionKey("⌥0", "jump to den, and back"),
			keys.newFromAgent, optionKey("click", "focus any pane"),
		}},
		{title: "YES AND NO", bindings: []key.Binding{
			relabeled(keys.accept, "yes to what den proposes"), relabeled(keys.reject, "no, and don't offer it again"),
			relabeled(keys.back, "back out, change nothing"), relabeled(keys.yes, "yes, in reviews and questions"),
		}},
		{title: "SESSIONS", bindings: []key.Binding{
			relabeled(keys.open, "resume, focus, open"), relabeled(keys.newSession, "new, with context"), keys.newWithout,
			keys.jump, relabeled(keys.move, "carry to a folder"), relabeled(keys.selectRow, "select several"),
			keys.rename, keys.archive, keys.delete, keys.filter,
		}},
		{title: "FOLDERS AND DEN", bindings: []key.Binding{
			relabeled(keys.newFolder, "new folder"), relabeled(keys.editContext, "edit context.md"),
			relabeled(keys.wide, "wide, with a preview"), keys.scrollPreview, relabeled(keys.review, "review proposals"),
			relabeled(keys.chat, "ask den"), keys.quit,
		}},
	}
}

func (currentModel model) updateHelp(message tea.Msg) (tea.Model, tea.Cmd) {
	if keyPress, isKeyPress := message.(tea.KeyPressMsg); isKeyPress {
		if key.Matches(keyPress, currentModel.keys.back) || key.Matches(keyPress, currentModel.keys.help) || key.Matches(keyPress, currentModel.keys.quit) {
			currentModel.closeScreen()
		}
	}
	return currentModel, nil
}

func (currentModel model) helpView() string {
	theme := currentModel.theme
	width := currentModel.width
	lines := []string{currentModel.screenTopBar("keys", width), theme.border.Render(strings.Repeat("─", width)), ""}
	const columnWidth = 40
	var columns []string
	for _, group := range currentModel.helpGroups() {
		column := []string{theme.columnHeader.Render(group.title), ""}
		for _, binding := range group.bindings {
			help := binding.Help()
			column = append(column, theme.key.Render(padRight(help.Key, 7))+theme.action.Render(truncate(help.Desc, columnWidth-8)))
		}
		columns = append(columns, padBlock(strings.Join(column, "\n"), columnWidth))
	}
	perRow := max(min((width-4)/columnWidth, len(columns)), 1)
	for start := 0; start < len(columns); start += perRow {
		row := lipgloss.JoinHorizontal(lipgloss.Top, columns[start:min(start+perRow, len(columns))]...)
		lines = append(lines, centered(row, width)...)
		lines = append(lines, "")
	}
	notes := []string{
		theme.dim.Render("The Option keys need \"Use Option as Meta key\" in Terminal.app: Settings, Profiles, Keyboard. Drag a pane's title onto another to swap them."),
		theme.dim.Render("◆ marks folders with context. Agents in a folder start with it; say \"remember for <folder>: …\" to add to it."),
		theme.faint.Render("den only suggests. Nothing moves, changes or gets deleted until you say so."),
	}
	for _, note := range notes {
		lines = append(lines, centered(note, width)...)
	}
	return strings.Join(lines, "\n")
}

func padBlock(block string, width int) string {
	lines := strings.Split(block, "\n")
	for index, line := range lines {
		lines[index] = padRight(line, width)
	}
	return strings.Join(lines, "\n")
}
