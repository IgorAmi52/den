package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/layout"
	"github.com/IgorAmi52/den/internal/tmux"
	"github.com/IgorAmi52/den/internal/ui"
)

const windowName = "den"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) > 0 && arguments[0] == "panel" {
		return ui.Run()
	}
	// tmux runs "den layout" from its layout hooks, with nowhere to show an error but a message over the panel,
	// so a failed layout, such as for a window that just closed, stays silent.
	if len(arguments) > 1 && arguments[0] == "layout" {
		layout.Apply(arguments[1], ui.PanelMarkerOption, ui.PanelWidth)
		return nil
	}
	// Agents run "den hook status <state>" from their hooks; the pane they run in comes from tmux.
	// Codex appends a JSON argument, which is ignored.
	if len(arguments) > 2 && arguments[0] == "hook" && arguments[1] == "status" {
		if paneId := tmux.CurrentPane(); paneId != "" {
			return tmux.SetPaneStatus(paneId, arguments[2])
		}
		return nil
	}
	if err := checkDependencies(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	panelCommand := []string{executable, "panel"}
	if tmux.InsideSession() {
		return tmux.NewWindow(windowName, workingDirectory, panelCommand)
	}
	denSessionName := sessionName(workingDirectory)
	if tmux.SessionExists(denSessionName) {
		if err := restorePanelIfMissing(denSessionName, workingDirectory, panelCommand); err != nil {
			return err
		}
	}
	return tmux.NewOrAttachSession(denSessionName, workingDirectory, panelCommand)
}

func restorePanelIfMissing(denSessionName string, workingDirectory string, panelCommand []string) error {
	panelPanes, err := tmux.SessionPanesWithOption(denSessionName, ui.PanelMarkerOption)
	if err != nil || len(panelPanes) > 0 {
		return err
	}
	return tmux.SplitFullHeightLeft(denSessionName, ui.PanelWidth, workingDirectory, panelCommand)
}

func checkDependencies() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return errors.New("tmux not found. Install it with: brew install tmux")
	}
	if len(agent.Installed()) == 0 {
		return errors.New("no agent CLI found. Install one with: brew install --cask claude-code (or: brew install --cask codex)")
	}
	return nil
}

func sessionName(workingDirectory string) string {
	repositoryName := strings.NewReplacer(".", "-", ":", "-").Replace(filepath.Base(workingDirectory))
	return "den-" + repositoryName
}
