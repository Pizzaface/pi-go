package tui

import (
	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/internal/extension/lifecycle"

	tea "charm.land/bubbletea/v2"
)

// extensionEventMsg wraps a lifecycle.Event for the bubbletea loop.
type extensionEventMsg struct {
	event lifecycle.Event
}

// startExtensionEventBridge subscribes to lifecycle events and returns a
// tea.Cmd that reads one event at a time and re-queues itself.
func (m *model) startExtensionEventBridge() tea.Cmd {
	if m.lifecycle == nil {
		return nil
	}
	ch, cancel := m.lifecycle.Subscribe()
	m.extensionEventCancel = cancel
	m.extensionEventCh = ch
	return waitForExtensionEvent(ch)
}

func waitForExtensionEvent(ch <-chan lifecycle.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return extensionEventMsg{event: ev}
	}
}

// refreshExtensionToast counts pending hosted extensions and updates the toast.
func (m *model) refreshExtensionToast() {
	if m.lifecycle == nil {
		return
	}
	n := 0
	for _, v := range m.lifecycle.List() {
		if v.State == host.StatePending {
			n++
		}
	}
	m.extensionToast.SetPending(n)
}
