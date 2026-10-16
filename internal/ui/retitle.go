package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/IgorAmi52/den/internal/store"
	"github.com/IgorAmi52/den/internal/tmux"
)

const (
	animationFrame = 30 * time.Millisecond
	// The old name is erased, then the new one typed, each within a capped time however long the names are,
	// and the row stays highlighted a moment after.
	eraseDuration = 300 * time.Millisecond
	typeDuration  = 500 * time.Millisecond
	holdDuration  = 1500 * time.Millisecond
)

type animationTickMessage struct{}

// retitleAnimation retypes a session's name in place: the old name is erased, then the new one typed.
type retitleAnimation struct {
	from      []rune
	to        []rune
	startedAt time.Time
}

// frame is the name to show at a moment, and whether the animation is still running.
func (animation retitleAnimation) frame(now time.Time) (text string, isTyping bool, isRunning bool) {
	elapsed := now.Sub(animation.startedAt)
	switch {
	case elapsed < eraseDuration:
		kept := len(animation.from) - int(float64(len(animation.from))*float64(elapsed)/float64(eraseDuration))
		return string(animation.from[:max(kept, 0)]), true, true
	case elapsed < eraseDuration+typeDuration:
		typed := int(float64(len(animation.to)) * float64(elapsed-eraseDuration) / float64(typeDuration))
		return string(animation.to[:min(typed, len(animation.to))]), true, true
	case elapsed < eraseDuration+typeDuration+holdDuration:
		return string(animation.to), false, true
	}
	return string(animation.to), false, false
}

// retitle renames a session and its pane, and retypes the name on its row.
func (currentModel *model) retitle(session *store.Session, title string) {
	currentModel.retitles[session.SessionId] = retitleAnimation{from: []rune(session.Title), to: []rune(title), startedAt: time.Now()}
	session.Title = title
	if open := currentModel.openAgentFor(session.SessionId); open != nil {
		currentModel.reportError(tmux.SetPaneOption(open.paneId, paneTitleOption, title))
	}
}

// animateRetitles starts the animation ticks, unless they are already running.
func (currentModel *model) animateRetitles() tea.Cmd {
	if len(currentModel.retitles) == 0 || currentModel.animating {
		return nil
	}
	currentModel.animating = true
	return animationTick()
}

// stepRetitles drops the finished animations and keeps ticking while any are left.
func (currentModel *model) stepRetitles() tea.Cmd {
	now := time.Now()
	for sessionId, animation := range currentModel.retitles {
		if _, _, isRunning := animation.frame(now); !isRunning {
			delete(currentModel.retitles, sessionId)
		}
	}
	if len(currentModel.retitles) == 0 {
		currentModel.animating = false
		return nil
	}
	return animationTick()
}

func animationTick() tea.Cmd {
	return tea.Tick(animationFrame, func(time.Time) tea.Msg {
		return animationTickMessage{}
	})
}

// sessionTitleSegments shows a session's name, mid-retype while den renames it, in the accent color until it settles.
func (currentModel model) sessionTitleSegments(session *store.Session) []segment {
	theme := currentModel.theme
	animation, isRetitled := currentModel.retitles[session.SessionId]
	if !isRetitled {
		style := theme.text
		if session.Archived {
			style = theme.dim
		}
		return []segment{{text: " " + session.Title, style: style}}
	}
	text, isTyping, _ := animation.frame(time.Now())
	segments := []segment{{text: " " + text, style: theme.accent}}
	if isTyping {
		segments = append(segments, segment{text: "▏", style: theme.accentBold})
	}
	return segments
}
