package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// extensionToastState tracks the "N extensions pending — press e to review"
// status-line indicator. Hidden when pending==0 OR dismissed==true (until
// pending rises again).
type extensionToastState struct {
	pending   int
	dismissed bool
	lastCount int
}

// SetPending updates the count from a lifecycle.List() scan. Whenever
// the count rises above the last-seen count, dismissed is cleared so
// the toast reappears for the new pending extension.
func (s *extensionToastState) SetPending(n int) {
	if n > s.lastCount {
		s.dismissed = false
	}
	s.lastCount = n
	s.pending = n
}

// Dismiss hides the toast until SetPending raises the count again.
func (s *extensionToastState) Dismiss() { s.dismissed = true }

var toastStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("214")).
	Bold(true)

// View renders the toast line. Empty string = no render.
func (s *extensionToastState) View() string {
	if s.pending == 0 || s.dismissed {
		return ""
	}
	text := fmt.Sprintf("%d extension", s.pending)
	if s.pending != 1 {
		text += "s"
	}
	text += " pending approval — press e to review"
	return toastStyle.Render(text)
}
