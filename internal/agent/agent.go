package agent

import "os/exec"

type Adapter interface {
	Name() string
	NewSessionCommand(workingDirectory string, systemContext string) *exec.Cmd
	ResumeCommand(workingDirectory string, sessionId string) *exec.Cmd
}
