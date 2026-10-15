package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

type model struct{}

func Run() error {
	_, err := tea.NewProgram(model{}).Run()
	return err
}

func (model) Init() tea.Cmd {
	return nil
}

func (currentModel model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if keyPress, isKeyPress := message.(tea.KeyPressMsg); isKeyPress {
		switch keyPress.String() {
		case "q", "ctrl+c":
			return currentModel, tea.Quit
		}
	}
	return currentModel, nil
}

func (model) View() tea.View {
	return tea.NewView(titleStyle.Render("den") + "\n\nq to quit\n")
}
