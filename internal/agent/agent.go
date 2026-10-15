package agent

import "os/exec"

type Adapter interface {
	Name() string
	NewSessionCommand() []string
}

var supportedAdapters = []Adapter{Claude{}, Codex{}}

func Installed() []Adapter {
	var installedAdapters []Adapter
	for _, adapter := range supportedAdapters {
		if _, err := exec.LookPath(adapter.Name()); err == nil {
			installedAdapters = append(installedAdapters, adapter)
		}
	}
	return installedAdapters
}
