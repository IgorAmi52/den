// Package eval packs what den knows on this machine into one archive, so den can be evaluated and improved from real use.
package eval

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// Summary says what an export packed.
type Summary struct {
	Path               string
	Bytes              int64
	Repositories       int
	Sessions           int
	Transcripts        int
	MissingTranscripts int
	Calls              int
	Decisions          int
}

// manifest is the archive's index: who exported it, from which den, with which agent tools, and what each repo holds.
type manifest struct {
	ExportedAt         time.Time            `json:"exportedAt"`
	DenVersion         string               `json:"denVersion"`
	User               string               `json:"user"`
	Platform           string               `json:"platform"`
	ToolVersions       map[string]string    `json:"toolVersions"`
	Repositories       []manifestRepository `json:"repositories"`
	MissingTranscripts []string             `json:"missingTranscripts,omitempty"`
}

type manifestRepository struct {
	Path string `json:"path"`
	// Directory is the repo's folder under data/repos in the archive.
	Directory string `json:"directory"`
	Sessions  int    `json:"sessions"`
	Folders   int    `json:"folders"`
	Calls     int    `json:"calls"`
	Decisions int    `json:"decisions"`
}

// Export writes den-eval-<user>-<time>.tar.gz into outputDirectory. Inside one folder of that name it holds:
// data, all of den's own directory but its scratch files; transcripts, the whole transcript of every session den tracks;
// linked, the user's files that folders use as context; laptop, the user's own rule files; and manifest.json.
// A failed export leaves no archive behind.
func Export(outputDirectory string, now time.Time) (exportSummary Summary, exportError error) {
	dataDirectory, err := store.DataDirectory()
	if err != nil {
		return Summary{}, err
	}
	userName := "unknown"
	if current, err := user.Current(); err == nil {
		userName = current.Username
	}
	archiveName := "den-eval-" + userName + "-" + now.Format("20060102-1504")
	summary := Summary{Path: filepath.Join(outputDirectory, archiveName+".tar.gz")}
	archiveFile, err := os.Create(summary.Path)
	if err != nil {
		return Summary{}, err
	}
	defer func() {
		archiveFile.Close()
		if exportError != nil {
			os.Remove(summary.Path)
		}
	}()
	gzipWriter := gzip.NewWriter(archiveFile)
	tarWriter := tar.NewWriter(gzipWriter)
	packer := packer{tarWriter: tarWriter, root: archiveName}

	if err := packer.addDirectory("data", dataDirectory, filepath.Join(dataDirectory, "tmp"), filepath.Join(dataDirectory, "gate", "venv"), filepath.Join(dataDirectory, "gate", "model", "model")); err != nil {
		return Summary{}, err
	}
	exported := manifest{ExportedAt: now, DenVersion: store.DenVersion(), User: userName,
		Platform: runtime.GOOS + "/" + runtime.GOARCH, ToolVersions: toolVersions()}
	for _, repositoryPath := range store.KnownRepositoryPaths() {
		repository, err := store.Load(repositoryPath)
		if err != nil {
			return Summary{}, err
		}
		repositoryDirectory, err := repository.Directory()
		if err != nil {
			return Summary{}, err
		}
		entry := manifestRepository{Path: repositoryPath, Directory: filepath.Base(repositoryDirectory),
			Sessions: len(repository.Sessions), Folders: len(repository.Folders),
			Calls: countLines(filepath.Join(repositoryDirectory, "calls.jsonl")), Decisions: countLines(filepath.Join(repositoryDirectory, "decisions.jsonl"))}
		exported.Repositories = append(exported.Repositories, entry)
		summary.Repositories++
		summary.Sessions += entry.Sessions
		summary.Calls += entry.Calls
		summary.Decisions += entry.Decisions
		for _, session := range repository.Sessions {
			adapter, known := agent.ByName(session.Tool)
			if !known {
				continue
			}
			transcriptPath, found, err := adapter.TranscriptPath(session.SessionId)
			if err != nil || !found {
				exported.MissingTranscripts = append(exported.MissingTranscripts, session.Tool+"/"+session.SessionId)
				continue
			}
			if err := packer.addFile(filepath.Join("transcripts", session.Tool, session.SessionId+".jsonl"), transcriptPath); err != nil {
				return Summary{}, err
			}
			summary.Transcripts++
		}
		for _, folder := range repository.Folders {
			if folder.LinkedPath == "" {
				continue
			}
			if err := packer.addFile(filepath.Join("linked", entry.Directory, folder.Id, filepath.Base(folder.LinkedPath)), folder.LinkedPath); err != nil && !os.IsNotExist(err) {
				return Summary{}, err
			}
		}
	}
	summary.MissingTranscripts = len(exported.MissingTranscripts)
	for _, adapter := range agent.Installed() {
		if laptopFilePath, err := adapter.LaptopFilePath(); err == nil {
			if err := packer.addFile(filepath.Join("laptop", adapter.Name(), filepath.Base(laptopFilePath)), laptopFilePath); err != nil && !os.IsNotExist(err) {
				return Summary{}, err
			}
		}
	}
	manifestJson, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return Summary{}, err
	}
	if err := packer.addBytes("manifest.json", manifestJson, now); err != nil {
		return Summary{}, err
	}
	if err := tarWriter.Close(); err != nil {
		return Summary{}, err
	}
	if err := gzipWriter.Close(); err != nil {
		return Summary{}, err
	}
	if information, err := archiveFile.Stat(); err == nil {
		summary.Bytes = information.Size()
	}
	return summary, nil
}

// packer writes files into the archive under its root folder.
type packer struct {
	tarWriter *tar.Writer
	root      string
}

func (packer packer) addFile(archivePath string, sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	information, err := source.Stat()
	if err != nil {
		return err
	}
	header := &tar.Header{Name: filepath.ToSlash(filepath.Join(packer.root, archivePath)), Mode: 0o644, Size: information.Size(), ModTime: information.ModTime()}
	if err := packer.tarWriter.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.Copy(packer.tarWriter, source)
	return err
}

func (packer packer) addBytes(archivePath string, content []byte, modifiedTime time.Time) error {
	header := &tar.Header{Name: filepath.ToSlash(filepath.Join(packer.root, archivePath)), Mode: 0o644, Size: int64(len(content)), ModTime: modifiedTime}
	if err := packer.tarWriter.WriteHeader(header); err != nil {
		return err
	}
	_, err := packer.tarWriter.Write(content)
	return err
}

func (packer packer) addDirectory(archivePath string, sourceDirectory string, skippedDirectories ...string) error {
	return filepath.WalkDir(sourceDirectory, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && slices.Contains(skippedDirectories, filePath) {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relativePath, err := filepath.Rel(sourceDirectory, filePath)
		if err != nil {
			return err
		}
		return packer.addFile(filepath.Join(archivePath, relativePath), filePath)
	})
}

// toolVersions asks each installed agent CLI for its version, since den's behavior depends on theirs.
func toolVersions() map[string]string {
	versions := map[string]string{}
	for _, adapter := range agent.Installed() {
		output, err := exec.Command(adapter.Name(), "--version").Output()
		if err != nil {
			versions[adapter.Name()] = "unknown"
			continue
		}
		firstLine, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
		versions[adapter.Name()] = firstLine
	}
	return versions
}

func countLines(filePath string) int {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0
	}
	return bytes.Count(content, []byte("\n"))
}
