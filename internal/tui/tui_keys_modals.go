package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/pizzaface/go-pi/internal/extension/host"
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

// handleExtensionDialogKey handles keys when the approval dialog is open.
func (m *model) handleExtensionDialogKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.extensionApproval == nil {
		return false, nil, nil
	}
	switch key.Code {
	case tea.KeyEscape:
		m.extensionApproval = nil
		return true, m, nil
	case tea.KeyEnter:
		grants := m.extensionApproval.SelectedGrants()
		id := m.extensionApproval.id
		m.extensionApproval = nil
		if m.lifecycle != nil {
			if err := m.lifecycle.Approve(m.ctx, id, grants); err != nil {
				m.appendAssistant("Approve failed: " + err.Error())
			}
			m.extensionPanel.SetViews(m.lifecycle.List())
			m.refreshExtensionToast()
		}
		return true, m, nil
	case tea.KeySpace:
		m.extensionApproval.Toggle()
		return true, m, nil
	case tea.KeyUp:
		m.extensionApproval.MoveSelection(-1)
		return true, m, nil
	case tea.KeyDown:
		m.extensionApproval.MoveSelection(1)
		return true, m, nil
	default:
		return true, m, nil
	}
}

// handleExtensionsPanelKey handles keys when the /extensions panel is open.
func (m *model) handleExtensionsPanelKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if !m.extensionPanel.Open() {
		return false, nil, nil
	}
	switch key.Code {
	case tea.KeyEscape:
		m.extensionPanel.Close()
		return true, m, nil
	case tea.KeyUp:
		m.extensionPanel.MoveSelection(-1)
		return true, m, nil
	case tea.KeyDown:
		m.extensionPanel.MoveSelection(1)
		return true, m, nil
	default:
		// Handle rune keys for actions.
		if len(key.Text) == 1 {
			r := rune(key.Text[0])
			if r == 'a' {
				// Open approval dialog for the selected extension.
				views := m.extensionPanel.filteredViews()
				if len(views) > 0 && m.extensionPanel.selected < len(views) {
					v := views[m.extensionPanel.selected]
					if v.State == host.StatePending {
						m.extensionApproval = newApprovalDialog(v.ID, v.Version, v.Mode, v.Requested)
					}
				}
				return true, m, nil
			}
			if m.lifecycle != nil {
				if err := m.extensionPanel.DispatchKey(m.ctx, m.lifecycle, r); err != nil {
					m.appendAssistant(err.Error())
				}
				m.extensionPanel.SetViews(m.lifecycle.List())
				m.refreshExtensionToast()
			}
			return true, m, nil
		}
		return true, m, nil
	}
}
