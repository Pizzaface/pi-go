package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

func formatExtensionPayload(kind string, payload any) string {
	if payload == nil {
		return "[" + kind + "]"
	}
	if s, ok := payload.(string); ok {
		return s
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "[" + kind + "] (unrenderable payload)"
	}
	return string(b)
}

func joinContent(parts []piapi.ContentPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// startTurnWithText kicks off an agent turn with the given user-visible
// text. Shares submitPrompt's path so extension-driven turns behave like
// user-typed turns.
func (m *model) startTurnWithText(text string) tea.Cmd {
	_, cmd := m.submitPrompt(text, nil)
	return cmd
}

// abortCurrentTurn cancels any running turn. Used by SendUserMessage
// delivery mode "steer".
func (m *model) abortCurrentTurn() {
	if m.running {
		m.cancelAgent()
	}
}
