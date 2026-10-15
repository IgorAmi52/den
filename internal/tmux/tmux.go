package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Pane struct {
	Id      string
	Command string
	Left    int
	Top     int
	Width   int
	Height  int
}

func InsideSession() bool {
	return os.Getenv("TMUX") != ""
}

func CurrentPane() string {
	return os.Getenv("TMUX_PANE")
}

func NewOrAttachSession(sessionName string, workingDirectory string, command []string) error {
	arguments := append([]string{"new-session", "-A", "-s", sessionName, "-c", workingDirectory}, command...)
	tmuxCommand := exec.Command("tmux", arguments...)
	tmuxCommand.Stdin = os.Stdin
	tmuxCommand.Stdout = os.Stdout
	tmuxCommand.Stderr = os.Stderr
	return tmuxCommand.Run()
}

func SessionExists(sessionName string) bool {
	_, err := run("has-session", "-t", "="+sessionName)
	return err == nil
}

func NewWindow(windowName string, workingDirectory string, command []string) error {
	_, err := run(append([]string{"new-window", "-n", windowName, "-c", workingDirectory}, command...)...)
	return err
}

func WindowOf(paneId string) (string, error) {
	return run("display-message", "-p", "-t", paneId, "#{window_id}")
}

func SetPaneOption(paneId string, option string, value string) error {
	_, err := run("set-option", "-p", "-t", paneId, option, value)
	return err
}

func SessionPanesWithOption(sessionName string, option string) ([]string, error) {
	output, err := run("list-panes", "-s", "-t", "="+sessionName, "-f", "#{"+option+"}", "-F", "#{pane_id}")
	if err != nil || output == "" {
		return nil, err
	}
	return strings.Split(output, "\n"), nil
}

func SplitFullHeightLeft(sessionName string, width int, workingDirectory string, command []string) error {
	arguments := append([]string{"split-window", "-h", "-b", "-f", "-l", strconv.Itoa(width), "-t", "=" + sessionName + ":", "-c", workingDirectory}, command...)
	_, err := run(arguments...)
	return err
}

func KillWindow(windowId string) error {
	_, err := run("kill-window", "-t", windowId)
	return err
}

func SetWindowOption(windowId string, option string, value string) error {
	_, err := run("set-option", "-w", "-t", windowId, option, value)
	return err
}

func SplitPane(targetPaneId string, sideBySide bool, workingDirectory string, command []string) (string, error) {
	direction := "-v"
	if sideBySide {
		direction = "-h"
	}
	arguments := append([]string{"split-window", direction, "-t", targetPaneId, "-c", workingDirectory, "-P", "-F", "#{pane_id}"}, command...)
	return run(arguments...)
}

func WindowPanesWithOption(windowId string, option string) ([]string, error) {
	output, err := run("list-panes", "-t", windowId, "-f", "#{"+option+"}", "-F", "#{pane_id}")
	if err != nil || output == "" {
		return nil, err
	}
	return strings.Split(output, "\n"), nil
}

func WindowSize(windowId string) (width int, height int, err error) {
	output, err := run("display-message", "-p", "-t", windowId, "#{window_width} #{window_height}")
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(output, "%d %d", &width, &height)
	return width, height, err
}

func SelectLayout(windowId string, layout string) error {
	_, err := run("select-layout", "-t", windowId, layout)
	return err
}

func SetWindowHook(windowId string, hook string, command string) error {
	_, err := run("set-hook", "-w", "-t", windowId, hook, command)
	return err
}

// BindTitleDragToSwapPanes makes dragging a pane's title bar onto another pane swap the two,
// and stops border drags from resizing panes. It only acts in windows carrying windowMarkerOption,
// and panes carrying fixedPaneOption never move.
func BindTitleDragToSwapPanes(windowMarkerOption string, fixedPaneOption string) error {
	insideMarkedWindow := "#{" + windowMarkerOption + "}"
	movablePane := "#{&&:" + insideMarkedWindow + ",#{!:#{" + fixedPaneOption + "}}}"
	swapOntoTarget := []string{
		"if-shell", "-F", "-t", "=", "#{&&:#{pane_marked_set}," + movablePane + "}",
		"swap-pane -s '{marked}' -t = ; select-pane -M",
		"if-shell -F '" + insideMarkedWindow + "' 'select-pane -M'",
	}
	bindings := map[string][]string{
		"MouseDown1Border":    {"if-shell", "-F", "-t", "=", movablePane, "select-pane -t = ; select-pane -M ; select-pane -m -t =", "select-pane -t ="},
		"MouseDrag1Border":    {"if-shell", "-F", insideMarkedWindow, "", "resize-pane -M"},
		"MouseUp1Border":      {"if-shell", "-F", insideMarkedWindow, "select-pane -M"},
		"MouseDragEnd1Border": swapOntoTarget,
		"MouseDragEnd1Pane":   swapOntoTarget,
	}
	for key, command := range bindings {
		if _, err := run(append([]string{"bind-key", "-n", key}, command...)...); err != nil {
			return err
		}
	}
	return nil
}

func SelectPane(paneId string) error {
	_, err := run("select-pane", "-t", paneId)
	return err
}

func ListPanes(windowId string) ([]Pane, error) {
	output, err := run("list-panes", "-t", windowId, "-F", "#{pane_id} #{pane_left} #{pane_top} #{pane_width} #{pane_height} #{pane_current_command}")
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(output, "\n") {
		var pane Pane
		if _, err := fmt.Sscanf(line, "%s %d %d %d %d %s", &pane.Id, &pane.Left, &pane.Top, &pane.Width, &pane.Height, &pane.Command); err != nil {
			return nil, err
		}
		panes = append(panes, pane)
	}
	return panes, nil
}

func run(arguments ...string) (string, error) {
	output, err := exec.Command("tmux", arguments...).CombinedOutput()
	trimmedOutput := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s", arguments[0], trimmedOutput)
	}
	return trimmedOutput, nil
}
