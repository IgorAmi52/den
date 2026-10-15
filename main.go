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
	if len(arguments) > 1 && arguments[0] == "layout" {
		return layout.Apply(arguments[1], ui.PanelMarkerOption, ui.PanelWidth)
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
