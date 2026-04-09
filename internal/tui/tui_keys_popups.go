package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *model) handleBranchPopupKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.branchPopup == nil {
		return false, nil, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		m.branchPopup = nil
		return true, m, nil
	case tea.KeyEnter:
		nextM, cmd := m.handleBranchSelect()
		return true, nextM, cmd
	case tea.KeyUp:
		if m.branchPopup.selected > 0 {
			m.branchPopup.selected--
			if m.branchPopup.selected < m.branchPopup.scrollOff {
				m.branchPopup.scrollOff--
			}
		}
		return true, m, nil
	case tea.KeyDown:
		if m.branchPopup.selected < len(m.branchPopup.branches)-1 {
			m.branchPopup.selected++
			if m.branchPopup.selected >= m.branchPopup.scrollOff+m.branchPopup.height {
				m.branchPopup.scrollOff++
			}
		}
		return true, m, nil
	default:
		m.branchPopup = nil
		return true, m, nil
	}
}

func (m *model) handleModelPickerKey(msg tea.KeyPressMsg, key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.modelPicker == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEsc:
		m.modelPicker = nil
		return true, m, nil
	case key.Code == tea.KeyEnter:
		nextM, cmd := m.handleModelSelect()
		return true, nextM, cmd
	case key.Code == tea.KeyUp:
		m.modelPicker.moveUp()
		return true, m, nil
	case key.Code == tea.KeyDown:
		m.modelPicker.moveDown()
		return true, m, nil
	case key.Code == tea.KeyBackspace:
		if len(m.modelPicker.filter) > 0 {
			m.modelPicker.filter = m.modelPicker.filter[:len(m.modelPicker.filter)-1]
			m.modelPicker.applyFilter()
		}
		return true, m, nil
	case key.String() == "H":
		if id := m.modelPicker.toggleHidden(); id != "" {
			saveHiddenModels(m.modelPicker.hidden)
			m.modelPicker.applyFilter()
		}
		return true, m, nil
	case key.String() == "S":
		m.modelPicker.showHidden = !m.modelPicker.showHidden
		m.modelPicker.applyFilter()
		return true, m, nil
	default:
		if key.Text != "" && key.Mod == 0 {
			m.modelPicker.filter += key.Text
			m.modelPicker.applyFilter()
			return true, m, nil
		}
		m.modelPicker = nil
		return true, m, nil
	}
}

func (m *model) handleLoginPickerKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.loginPicker == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEsc:
		m.loginPicker = nil
		return true, m, nil
	case key.Code == tea.KeyEnter:
		nextM, cmd := m.handleLoginPickerSelect()
		return true, nextM, cmd
	case key.Code == tea.KeyUp:
		m.loginPicker.moveUp()
		return true, m, nil
	case key.Code == tea.KeyDown:
		m.loginPicker.moveDown()
		return true, m, nil
	default:
		m.loginPicker = nil
		return true, m, nil
	}
}

func (m *model) handleSlashOverlayKey(msg tea.KeyPressMsg, key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.slashOverlay == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEsc:
		m.slashOverlay = nil
		return true, m, nil
	case key.Code == tea.KeyEnter:
		if row, ok := m.slashOverlay.SelectedRow(); ok {
			m.inputModel.Text = row.Name
			m.inputModel.CursorPos = len(m.inputModel.Text)
			m.inputModel.CyclingIdx = -1
			m.slashOverlay = nil
		}
		return true, m, nil
	case key.Code == tea.KeyUp:
		m.slashOverlay.Move(-1)
		return true, m, nil
	case key.Code == tea.KeyDown:
		m.slashOverlay.Move(1)
		return true, m, nil
	case key.Code == tea.KeyTab && key.Mod == tea.ModShift:
		return true, m, nil
	case key.Code == tea.KeyTab:
		return true, m, nil
	default:
		cmd := m.inputModel.HandleKey(msg, m.running)
		if m.inputModel.Text != "/" {
			m.slashOverlay = nil
		}
		return true, m, cmd
	}
}
