package store

import (
	"os"
	"path/filepath"
)

// LessonsPath is den's file of what it learned from the cards the user said no to. It's one file for every repo,
// since the lessons are about den's judgment, not about a repo's work.
func LessonsPath() (string, error) {
	dataDirectory, err := DataDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDirectory, "lessons.md"), nil
}

func ReadLessons() string {
	lessonsPath, err := LessonsPath()
	if err != nil {
		return ""
	}
	text, _ := os.ReadFile(lessonsPath)
	return string(text)
}

func WriteLessons(text string) error {
	lessonsPath, err := LessonsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lessonsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(lessonsPath, []byte(text), 0o644)
}
