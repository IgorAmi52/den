package brain

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/agent"
)

const (
	maximumProposedFolders = 6
	// minimumFolderSessions is how many sessions a topic needs before den proposes a folder for it.
	minimumFolderSessions = 3
	// recentSession is how new an unfiled past session must be to land in the Inbox; older ones are archived.
	recentSession = 14 * 24 * time.Hour
)

var (
	issueKeyPattern           = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)
	conventionalCommitPattern = regexp.MustCompile(`^(feat|fix|chore|refactor|docs|test|perf|build|ci|style|revert)(\([^)]*\))?!?: `)
	// genericWords say nothing about a topic, so they never name a folder.
	genericWords = toSet(`a an and are as at be but by can could did do does for from get got had has have help how i i'd i'm id im
		if in into is it its just let lets like make me my need new no not now of on or our please project repo run see so some
		that the then there these this to up use want was we what when where which why will with work working would you your
		add added adding change changes check code create file files fix fixed fixing implement make new update updated use using
		feature features feat chore refactor bug bugs issue test tests wip tmp temp try notes main master develop dev
		src lib app apps internal pkg cmd java kotlin kt go ts js tsx py resources test tests spec docs com org net www
		users home library tmp private var`)
)

// Welcome is what den proposes the first time it opens in a repo, from history already on this machine.
type Welcome struct {
	PastSessionCount int
	Folders          []ProposedFolder
	// Unfiled are the past sessions no proposed folder takes.
	Unfiled []agent.Digest
	Noticed []string
}

type ProposedFolder struct {
	Name     string
	Keywords []string
	Sessions []agent.Digest
	// Context is a draft context.md built from where these sessions worked.
	Context string
}

// IsRecent tells whether an unfiled past session goes to the Inbox rather than straight to the archive.
func IsRecent(digest agent.Digest, now time.Time) bool {
	return now.Sub(digest.LastActivityAt) < recentSession
}

// FirstRun groups past sessions into topics by the words of their branches, edited directories and first prompts.
func FirstRun(digests []agent.Digest, commitSubjects []string, repositoryPath string) Welcome {
	welcome := Welcome{PastSessionCount: len(digests)}
	topicWordsBySession := make([][]string, len(digests))
	sessionsByWord := map[string][]int{}
	for sessionIndex, digest := range digests {
		topicWordsBySession[sessionIndex] = topicWords(digest, repositoryPath)
		for _, word := range topicWordsBySession[sessionIndex] {
			sessionsByWord[word] = append(sessionsByWord[word], sessionIndex)
		}
	}
	candidateWords := make([]string, 0, len(sessionsByWord))
	for word := range sessionsByWord {
		candidateWords = append(candidateWords, word)
	}
	sort.Slice(candidateWords, func(first int, second int) bool {
		firstCount, secondCount := len(sessionsByWord[candidateWords[first]]), len(sessionsByWord[candidateWords[second]])
		if firstCount != secondCount {
			return firstCount > secondCount
		}
		return candidateWords[first] < candidateWords[second]
	})
	assigned := map[int]bool{}
	for _, word := range candidateWords {
		if len(welcome.Folders) == maximumProposedFolders {
			break
		}
		var unassigned []int
		for _, sessionIndex := range sessionsByWord[word] {
			if !assigned[sessionIndex] {
				unassigned = append(unassigned, sessionIndex)
			}
		}
		// A word that mostly covers sessions another folder already took is the same topic seen twice.
		if len(unassigned) < minimumFolderSessions || len(unassigned)*2 < len(sessionsByWord[word]) {
			continue
		}
		folder := ProposedFolder{Name: titleCase(word)}
		for _, sessionIndex := range unassigned {
			assigned[sessionIndex] = true
			folder.Sessions = append(folder.Sessions, digests[sessionIndex])
		}
		folder.Keywords = companionWords(word, unassigned, topicWordsBySession)
		folder.Context = draftContext(folder, repositoryPath)
		welcome.Folders = append(welcome.Folders, folder)
	}
	for sessionIndex, digest := range digests {
		if !assigned[sessionIndex] {
			welcome.Unfiled = append(welcome.Unfiled, digest)
		}
	}
	welcome.Noticed = noticedPatterns(digests, commitSubjects)
	return welcome
}

// topicWords are the distinct words of a session that could name its topic.
func topicWords(digest agent.Digest, repositoryPath string) []string {
	var sources []string
	if !defaultBranches[digest.Branch] {
		sources = append(sources, issueKeyPattern.ReplaceAllString(ShortBranch(digest.Branch), " "))
	}
	for _, filePath := range digest.TouchedFiles {
		if relativePath, err := filepath.Rel(repositoryPath, filePath); err == nil && !strings.HasPrefix(relativePath, "..") {
			sources = append(sources, filepath.Dir(relativePath))
		}
	}
	sources = append(sources, digest.FirstPrompt)
	var distinct []string
	for _, source := range sources {
		for _, word := range words(source) {
			if len(word) >= 3 && !genericWords[word] && !isNumber(word) && !slices.Contains(distinct, word) {
				distinct = append(distinct, word)
			}
		}
	}
	return distinct
}

// companionWords are the words that show up most often next to the folder's word, to say what the folder covers.
func companionWords(word string, sessionIndexes []int, topicWordsBySession [][]string) []string {
	counts := map[string]int{}
	for _, sessionIndex := range sessionIndexes {
		for _, companion := range topicWordsBySession[sessionIndex] {
			if companion != word {
				counts[companion]++
			}
		}
	}
	companions := make([]string, 0, len(counts))
	for companion, count := range counts {
		if count >= 2 {
			companions = append(companions, companion)
		}
	}
	sort.Slice(companions, func(first int, second int) bool {
		if counts[companions[first]] != counts[companions[second]] {
			return counts[companions[first]] > counts[companions[second]]
		}
		return companions[first] < companions[second]
	})
	return companions[:min(3, len(companions))]
}

