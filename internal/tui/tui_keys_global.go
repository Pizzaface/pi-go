package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *model) handleGlobalKey(msg tea.KeyPressMsg, key tea.Key) (tea.Model, tea.Cmd) {
	if key.Code == tea.KeyTab && m.inputModel.Text == "/" {
		m.openSlashCommandOverlay()
		if m.slashOverlay != nil {
			return m, nil
		}
	}

	switch {
	case key.Code == tea.KeyEsc:
		if m.inputModel.InCompletionMode() {
			m.inputModel.DismissCompletion()
			return m, nil
		}
		if m.running {
			m.cancelAgent()
			return m, nil
		}
		return m, nil
	case key.Code == 'c' && key.Mod == tea.ModCtrl:
		if m.inputModel.InCompletionMode() {
			m.inputModel.DismissCompletion()
			return m, nil
		}
		if m.running {
			m.cancelAgent()
			return m, nil
		}
		m.ctrlCCount++
		if m.ctrlCCount >= 2 {
			m.quitting = true
			return m, tea.Quit
		}
		m.chatModel.AppendWarning("\nCtrl+C again to quit (or wait 2s)...")
		return m, resetCtrlCCount(m)
	case key.Code == tea.KeyF12:
		m.toggleDebugPanel()
		return m, nil
	}

	if m.running || m.loading {
		return m, nil
	}

	if key.Code == 'o' && key.Mod == tea.ModCtrl {
		m.chatModel.ToolDisplay.CompactTools = !m.chatModel.ToolDisplay.CompactTools
		return m, nil
	}
	if key.Code == 'b' && key.Mod == tea.ModCtrl {
		if m.statusModel.GitBranch != "" {
			if m.branchPopup == nil {
				m.newBranchPopup()
			} else {
				m.branchPopup = nil
			}
		}
		return m, nil
	}

	switch key.Code {
	case tea.KeyPgUp:
		m.chatModel.ScrollUp(5, m.height)
		return m, nil
	case tea.KeyPgDown:
		m.chatModel.ScrollDown(5)
		return m, nil
	}

	cmd := m.inputModel.HandleKey(msg)
	if key.Text == "/" && m.inputModel.Text == "/" && m.slashOverlay == nil {
		m.openSlashCommandOverlay()
	}
	return m, cmd
}
