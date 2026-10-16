package brain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/store"
)

const (
	HabitIssueSubfolders = "issue-subfolders"
	HabitIssueKeyTitles  = "issue-key-titles"
	// questionEvery keeps den to one new question a day.
	questionEvery = 24 * time.Hour
	// minimumQuestionEvidence is how many issues must share sessions before den asks about grouping them.
	minimumQuestionEvidence = 2
	// minimumCommitsToAsk and the share range say when commit subjects are mixed enough to ask about issue keys:
	// almost all or almost none means the answer is already clear.
	minimumCommitsToAsk       = 10
	lowestMixedIssueKeyShare  = 0.25
	highestMixedIssueKeyShare = 0.75
	conventionalCommitRule    = "Write commit messages as conventional commits: <type>: <summary>, with types like feat, fix and chore."
	issueKeyTitleRule         = "Start commit subjects and pull request titles with the issue key from the branch, like YT-123: …"
)

// LaptopFile is one of the user's own rule files, as den sees it when looking for rules to propose.
type LaptopFile struct {
	Tool   string
	Path   string
	Text   string
	Exists bool
	// Installed tells whether the tool that reads it is installed, so den may create the file when it's missing.
	Installed bool
}

// issueGroup is the sessions of one folder that work on the same issue.
type issueGroup struct {
	folderId   string
	issueKey   string
	sessionIds []string
}

// NoticeHabits records the patterns den sees right now, with how often each shows up. It only records;
// suggestions come from habits once they're on.
func NoticeHabits(observation Observation) []store.Habit {
	groups := groupableIssues(observation)
	var keys []string
	for _, group := range groups {
		keys = append(keys, group.issueKey)
	}
	description := "groups the sessions of one issue in a subfolder"
	if len(keys) > 0 {
		description += ", like " + strings.Join(keys[:min(2, len(keys))], " and ")
	}
	return []store.Habit{{Name: HabitIssueSubfolders, Description: description, Seen: len(groups)}}
}

// questions asks about a habit when den isn't sure of it: one open question at a time, one new one a day,
// and only when the answer decides more than one future suggestion.
func questions(observation Observation) []store.Suggestion {
	repository := observation.Repository
	if repository.PendingOfKind(store.SuggestQuestion) != nil || observation.Now.Sub(repository.LastQuestionAt) < questionEvery {
		return nil
	}
	if habit := repository.FindHabit(HabitIssueSubfolders); habit != nil && habit.State == store.HabitNoticed && habit.Seen >= minimumQuestionEvidence {
		var keys []string
		for _, group := range groupableIssues(observation) {
			keys = append(keys, group.issueKey)
		}
		return []store.Suggestion{{
			Kind:     store.SuggestQuestion,
			Habit:    HabitIssueSubfolders,
			Question: "Group each issue's sessions in its own subfolder?",
			Reason: fmt.Sprintf("%s each have more than one session. Yes, and den proposes a subfolder per issue from now on; no, and it won't ask again.",
				strings.Join(keys[:min(2, len(keys))], " and ")),
		}}
	}
	style := commitStyle(observation.CommitSubjects)
	share := float64(style.issueKeyCount) / float64(max(style.subjectCount, 1))
	if repository.FindHabit(HabitIssueKeyTitles) == nil && style.subjectCount >= minimumCommitsToAsk && share >= lowestMixedIssueKeyShare && share <= highestMixedIssueKeyShare {
		return []store.Suggestion{{
			Kind:     store.SuggestQuestion,
			Habit:    HabitIssueKeyTitles,
			Question: "Should commits and PR titles start with the issue key, like YT-123: …?",
			Rule:     issueKeyTitleRule,
			Reason: fmt.Sprintf("%d of your last %d commits here name an issue, the rest don't. Yes adds that rule to this repo's context.md.",
				style.issueKeyCount, style.subjectCount),
		}}
	}
	return nil
}

