package agent

import "os/exec"

type Claude struct{}

func (Claude) Name() string {
	return "claude"
}

func (Claude) NewSessionCommand(workingDirectory string, systemContext string) *exec.Cmd {
	command := exec.Command("claude")
	if systemContext != "" {
		command.Args = append(command.Args, "--append-system-prompt", systemContext)
	}
	command.Dir = workingDirectory
	return command
}

func (Claude) ResumeCommand(workingDirectory string, sessionId string) *exec.Cmd {
	command := exec.Command("claude", "--resume", sessionId)
	command.Dir = workingDirectory
	return command
}
