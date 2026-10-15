package agent

type Codex struct{}

func (Codex) Name() string {
	return "codex"
}

func (Codex) NewSessionCommand() []string {
	return []string{"codex"}
}
