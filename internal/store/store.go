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
}

type Session struct {
	Tool           string    `json:"tool"`
	SessionId      string    `json:"sessionId"`
	FolderId       string    `json:"folderId,omitempty"`
	Title          string    `json:"title"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

type Repository struct {
	RepositoryPath string    `json:"repoPath"`
	Folders        []Folder  `json:"folders"`
	Sessions       []Session `json:"sessions"`
}

func DataDirectory() (string, error) {
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
	var randomBytes [4]byte
	rand.Read(randomBytes[:])
	folder := Folder{Id: hex.EncodeToString(randomBytes[:]), Name: name, ParentId: parentId}
	repository.Folders = append(repository.Folders, folder)
	return folder
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
}

// SessionCount counts the sessions in a folder and all its subfolders.
func (repository *Repository) SessionCount(folderId string) int {
	count := 0
	for _, session := range repository.Sessions {
		if session.FolderId == folderId {
			count++
		}
	}
	for _, folder := range repository.Folders {
		if folder.ParentId == folderId {
			count += repository.SessionCount(folder.Id)
		}
	}
	return count
}

func repositoryFilePath(repositoryPath string) (string, error) {
	dataDirectory, err := DataDirectory()
	if err != nil {
		return "", err
	}
	repositoryId := strings.NewReplacer("/", "-", ".", "-").Replace(repositoryPath)
	return filepath.Join(dataDirectory, "repos", repositoryId, "den.json"), nil
}
