package tui

import (
	"context"
	"strings"
	"time"

	"github.com/dimetron/pi-go/internal/extension"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type extensionBridge struct {
	ch     <-chan extension.UIIntentEnvelope
	cancel func()
}

func newExtensionBridge(manager *extension.Manager) *extensionBridge {
	if manager == nil {
		return nil
	}
	ch, cancel := manager.SubscribeUIIntents(64)
	return &extensionBridge{
		ch:     ch,
		cancel: cancel,
	}
}

func (b *extensionBridge) Channel() <-chan extension.UIIntentEnvelope {
	if b == nil {
		return nil
	}
	return b.ch
}

func (b *extensionBridge) Close() {
	if b == nil || b.cancel == nil {
		return
	}
	b.cancel()
	b.cancel = nil
}

func (m *model) resetExtensionBridge(manager *extension.Manager) {
	m.closeExtensionBridge()
	if manager == nil || m.cfg.DisableExtensionUI {
		return
	}
	m.extensionBridge = newExtensionBridge(manager)
	if m.extensionBridge != nil {
		m.extensionIntentCh = m.extensionBridge.Channel()
	}
}

func (m *model) closeExtensionBridge() {
	if m.extensionBridge != nil {
		m.extensionBridge.Close()
	}
	m.extensionBridge = nil
	m.extensionIntentCh = nil
}

func (m *model) handleExtensionIntent(msg extensionIntentMsg) (tea.Model, tea.Cmd) {
	if msg.closed {
		return m, nil
	}
	if m.cfg.DisableExtensionUI {
		return m, waitForExtensionIntent(msg.ch)
	}
	if err := msg.envelope.Intent.Validate(); err != nil {
		return m, waitForExtensionIntent(msg.ch)
	}

	env := msg.envelope
	switch env.Intent.Type {
	case extension.UIIntentStatus:
		if env.Intent.Status != nil {
			m.statusModel.ExtensionStatus = env.Intent.Status.Text
		}
	case extension.UIIntentWidget:
		if env.Intent.Widget != nil {
			state := &extensionWidgetState{
				owner:   env.ExtensionID,
				content: env.Intent.Widget.Content,
			}
			switch env.Intent.Widget.Placement {
			case extension.WidgetPlacementAboveEditor:
				m.extensionWidgetAbove = state
			case extension.WidgetPlacementBelowEditor:
				m.extensionWidgetBelow = state
			}
		}
	case extension.UIIntentNotification:
		if env.Intent.Notification != nil {
			rendered := m.renderExtensionContent(
				env.ExtensionID,
				extension.RenderSurfaceChatMessage,
				map[string]any{
					"type":    string(extension.UIIntentNotification),
					"content": env.Intent.Notification.Content.Content,
					"level":   env.Intent.Notification.Level,
				},
				env.Intent.Notification.Content,
			)
			level := strings.ToLower(strings.TrimSpace(env.Intent.Notification.Level))
			warning := level == "warn" || level == "warning" || level == "error"
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role:           "assistant",
				content:        rendered,
				isWarning:      warning,
				extensionOwner: env.ExtensionID,
			})
			m.chatModel.Scroll = 0
		}
	case extension.UIIntentDialog:
		if env.Intent.Dialog != nil {
			m.extensionDialog = &extensionDialogState{
				owner:   env.ExtensionID,
				title:   env.Intent.Dialog.Title,
				content: env.Intent.Dialog.Content,
				modal:   env.Intent.Dialog.Modal,
			}
		}
	}
	return m, waitForExtensionIntent(msg.ch)
}

func (m *model) renderExtensionContent(
	extensionID string,
	surface extension.RenderSurface,
	payload map[string]any,
	content extension.RenderContent,
) string {
	timeout := 250 * time.Millisecond
	if m.cfg.ExtensionManager != nil {
		if rendered, ok, err := m.cfg.ExtensionManager.Render(
			context.Background(),
			extensionID,
			surface,
			payload,
			timeout,
		); ok && err == nil {
			return m.renderExtensionResult(rendered.Kind, rendered.Content)
		}
	}
	return m.renderExtensionResult(content.Kind, content.Content)
}

func (m *model) renderExtensionResult(kind extension.RenderKind, content string) string {
	if kind == extension.RenderKindMarkdown {
		return m.chatModel.RenderMarkdown(content)
	}
	return content
}

func (m *model) renderExtensionWidget(widget *extensionWidgetState) string {
	if widget == nil {
		return ""
	}
	return m.renderExtensionContent(
		widget.owner,
		extension.RenderSurfaceChatMessage,
		map[string]any{"type": "widget", "content": widget.content.Content},
		widget.content,
	)
}

func (m *model) renderExtensionDialog(width int) string {
	if m.extensionDialog == nil {
		return ""
	}
	content := m.renderExtensionContent(
		m.extensionDialog.owner,
		extension.RenderSurfaceChatMessage,
		map[string]any{"type": "dialog", "title": m.extensionDialog.title, "content": m.extensionDialog.content.Content},
		m.extensionDialog.content,
	)
	title := strings.TrimSpace(m.extensionDialog.title)
	if title != "" {
		header := lipgloss.NewStyle().Bold(true).Render(title)
		content = header + "\n\n" + content
	}

	dialogWidth := width - 6
	if dialogWidth < 20 {
		dialogWidth = 20
	}
	if dialogWidth > 80 {
		dialogWidth = 80
	}
	style := lipgloss.NewStyle().
		Width(dialogWidth).
		Border(lipgloss.RoundedBorder(), true, true, true, true).
		BorderForeground(lipgloss.Color("75")).
		Padding(0, 1)
	return style.Render(content)
}
