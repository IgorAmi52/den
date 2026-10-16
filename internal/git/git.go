package git

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RepositoryRoot is the main checkout of the repo holding directory, the same for all its worktrees,
// or the directory itself outside git.
func RepositoryRoot(directory string) string {
	commonDirectory, err := run(directory, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return directory
	}
	return filepath.Dir(commonDirectory)
}

// Worktrees lists the checkouts of the repo, the main one first.
func Worktrees(directory string) []string {
	output, err := run(directory, "worktree", "list", "--porcelain")
	if err != nil {
		return []string{directory}
	}
	var worktrees []string
	for _, line := range strings.Split(output, "\n") {
		if path, isWorktree := strings.CutPrefix(line, "worktree "); isWorktree {
			worktrees = append(worktrees, path)
		}
	}
	return worktrees
}

func CurrentBranch(directory string) string {
	branch, _ := run(directory, "branch", "--show-current")
	return branch
}

// RecentCommitSubjects returns the subjects of the user's own latest commits, newest first.
func RecentCommitSubjects(directory string, count int) []string {
	email, _ := run(directory, "config", "user.email")
	arguments := []string{"log", "--no-merges", "-n", strconv.Itoa(count), "--format=%s"}
	if email != "" {
		arguments = append(arguments, "--author="+email)
	}
	output, err := run(directory, arguments...)
	if err != nil || output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

// MergedBranches lists local branches already merged into main or master, and those whose remote branch is gone,
// which is how a squash-merged pull request looks after a fetch. den never fetches; it reads what the last fetch left.
func MergedBranches(directory string) map[string]bool {
	merged := map[string]bool{}
	for _, defaultBranch := range []string{"main", "master"} {
		output, err := run(directory, "branch", "--merged", defaultBranch, "--format=%(refname:short)")
		if err != nil {
			continue
		}
		for _, branch := range strings.Split(output, "\n") {
			if branch != "" && branch != defaultBranch {
				merged[branch] = true
			}
		}
		break
	}
	output, _ := run(directory, "for-each-ref", "--format=%(refname:short) %(upstream:track)", "refs/heads")
	for _, line := range strings.Split(output, "\n") {
		if branch, isGone := strings.CutSuffix(line, " [gone]"); isGone {
			merged[branch] = true
		}
	}
	return merged
}

func run(directory string, arguments ...string) (string, error) {
	output, err := exec.Command("git", append([]string{"-C", directory}, arguments...)...).Output()
	return strings.TrimSpace(string(output)), err
}
