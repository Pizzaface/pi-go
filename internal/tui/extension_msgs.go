package tui

import (
	"github.com/dimetron/pi-go/internal/extension"

	tea "charm.land/bubbletea/v2"
)

type extensionIntentMsg struct {
	envelope extension.UIIntentEnvelope
	ch       <-chan extension.UIIntentEnvelope
	closed   bool
}

type extensionWidgetState struct {
	owner   string
	content extension.RenderContent
}

type extensionDialogState struct {
	owner   string
	title   string
	content extension.RenderContent
	modal   bool
}

func waitForExtensionIntent(ch <-chan extension.UIIntentEnvelope) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		envelope, ok := <-ch
		if !ok {
			return extensionIntentMsg{ch: ch, closed: true}
		}
		return extensionIntentMsg{envelope: envelope, ch: ch}
	}
}
