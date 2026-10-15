package agent

type Claude struct{}

func (Claude) Name() string {
	return "claude"
}

func (Claude) NewSessionCommand() []string {
	return []string{"claude"}
}