// draftContext lists where the folder's sessions edited code and which branches they used.
// It is a starting point the user edits, not a summary; summaries need a model.
func draftContext(folder ProposedFolder, repositoryPath string) string {
	var draft strings.Builder
	fmt.Fprintf(&draft, "# %s\n", folder.Name)
	if len(folder.Keywords) > 0 {
		fmt.Fprintf(&draft, "\nCovers %s.\n", strings.Join(folder.Keywords, ", "))
	}
	writeCodeSections(&draft, folder.Sessions, repositoryPath)
	draft.WriteString("\n## Notes\n- \n")
	return draft.String()
}

// writeCodeSections adds where the sessions edited code, the directories most sessions touched first, and their branches.
func writeCodeSections(draft *strings.Builder, digests []agent.Digest, repositoryPath string) {
	sessionCountByDirectory := map[string]int{}
	var branches []string
	for _, digest := range digests {
		seen := map[string]bool{}
		for _, filePath := range digest.TouchedFiles {
			relativePath, err := filepath.Rel(repositoryPath, filePath)
			if err != nil || strings.HasPrefix(relativePath, "..") {
				continue
			}
			directory := filepath.Dir(relativePath)
			if !seen[directory] {
				seen[directory] = true
				sessionCountByDirectory[directory]++
			}
		}
		if !defaultBranches[digest.Branch] && !slices.Contains(branches, digest.Branch) {
			branches = append(branches, digest.Branch)
		}
	}
	directories := make([]string, 0, len(sessionCountByDirectory))
	for directory := range sessionCountByDirectory {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(first int, second int) bool {
		if sessionCountByDirectory[directories[first]] != sessionCountByDirectory[directories[second]] {
			return sessionCountByDirectory[directories[first]] > sessionCountByDirectory[directories[second]]
		}
		return directories[first] < directories[second]
	})
	if len(directories) > 0 {
		draft.WriteString("\n## Where the code is\n")
		for _, directory := range directories[:min(6, len(directories))] {
			fmt.Fprintf(draft, "- %s/ (edited in %s)\n", directory, countedSessions(sessionCountByDirectory[directory], "session"))
		}
	}
	if len(branches) > 0 {
		draft.WriteString("\n## Branches\n")
		for _, branch := range branches[:min(4, len(branches))] {
			fmt.Fprintf(draft, "- %s\n", branch)
		}
	}
}

func noticedPatterns(digests []agent.Digest, commitSubjects []string) []string {
	var noticed []string
	branchCount, prefixCounts, issueKeyBranches := 0, map[string]int{}, 0
	var issueKeyExample string
	for _, digest := range digests {
		if defaultBranches[digest.Branch] {
			continue
		}
		branchCount++
		if prefix, _, hasPrefix := strings.Cut(digest.Branch, "/"); hasPrefix {
			prefixCounts[prefix]++
		}
		if issueKey := issueKeyPattern.FindString(digest.Branch); issueKey != "" {
			issueKeyBranches++
			issueKeyExample = issueKey
		}
	}
	for prefix, count := range prefixCounts {
		if count*2 > branchCount {
			noticed = append(noticed, fmt.Sprintf("branches look like %s/<topic>", prefix))
		}
	}
	if issueKeyBranches*2 > branchCount && issueKeyExample != "" {
		noticed = append(noticed, fmt.Sprintf("issue keys look like %s", issueKeyExample))
	}
	if style := commitStyle(commitSubjects); style.conventional {
		noticed = append(noticed, fmt.Sprintf("commits are conventional: %s", strings.Join(style.types, ", ")))
	}
	return noticed
}

type commitHabits struct {
	conventional bool
	types        []string
	// issueKeyCount of subjectCount subjects name an issue key.
	issueKeyCount int
	subjectCount  int
}

// IsConventional tells whether most of the commit subjects are conventional commits.
func IsConventional(commitSubjects []string) bool {
	return commitStyle(commitSubjects).conventional
}

// commitStyle reads the habits most of the user's commit subjects share.
func commitStyle(commitSubjects []string) commitHabits {
	var habits commitHabits
	if len(commitSubjects) == 0 {
		return habits
	}
	conventionalCount := 0
	typeCounts := map[string]int{}
	for _, subject := range commitSubjects {
		if match := conventionalCommitPattern.FindStringSubmatch(subject); match != nil {
			conventionalCount++
			typeCounts[match[1]]++
		}
		if agent.IssueKeyIn(subject) != "" {
			habits.issueKeyCount++
		}
	}
	habits.subjectCount = len(commitSubjects)
	habits.conventional = conventionalCount*2 > len(commitSubjects)
	for commitType := range typeCounts {
		habits.types = append(habits.types, commitType)
	}
	sort.Slice(habits.types, func(first int, second int) bool {
		if typeCounts[habits.types[first]] != typeCounts[habits.types[second]] {
			return typeCounts[habits.types[first]] > typeCounts[habits.types[second]]
		}
		return habits.types[first] < habits.types[second]
	})
	return habits
}

func titleCase(word string) string {
	return strings.ToUpper(word[:1]) + word[1:]
}

func isNumber(word string) bool {
	return strings.Trim(word, "0123456789") == ""
}

func toSet(spaceSeparated string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(spaceSeparated) {
		set[word] = true
	}
	return set
}
