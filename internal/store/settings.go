package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Settings are the user's choices for den on this machine, shared by every repo.
type Settings struct {
	// HelperTool is the agent CLI den makes its own model calls through, or "" for the first one installed.
	HelperTool string `json:"helperTool,omitempty"`
}

func settingsFilePath() (string, error) {
	dataDirectory, err := DataDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDirectory, "settings.json"), nil
}

// LoadSettings reads the settings, or the defaults when there are none yet.
func LoadSettings() (Settings, error) {
	var settings Settings
	filePath, err := settingsFilePath()
	if err != nil {
		return settings, err
	}
	content, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	return settings, json.Unmarshal(content, &settings)
}

// Save writes to a temporary file and renames it into place, like the repo's own file.
func (settings Settings) Save() error {
	filePath, err := settingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	temporaryFilePath := filePath + ".tmp"
	if err := os.WriteFile(temporaryFilePath, content, 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryFilePath, filePath)
}
