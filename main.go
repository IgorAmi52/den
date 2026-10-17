package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/layout"
	"github.com/IgorAmi52/den/internal/tmux"
	"github.com/IgorAmi52/den/internal/ui"
)

const (
	windowName = "den"
	// maximumHookEventSize caps what den reads of a hook's event; the reply it needs is near the start.
	maximumHookEventSize = 1 << 20
)

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
	// Claude sends its event as JSON on stdin and Codex appends it as an argument; a waiting agent's event says why it waits.
	if len(arguments) > 2 && arguments[0] == "hook" && arguments[1] == "status" {
		paneId := tmux.CurrentPane()
		if paneId == "" {
			return nil
		}
		state, reason := arguments[2], ""
		if state == agent.StateWaiting {
			state, reason = agent.StopState(hookEvent(arguments[3:]))
		}
		if state == "" {
			return nil
		}
		return tmux.SetPaneStatus(paneId, state, reason)
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

// hookEvent is the event an agent's hook passes: Codex's JSON argument, or else Claude's JSON on stdin.
// stdin is only read when something is piped into it, so a hook run by hand never waits.
func hookEvent(extraArguments []string) []byte {
	if len(extraArguments) > 0 {
		return []byte(extraArguments[len(extraArguments)-1])
	}
	if information, err := os.Stdin.Stat(); err != nil || information.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	event, _ := io.ReadAll(io.LimitReader(os.Stdin, maximumHookEventSize))
	return event
}

func checkDependencies() error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git not found. Install it with: xcode-select --install (or: brew install git)")
	}
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
