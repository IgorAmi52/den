package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/IgorAmi52/den/internal/ui"
)

var requiredBinaries = map[string]string{
	"tmux":   "brew install tmux",
	"claude": "brew install --cask claude-code",
}

func main() {
	if !requiredBinariesInstalled() {
		os.Exit(1)
	}
	if err := ui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func requiredBinariesInstalled() bool {
	allInstalled := true
	for binary, installCommand := range requiredBinaries {
		if _, err := exec.LookPath(binary); err != nil {
			fmt.Fprintf(os.Stderr, "%s not found. Install it with: %s\n", binary, installCommand)
			allInstalled = false
		}
	}
	return allInstalled
}
