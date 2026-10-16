// Package terminal reads and changes the one Terminal.app setting den depends on:
// "Use Option as Meta key". Without it, Option types characters like å, and den's Option keys never reach tmux.
package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const appleTerminalDomain = "com.apple.Terminal"

// OptionKey reports the profile new Terminal.app windows open with, and whether it sends Option as Meta.
func OptionKey() (profile string, enabled bool, err error) {
	profileOutput, err := exec.Command("defaults", "read", appleTerminalDomain, "Default Window Settings").Output()
	if err != nil {
		return "", false, err
	}
	profile = strings.TrimSpace(string(profileOutput))
	exportedPath, cleanUp, err := exportPreferences()
	if err != nil {
		return profile, false, err
	}
	defer cleanUp()
	value, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :Window\\ Settings:"+escaped(profile)+":useOptionAsMetaKey", exportedPath).Output()
	return profile, err == nil && strings.TrimSpace(string(value)) == "true", nil
}

// EnableOptionKey turns on "Use Option as Meta key" in the profile. It goes through the preferences system,
// so Terminal.app picks it up for windows opened afterwards.
func EnableOptionKey(profile string) error {
	exportedPath, cleanUp, err := exportPreferences()
	if err != nil {
		return err
	}
	defer cleanUp()
	entry := ":Window\\ Settings:" + escaped(profile) + ":useOptionAsMetaKey"
	if exec.Command("/usr/libexec/PlistBuddy", "-c", "Set "+entry+" true", exportedPath).Run() != nil {
		if err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add "+entry+" bool true", exportedPath).Run(); err != nil {
			return err
		}
	}
	return exec.Command("defaults", "import", appleTerminalDomain, exportedPath).Run()
}

func exportPreferences() (string, func(), error) {
	directory, err := os.MkdirTemp("", "den-terminal")
	if err != nil {
		return "", nil, err
	}
	exportedPath := filepath.Join(directory, "terminal.plist")
	cleanUp := func() { os.RemoveAll(directory) }
	if err := exec.Command("defaults", "export", appleTerminalDomain, exportedPath).Run(); err != nil {
		cleanUp()
		return "", nil, err
	}
	return exportedPath, cleanUp, nil
}

// escaped quotes a profile name for a PlistBuddy entry path, where spaces and colons separate parts.
func escaped(profile string) string {
	return strings.NewReplacer(`\`, `\\`, " ", `\ `, ":", `\:`).Replace(profile)
}
