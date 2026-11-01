package brain

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// skillFilePattern matches where agents keep skills and slash commands, the same places SkillFile looks.
var skillFilePattern = regexp.MustCompile(`/\.(claude|codex|agents)/skills/[^/]+/(?i:skill\.md)$|/\.claude/commands/[^/]+\.md$`)

// writtenSkillFiles are the skill files an agent wrote in the session that still exist.
func writtenSkillFiles(digest agent.Digest) []string {
	var skillFiles []string
	for _, touched := range digest.TouchedFiles {
		filePath := absoluteIn(digest.WorkingDirectory, touched)
		if information, err := os.Stat(filePath); err == nil && !information.IsDir() && skillFilePattern.MatchString(filepath.ToSlash(filePath)) {
			skillFiles = append(skillFiles, filePath)
		}
	}
	return skillFiles
}

// skillDescription is the description line of a skill file's front matter, which says what the skill is for.
func skillDescription(filePath string) string {
	for _, line := range strings.Split(store.ReadText(filePath), "\n") {
		if description, found := strings.CutPrefix(strings.TrimSpace(line), "description:"); found {
			return strings.TrimSpace(description)
		}
	}
	return ""
}
