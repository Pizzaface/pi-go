package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *model) handleSetupAlertKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if !m.setupAlert {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEnter, key.Code == tea.KeyEsc, key.Code == ' ':
		m.setupAlert = false
		return true, m, nil
	default:
		return true, m, nil
	}
}

func (m *model) handleCommitConfirmKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.running || m.commit == nil || m.commit.phase != "confirming" {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEnter:
		nextM, cmd := m.handleCommitConfirm()
		return true, nextM, cmd
	case key.Code == tea.KeyEsc:
		nextM, cmd := m.handleCommitCancel()
		return true, nextM, cmd
	case key.Code == 'c' && key.Mod == tea.ModCtrl:
		nextM, cmd := m.handleCommitCancel()
		return true, nextM, cmd
	default:
		return true, m, nil
	}
}

func (m *model) handleLoginFlowKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.running || m.login == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEsc:
		nextM, cmd := m.handleLoginCancel()
		return true, nextM, cmd
	case key.Code == 'c' && key.Mod == tea.ModCtrl:
		nextM, cmd := m.handleLoginCancel()
		return true, nextM, cmd
	case key.Code == tea.KeyEnter && m.login.phase == "waiting":
		apiKey := strings.TrimSpace(m.inputModel.Text)
		if apiKey == "" {
			return true, m, nil
		}
		m.inputModel.Clear()
		nextM, cmd := m.handleLoginSave(apiKey)
		return true, nextM, cmd
	}
	if m.login.phase != "waiting" {
		return true, m, nil
	}
	return false, nil, nil
}

func (m *model) handleSkillCreateKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.running || m.pendingSkillCreate == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEnter:
		nextM, cmd := m.handleSkillCreateConfirm()
		return true, nextM, cmd
	case key.Code == tea.KeyEsc:
		nextM, cmd := m.handleSkillCreateCancel()
		return true, nextM, cmd
	case key.Code == 'c' && key.Mod == tea.ModCtrl:
		nextM, cmd := m.handleSkillCreateCancel()
		return true, nextM, cmd
	default:
		return true, m, nil
	}
}

func (m *model) handleExtensionDialogKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.extensionDialog == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEsc, key.Code == tea.KeyEnter, key.Code == 'c' && key.Mod == tea.ModCtrl:
		m.extensionDialog = nil
	}
	return true, m, nil
}