// issueSubfolders groups the sessions of one issue in a subfolder named after it, once the user turned the habit on,
// and files later sessions of that issue into the subfolder that already exists.
func issueSubfolders(observation Observation) []store.Suggestion {
	repository := observation.Repository
	if !repository.IsHabitActive(HabitIssueSubfolders) {
		return nil
	}
	var suggestions []store.Suggestion
	for _, group := range issueGroups(observation) {
		if existing := issueSubfolder(repository, group.folderId, group.issueKey); existing != nil {
			reason := fmt.Sprintf("⎇ %s is the subfolder for %s.", existing.Name, group.issueKey)
			suggestions = append(suggestions, store.Suggestion{Kind: store.SuggestMove, SessionIds: group.sessionIds, FolderId: existing.Id, Reason: reason, Habit: HabitIssueSubfolders})
			continue
		}
		if len(group.sessionIds) < 2 {
			continue
		}
		suggestions = append(suggestions, store.Suggestion{
			Kind:           store.SuggestSubfolder,
			SessionIds:     group.sessionIds,
			ParentFolderId: group.folderId,
			IssueKey:       group.issueKey,
			FolderName:     group.issueKey,
			Habit:          HabitIssueSubfolders,
			Reason:         fmt.Sprintf("%d sessions in %s are about %s. You group work by issue.", len(group.sessionIds), repository.FolderName(group.folderId), group.issueKey),
		})
	}
	return suggestions
}

// issueGroups lists, per folder, the sessions that name the same issue, leaving out branch and issue subfolders,
// which are already grouped.
func issueGroups(observation Observation) []issueGroup {
	repository := observation.Repository
	var groups []issueGroup
	for _, session := range repository.Sessions {
		issueKey := observation.Digests[session.SessionId].IssueKey
		folder := repository.FindFolder(session.FolderId)
		if issueKey == "" || session.Archived || (folder != nil && (folder.Branch != "" || folder.IssueKey != "")) {
			continue
		}
		groupIndex := slices.IndexFunc(groups, func(group issueGroup) bool {
			return group.folderId == session.FolderId && group.issueKey == issueKey
		})
		if groupIndex < 0 {
			groups = append(groups, issueGroup{folderId: session.FolderId, issueKey: issueKey})
			groupIndex = len(groups) - 1
		}
		groups[groupIndex].sessionIds = append(groups[groupIndex].sessionIds, session.SessionId)
	}
	return groups
}

// groupableIssues are the issues with 2+ sessions and no subfolder yet, where grouping by issue adds something:
// sessions all on one branch are already grouped by the branch subfolder.
func groupableIssues(observation Observation) []issueGroup {
	var groupable []issueGroup
	for _, group := range issueGroups(observation) {
		if len(group.sessionIds) < 2 || issueSubfolder(observation.Repository, group.folderId, group.issueKey) != nil {
			continue
		}
		branches := map[string]bool{}
		for _, sessionId := range group.sessionIds {
			branches[branchOf(*observation.Repository.FindSession(sessionId), observation.Digests)] = true
		}
		if len(branches) == 1 {
			onlyBranch := ""
			for branch := range branches {
				onlyBranch = branch
			}
			if !defaultBranches[onlyBranch] {
				continue
			}
		}
		groupable = append(groupable, group)
	}
	return groupable
}

func issueSubfolder(repository *store.Repository, parentId string, issueKey string) *store.Folder {
	for index := range repository.Folders {
		if repository.Folders[index].ParentId == parentId && repository.Folders[index].IssueKey == issueKey {
			return &repository.Folders[index]
		}
	}
	return nil
}

// laptopRules proposes a rule for the user's own files once it holds in more than one repo:
// written to every laptop file that doesn't say it yet. With no file left to change, the rule has no files,
// which clears a waiting one.
func laptopRules(observation Observation) []store.Suggestion {
	var filePaths, names []string
	holdsInSeveralRepositories := commitStyle(observation.CommitSubjects).conventional && len(observation.ConventionalRepositories) > 0
	for _, laptopFile := range observation.LaptopFiles {
		if !holdsInSeveralRepositories || strings.Contains(strings.ToLower(laptopFile.Text), "conventional commit") || (!laptopFile.Exists && !laptopFile.Installed) {
			continue
		}
		filePaths = append(filePaths, laptopFile.Path)
		names = append(names, filepath.Base(laptopFile.Path))
	}
	if len(filePaths) == 0 {
		return []store.Suggestion{{Kind: store.SuggestLaptopRule, Rule: conventionalCommitRule}}
	}
	return []store.Suggestion{{
		Kind:      store.SuggestLaptopRule,
		Rule:      conventionalCommitRule,
		FilePaths: filePaths,
		Reason: fmt.Sprintf("Your commits here and in %s are conventional commits, and %s doesn't say so yet. Agents read it in every repo, even new ones with no history.",
			strings.Join(observation.ConventionalRepositories[:min(2, len(observation.ConventionalRepositories))], " and "), strings.Join(names, " and ")),
	}}
}
