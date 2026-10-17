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
	Options map[string]string
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

func KillPane(paneId string) error {
	_, err := run("kill-pane", "-t", paneId)
	return err
}

func KillWindow(windowId string) error {
	_, err := run("kill-window", "-t", windowId)
	return err
}

// HideStatusLineOfDenSession turns tmux's status line off when the window is in a session den made for itself,
// named den-<repo>, so the panel gets that row. In a session of the user's, the status line stays.
func HideStatusLineOfDenSession(windowId string) error {
	sessionName, err := run("display-message", "-p", "-t", windowId, "#{session_name}")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(sessionName), "den-") {
		return err
	}
	_, err = run("set-option", "-t", strings.TrimSpace(sessionName), "status", "off")
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
		// Not a title drag: pass the release on, so apps with their own mouse handling (like the panel) see the drop.
		"if-shell -F '" + insideMarkedWindow + "' 'select-pane -M' ; send-keys -M -t =",
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

func UnsetPaneOption(paneId string, option string) error {
	_, err := run("set-option", "-p", "-u", "-t", paneId, option)
	return err
}

// SetPaneStatus records an agent's state on its pane and colors a waiting pane's border amber.
// reason says why it waits, or what it finished; an empty one keeps the reason already shown, and running clears it.
func SetPaneStatus(paneId string, state string, reason string) error {
	if err := SetPaneOption(paneId, "@den-status", state); err != nil {
		return err
	}
	var err error
	switch {
	case state == "running":
		err = UnsetPaneOption(paneId, "@den-reason")
	case reason != "":
		err = SetPaneOption(paneId, "@den-reason", reason)
	}
	if err != nil {
		return err
	}
	for _, option := range []string{"pane-border-style", "pane-active-border-style"} {
		var err error
		if state == "waiting" {
			err = SetPaneOption(paneId, option, "fg=colour214")
		} else {
			err = UnsetPaneOption(paneId, option)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// GlobalEnvironment reads a variable from the environment tmux started with, which is the terminal's.
func GlobalEnvironment(name string) string {
	output, err := run("show-environment", "-g", name)
	if err != nil {
		return ""
	}
	_, value, _ := strings.Cut(output, "=")
	return value
}

func IsZoomed(windowId string) bool {
	output, err := run("display-message", "-p", "-t", windowId, "#{window_zoomed_flag}")
	return err == nil && output == "1"
}

// ToggleZoom makes the pane fill its window, or puts the window back as it was.
func ToggleZoom(paneId string) error {
	_, err := run("resize-pane", "-Z", "-t", paneId)
	return err
}

// DisplayPopup runs a command in a rounded popup over the window and returns once the command exits.
func DisplayPopup(title string, workingDirectory string, command []string) error {
	var quoted []string
	for _, argument := range command {
		quoted = append(quoted, "'"+strings.ReplaceAll(argument, "'", `'\''`)+"'")
	}
	_, err := run("display-popup", "-E", "-b", "rounded", "-S", "fg=colour75", "-T", " "+title+" ",
		"-w", "80%", "-h", "80%", "-d", workingDirectory, strings.Join(quoted, " "))
	return err
}

// BindDenKeys binds Option keys, with no prefix, in den windows: ⌥1–⌥4 select that agent, the panel being the top-left
// pane and the agents following it in pane order; ⌥0 selects the panel, or from the panel goes back to the last agent;
// ⌥Tab moves to the next pane, wrapping from the last agent back to the panel; ⌥N asks the panel for a new agent.
// Outside den windows each key reaches the program as usual.
// They replace Ctrl+Space, which an earlier den bound and macOS keeps for switching keyboard layouts.
func BindDenKeys(windowMarkerOption string, panelMarkerOption string) error {
	insideDenWindow := "#{" + windowMarkerOption + "}"
	bindings := map[string]string{
		"M-0":   "if-shell -F '#{" + panelMarkerOption + "}' 'select-pane -l' \"select-pane -t '{top-left}'\"",
		"M-n":   "send-keys -t '{top-left}' M-n",
		"M-Tab": "select-pane -t ':.+'",
	}
	for agentNumber := 1; agentNumber <= 4; agentNumber++ {
		number := strconv.Itoa(agentNumber)
		bindings["M-"+number] = "if-shell -F '#{e|>:#{window_panes}," + number + "}' " +
			"\"select-pane -t '{top-left}' ; select-pane -t ':.+" + number + "'\" " +
			"'display-message \"no agent " + number + " open\"'"
	}
	for key, denCommand := range bindings {
		if _, err := run("bind-key", "-n", key, "if-shell", "-F", insideDenWindow, denCommand, "send-keys "+key); err != nil {
			return err
		}
	}
	run("unbind-key", "-n", "C-Space")
	_, err := run("bind-key", "Space", "next-layout")
	return err
}

func SelectPane(paneId string) error {
	_, err := run("select-pane", "-t", paneId)
	return err
}

// ListPanes returns the window's panes in pane order, with the values of the requested pane options.
func ListPanes(windowId string, optionNames ...string) ([]Pane, error) {
	fields := []string{"#{pane_id}", "#{pane_left}", "#{pane_top}", "#{pane_width}", "#{pane_height}"}
	for _, optionName := range optionNames {
		fields = append(fields, "#{"+optionName+"}")
	}
	// The command name is never empty, so it goes last: the output is trimmed, which would drop empty trailing fields.
	fields = append(fields, "#{pane_current_command}")
	output, err := run("list-panes", "-t", windowId, "-F", strings.Join(fields, "\t"))
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(output, "\n") {
		values := strings.Split(line, "\t")
		pane := Pane{Id: values[0], Command: values[len(values)-1], Options: map[string]string{}}
		pane.Left, _ = strconv.Atoi(values[1])
		pane.Top, _ = strconv.Atoi(values[2])
		pane.Width, _ = strconv.Atoi(values[3])
		pane.Height, _ = strconv.Atoi(values[4])
		for optionIndex, optionName := range optionNames {
			pane.Options[optionName] = values[5+optionIndex]
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
