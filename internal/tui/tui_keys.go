package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// Map screen Y to absolute rendered line and check for Agent accordion toggles.
	absLine := m.lastViewStartLine + msg.Y
	for _, r := range m.chatModel.AgentLineRanges {
		if absLine >= r.startLine && absLine < r.endLine {
			if r.msgIndex >= 0 && r.msgIndex < len(m.chatModel.Messages) {
				m.chatModel.Messages[r.msgIndex].collapsed = !m.chatModel.Messages[r.msgIndex].collapsed
			}
			break
		}
	}
	return m, nil
}

func (m *model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseWheelUp {
		m.chatModel.ScrollUp(3, m.height)
	} else if msg.Button == tea.MouseWheelDown {
		m.chatModel.ScrollDown(3)
	}
	return m, nil
}

func (m *model) toggleDebugPanel() {
	m.debugPanel = !m.debugPanel
	mainWidth := m.layoutMainWidth()
	m.statusModel.Width = mainWidth
	m.chatModel.UpdateRenderer(mainWidth)
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	if handled, nextM, cmd := m.handleSetupAlertKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleCommitConfirmKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleLoginFlowKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleSkillCreateKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleExtensionDialogKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleExtensionsPanelKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleBranchPopupKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleModelPickerKey(msg, key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleLoginPickerKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleSessionPickerKey(msg, key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleSlashOverlayKey(msg, key); handled {
		return nextM, cmd
	}

	return m.handleGlobalKey(msg, key)
}
