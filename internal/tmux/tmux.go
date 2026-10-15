package tmux

import "os"

func InsideSession() bool {
	return os.Getenv("TMUX") != ""
}
