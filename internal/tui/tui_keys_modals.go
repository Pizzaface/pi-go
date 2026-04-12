package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/extension"
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

func (m *model) handleExtensionsPanelKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	if m.extensionsPanel == nil {
		return false, nil, nil
	}

	// Sub-dialog takes precedence.
	if m.extensionsPanel.subDialog != nil {
		return m.handleExtApprovalDialogKey(key)
	}

	switch {
	case key.Code == tea.KeyEsc, key.Code == 'c' && key.Mod == tea.ModCtrl:
		m.extensionsPanel = nil
		return true, m, nil

	case key.Code == tea.KeyUp, key.Code == 'k':
		m.moveExtPanelCursor(-1)
		return true, m, nil

	case key.Code == tea.KeyDown, key.Code == 'j':
		m.moveExtPanelCursor(+1)
		return true, m, nil

	case key.Code == 'R':
		if m.cfg.ExtensionManager != nil {
			return true, m, extensionReloadCmd(m.cfg.ExtensionManager, m.cfg.WorkDir)
		}
		return true, m, nil

	case key.Code == 'a':
		if row, ok := m.selectedExtRow(); ok && (row.info.State == extension.StatePending || row.info.State == extension.StateDenied) {
			m.openExtApprovalDialog(row.info, extensionDialogApprove)
		}
		return true, m, nil

	case key.Code == 'd':
		if row, ok := m.selectedExtRow(); ok && row.info.State == extension.StatePending {
			if m.cfg.ExtensionManager != nil {
				return true, m, extensionDenyCmd(m.cfg.ExtensionManager, row.info.ID)
			}
		}
		return true, m, nil

	case key.Code == 'r':
		if row, ok := m.selectedExtRow(); ok && m.cfg.ExtensionManager != nil {
			switch row.info.State {
			case extension.StateRunning, extension.StateStopped, extension.StateErrored:
				return true, m, extensionRestartCmd(m.cfg.ExtensionManager, row.info.ID)
			}
		}
		return true, m, nil

	case key.Code == 's':
		if row, ok := m.selectedExtRow(); ok && row.info.State == extension.StateRunning && m.cfg.ExtensionManager != nil {
			return true, m, extensionStopCmd(m.cfg.ExtensionManager, row.info.ID)
		}
		return true, m, nil

	case key.Code == 'x':
		if row, ok := m.selectedExtRow(); ok &&
			(row.info.State == extension.StateRunning || row.info.State == extension.StateStopped) {
			m.openExtApprovalDialog(row.info, extensionDialogRevoke)
		}
		return true, m, nil

	case key.Code == tea.KeyEnter:
		if row, ok := m.selectedExtRow(); ok {
			switch row.info.State {
			case extension.StatePending:
				m.openExtApprovalDialog(row.info, extensionDialogApprove)
			case extension.StateRunning, extension.StateStopped, extension.StateErrored:
				if m.cfg.ExtensionManager != nil {
					return true, m, extensionRestartCmd(m.cfg.ExtensionManager, row.info.ID)
				}
			}
		}
		return true, m, nil
	}

	return true, m, nil // swallow other keys while panel is open
}

func (m *model) handleExtApprovalDialogKey(key tea.Key) (bool, tea.Model, tea.Cmd) {
	d := m.extensionsPanel.subDialog
	if d == nil {
		return false, nil, nil
	}
	switch {
	case key.Code == tea.KeyEsc, key.Code == 'c' && key.Mod == tea.ModCtrl:
		m.extensionsPanel.subDialog = nil
		return true, m, nil
	case key.Code == tea.KeyEnter:
		id := d.id
		action := d.action
		m.extensionsPanel.subDialog = nil
		if m.cfg.ExtensionManager == nil {
			return true, m, nil
		}
		if action == extensionDialogApprove {
			return true, m, extensionGrantAndStartCmd(m.cfg.ExtensionManager, m.cfg.WorkDir, id, d.trustClass, d.capabilities)
		}
		return true, m, extensionRevokeCmd(m.cfg.ExtensionManager, id)
	}
	return true, m, nil
}

func (m *model) moveExtPanelCursor(delta int) {
	p := m.extensionsPanel
	if p == nil || len(p.rows) == 0 {
		return
	}
	i := p.cursor + delta
	for i >= 0 && i < len(p.rows) && p.rows[i].isGroup {
		i += delta
	}
	if i < 0 || i >= len(p.rows) {
		return
	}
	p.cursor = i
}

func (m *model) selectedExtRow() (extensionPanelRow, bool) {
	p := m.extensionsPanel
	if p == nil || p.cursor < 0 || p.cursor >= len(p.rows) {
		return extensionPanelRow{}, false
	}
	row := p.rows[p.cursor]
	if row.isGroup {
		return extensionPanelRow{}, false
	}
	return row, true
}

func (m *model) openExtApprovalDialog(info extension.ExtensionInfo, action extensionDialogAction) {
	m.extensionsPanel.subDialog = &extensionApprovalDialogState{
		id:           info.ID,
		trustClass:   info.TrustClass,
		runtime:      info.Runtime,
		capabilities: info.RequestedCapabilities,
		action:       action,
	}
}
