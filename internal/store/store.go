package store

import (
	"os"
	"path/filepath"
)

func DataDirectory() (string, error) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDirectory, ".den"), nil
}
