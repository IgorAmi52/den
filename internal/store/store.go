package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Folder struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	ParentId  string `json:"parentId,omitempty"`
	Collapsed bool   `json:"collapsed,omitempty"`
	// LinkedPath is a markdown file the user or an agent wrote, linked as the folder's context. den never writes it.
	LinkedPath string `json:"linkedPath,omitempty"`
}

type Session struct {
	Tool             string `json:"tool"`
	SessionId        string `json:"sessionId"`
	FolderId         string `json:"folderId,omitempty"`
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	Branch           string `json:"branch,omitempty"`
	Title            string `json:"title"`
	// TitlePinned is set by a manual rename, so automatic titles stop changing it.
	TitlePinned bool `json:"titlePinned,omitempty"`
	// TitledAtPrompt is the prompt count when den last looked at the title, so it looks again a few prompts later.
	TitledAtPrompt int `json:"titledAtPrompt,omitempty"`
	// ReviewedAtPrompt is the prompt count when den's model last reviewed the session.
	ReviewedAtPrompt int `json:"reviewedAtPrompt,omitempty"`
	// CheckedAtPrompt is the prompt count whose reply den's local check last looked at, so each reply is checked once.
	CheckedAtPrompt int `json:"checkedAtPrompt,omitempty"`
	// CheckedFileCount and CheckedPlan are how many files the session had edited and a hash of its approved plan at that check,
	// so the next check sees what the reply added.
	CheckedFileCount int    `json:"checkedFileCount,omitempty"`
	CheckedPlan      string `json:"checkedPlan,omitempty"`
	// GatedAtPrompt is the prompt count up to which the gate check read the replies; the ones after wait for the next check.
	GatedAtPrompt       int      `json:"gatedAtPrompt,omitempty"`
	GateReviewReason    string   `json:"gateReviewReason,omitempty"`
	GateCallIdentifiers []string `json:"gateCallIdentifiers,omitempty"`
	// CardReviewDue is set once a check found something worth a card review, and cleared when the card review runs.
	CardReviewDue bool `json:"cardReviewDue,omitempty"`
	// CardReviewedAtPrompt is the prompt count when the card review last looked at the session.
	CardReviewedAtPrompt int `json:"cardReviewedAtPrompt,omitempty"`
	// Cards are the cards den proposed from this session and what became of them, newest last.
	Cards []CardRecord `json:"cards,omitempty"`
	// Progress is den's note on where the session stands, updated by every review rather than rewritten.
	Progress Progress `json:"progress,omitzero"`
	// WithoutContext is set for sessions started with N, so resuming them leaves the folder's context out too.
	WithoutContext bool      `json:"withoutContext,omitempty"`
	Archived       bool      `json:"archived,omitempty"`
	ArchivedAt     time.Time `json:"archivedAt,omitzero"`
	// CreatedAt is when den started tracking the session, so an imported old session gets its days before the archive.
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

// CardRecord is a card den proposed from a session and what became of it, so later card reviews don't propose it again.
type CardRecord struct {
	SuggestionId string `json:"suggestionId"`
	// Kind is what the card keeps: notes, folder, skill or rule.
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	// Outcome is waiting, accepted, rejected, withdrawn or replaced.
	Outcome string `json:"outcome"`
}

// Progress is what a session is after, what it got done so far, and what comes next; Next is empty when nothing is left.
type Progress struct {
	Goal string `json:"goal"`
	Done string `json:"done"`
	Next string `json:"next"`
}

const (
	// ArchiveAfter is how long a session sits untouched before den archives it on its own.
	ArchiveAfter = 3 * 24 * time.Hour
	// ArchiveKeepsFor is how long a session stays in the archive before den forgets it. Together with ArchiveAfter
	// it stays under the 30 days after which Claude Code deletes transcripts. The agent keeps its own history.
	ArchiveKeepsFor = 25 * 24 * time.Hour
)

type Repository struct {
	RepositoryPath string `json:"repoPath"`
	// Welcomed is set once the first-run review is done, so it shows only once per repo.
	Welcomed    bool         `json:"welcomed,omitempty"`
	Folders     []Folder     `json:"folders"`
	Sessions    []Session    `json:"sessions"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
	// RejectedKeys remembers rejected suggestions, so the same one is never offered again.
	RejectedKeys []string `json:"rejectedKeys,omitempty"`
	// TrackedSince is when den started watching the repo: sessions started after it outside den are picked up too.
	TrackedSince time.Time `json:"trackedSince,omitzero"`
	// IgnoredSessionIds are sessions the user deleted, or den forgot, so they aren't picked up again.
	IgnoredSessionIds []string `json:"ignoredSessionIds,omitempty"`
}

// DataDirectory is ~/.den, or DEN_HOME when set.
func DataDirectory() (string, error) {
	if override := os.Getenv("DEN_HOME"); override != "" {
		return override, nil
	}
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDirectory, ".den"), nil
}

// KnownRepositoryPaths lists every repo den was opened in on this machine.
func KnownRepositoryPaths() []string {
	dataDirectory, err := DataDirectory()
	if err != nil {
		return nil
	}
	filePaths, _ := filepath.Glob(filepath.Join(dataDirectory, "repos", "*", "den.json"))
	var repositoryPaths []string
	for _, filePath := range filePaths {
		var repository Repository
		if content, err := os.ReadFile(filePath); err == nil && json.Unmarshal(content, &repository) == nil && repository.RepositoryPath != "" {
			repositoryPaths = append(repositoryPaths, repository.RepositoryPath)
		}
	}
	return repositoryPaths
}

func Load(repositoryPath string) (Repository, error) {
	filePath, err := repositoryFilePath(repositoryPath)
	if err != nil {
		return Repository{}, err
	}
	repository := Repository{RepositoryPath: repositoryPath}
	content, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return repository, nil
	}
	if err != nil {
		return Repository{}, err
	}
	err = json.Unmarshal(content, &repository)
	return repository, err
}

// Save writes to a temporary file and renames it into place, so a crash never leaves a half-written file.
func (repository Repository) Save() error {
	filePath, err := repositoryFilePath(repository.RepositoryPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(repository, "", "  ")
	if err != nil {
		return err
	}
	temporaryFilePath := filePath + ".tmp"
	if err := os.WriteFile(temporaryFilePath, content, 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryFilePath, filePath)
}

// Directory holds everything den keeps for this repo outside the repo itself.
func (repository Repository) Directory() (string, error) {
	filePath, err := repositoryFilePath(repository.RepositoryPath)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filePath), nil
}

// NewWorktree names a fresh worktree for this repo, a folder in den's own data outside the repo, and its branch.
func (repository Repository) NewWorktree() (path string, branch string, err error) {
	directory, err := repository.Directory()
	if err != nil {
		return "", "", err
	}
	worktreeId := newId()
	return filepath.Join(directory, "worktrees", worktreeId), "den/" + worktreeId, nil
}

func (repository *Repository) FindSession(sessionId string) *Session {
	for index := range repository.Sessions {
		if repository.Sessions[index].SessionId == sessionId {
			return &repository.Sessions[index]
		}
	}
	return nil
}

func (repository *Repository) FindFolder(folderId string) *Folder {
	for index := range repository.Folders {
		if repository.Folders[index].Id == folderId {
			return &repository.Folders[index]
		}
	}
	return nil
}

func (repository *Repository) AddFolder(name string, parentId string) Folder {
	folder := Folder{Id: newId(), Name: name, ParentId: parentId}
	repository.Folders = append(repository.Folders, folder)
	return folder
}

// MoveFolder puts a folder, with everything in it, inside another folder or at the top level.
// A folder can't go inside itself or one of its own subfolders.
func (repository *Repository) MoveFolder(folderId string, parentId string) error {
	for ancestor := repository.FindFolder(parentId); ancestor != nil; ancestor = repository.FindFolder(ancestor.ParentId) {
		if ancestor.Id == folderId {
			return errors.New("a folder can't go inside itself")
		}
	}
	if folder := repository.FindFolder(folderId); folder != nil {
		folder.ParentId = parentId
	}
	return nil
}

// DeleteFolder removes only the folder: its subfolders and sessions move up to its parent. It counts as rejecting
// that folder, so den never proposes it, or a folder for its sessions, again.
func (repository *Repository) DeleteFolder(folderId string) {
	folder := repository.FindFolder(folderId)
	if folder == nil {
		return
	}
	parentId := folder.ParentId
	deletedFolder := Suggestion{Kind: SuggestFolder, FolderName: folder.Name}
	for _, session := range repository.Sessions {
		if session.FolderId == folderId {
			deletedFolder.SessionIds = append(deletedFolder.SessionIds, session.SessionId)
		}
	}
	repository.RejectedKeys = append(repository.RejectedKeys, deletedFolder.RejectionKeys()...)
	for index := range repository.Folders {
		if repository.Folders[index].ParentId == folderId {
			repository.Folders[index].ParentId = parentId
		}
	}
	for index := range repository.Sessions {
		if repository.Sessions[index].FolderId == folderId {
			repository.Sessions[index].FolderId = parentId
		}
	}
	repository.Folders = slices.DeleteFunc(repository.Folders, func(candidate Folder) bool {
		return candidate.Id == folderId
	})
	repository.Suggestions = slices.DeleteFunc(repository.Suggestions, func(suggestion Suggestion) bool {
		return suggestion.FolderId == folderId || (suggestion.Kind == SuggestFolder && suggestion.ParentFolderId == folderId)
	})
}

// SessionCount counts the sessions in a folder and all its subfolders, leaving archived ones out.
func (repository *Repository) SessionCount(folderId string) int {
	count := 0
	for _, session := range repository.Sessions {
		if session.FolderId == folderId && !session.Archived {
			count++
		}
	}
	for _, folder := range repository.Folders {
		if folder.ParentId == folderId && folderId != "" {
			count += repository.SessionCount(folder.Id)
		}
	}
	return count
}

// SetArchived archives a session, or brings it back. What den proposed from it stays, since a finding outlives its session.
func (repository *Repository) SetArchived(sessionId string, archived bool) {
	session := repository.FindSession(sessionId)
	if session == nil {
		return
	}
	session.Archived = archived
	session.ArchivedAt = time.Time{}
	if archived {
		session.ArchivedAt = time.Now()
	}
}

// ArchiveIdle archives the sessions untouched for ArchiveAfter, counted from when den started tracking them at the earliest,
// leaving out the ones open in a pane. It tells how many it archived.
func (repository *Repository) ArchiveIdle(now time.Time, openSessionIds map[string]bool) int {
	archived := 0
	for index := range repository.Sessions {
		session := &repository.Sessions[index]
		lastTouched := session.LastActivityAt
		if session.CreatedAt.After(lastTouched) {
			lastTouched = session.CreatedAt
		}
		if session.Archived || openSessionIds[session.SessionId] || now.Sub(lastTouched) < ArchiveAfter {
			continue
		}
		repository.SetArchived(session.SessionId, true)
		archived++
	}
	return archived
}

// ForgetExpiredArchive drops sessions archived longer than ArchiveKeepsFor and tells how many went.
// Sessions archived before den recorded the time start counting now.
func (repository *Repository) ForgetExpiredArchive(now time.Time) int {
	before := len(repository.Sessions)
	repository.Sessions = slices.DeleteFunc(repository.Sessions, func(session Session) bool {
		isExpired := session.Archived && !session.ArchivedAt.IsZero() && now.Sub(session.ArchivedAt) > ArchiveKeepsFor
		if isExpired {
			repository.IgnoredSessionIds = append(repository.IgnoredSessionIds, session.SessionId)
		}
		return isExpired
	})
	for index := range repository.Sessions {
		if session := &repository.Sessions[index]; session.Archived && session.ArchivedAt.IsZero() {
			session.ArchivedAt = now
		}
	}
	return before - len(repository.Sessions)
}

// ArchiveTimeLeft is how long until den forgets an archived session.
func (session Session) ArchiveTimeLeft(now time.Time) time.Duration {
	return max(session.ArchivedAt.Add(ArchiveKeepsFor).Sub(now), 0)
}

func (repository *Repository) ArchivedCount() int {
	count := 0
	for _, session := range repository.Sessions {
		if session.Archived {
			count++
		}
	}
	return count
}

// FolderPath lists a folder and its ancestors from the top level down.
func (repository *Repository) FolderPath(folderId string) []Folder {
	var path []Folder
	for folder := repository.FindFolder(folderId); folder != nil; folder = repository.FindFolder(folder.ParentId) {
		path = append([]Folder{*folder}, path...)
	}
	return path
}

// FolderName names a folder for display; the top level is the Inbox.
func (repository *Repository) FolderName(folderId string) string {
	if folder := repository.FindFolder(folderId); folder != nil {
		return folder.Name
	}
	return "Inbox"
}

func repositoryFilePath(repositoryPath string) (string, error) {
	dataDirectory, err := DataDirectory()
	if err != nil {
		return "", err
	}
	repositoryId := strings.NewReplacer("/", "-", ".", "-").Replace(repositoryPath)
	return filepath.Join(dataDirectory, "repos", repositoryId, "den.json"), nil
}

func newId() string {
	var randomBytes [4]byte
	rand.Read(randomBytes[:])
	return hex.EncodeToString(randomBytes[:])
}
