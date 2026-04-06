package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
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
	if handled, nextM, cmd := m.handleBranchPopupKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleModelPickerKey(msg, key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleLoginPickerKey(key); handled {
		return nextM, cmd
	}
	if handled, nextM, cmd := m.handleSlashOverlayKey(msg, key); handled {
		return nextM, cmd
	}

	return m.handleGlobalKey(msg, key)
}
