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
	// Branch is set on subfolders that group the sessions of one branch or worktree.
	Branch string `json:"branch,omitempty"`
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
	// WithoutContext is set for sessions started with N, so resuming them leaves the folder's context out too.
	WithoutContext bool      `json:"withoutContext,omitempty"`
	Archived       bool      `json:"archived,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

type Repository struct {
	RepositoryPath string `json:"repoPath"`
	// Welcomed is set once the first-run review is done, so it shows only once per repo.
	Welcomed    bool         `json:"welcomed,omitempty"`
	Folders     []Folder     `json:"folders"`
	Sessions    []Session    `json:"sessions"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
	// RejectedKeys remembers rejected suggestions, so the same one is never offered again.
	RejectedKeys []string `json:"rejectedKeys,omitempty"`
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

// DeleteFolder removes only the folder: its subfolders and sessions move up to its parent.
func (repository *Repository) DeleteFolder(folderId string) {
	folder := repository.FindFolder(folderId)
	if folder == nil {
		return
	}
	parentId := folder.ParentId
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
		return suggestion.FolderId == folderId || suggestion.ParentFolderId == folderId
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
