package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		mainWidth := m.layoutMainWidth()
		m.statusModel.Width = mainWidth
		m.chatModel.UpdateRenderer(mainWidth)
	case tea.PasteMsg:
		if !m.loading {
			m.inputModel.InsertText(msg.Content)
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		switch msg := msg.(type) {
		case tea.MouseClickMsg:
			return m.handleMouseClick(msg)
		case tea.MouseWheelMsg:
			return m.handleMouseWheel(msg)
		}
		return m, nil
	case InputSubmitMsg:
		if strings.HasPrefix(msg.Text, "/") {
			return m.handleSlashCommand(msg.Text)
		}
		return m.submitPrompt(msg.Text, msg.Mentions)
	case SteeringSubmitMsg:
		return m.handleSteeringSubmit(msg)
	case FollowUpSubmitMsg:
		return m.handleFollowUpSubmit(msg)
	case initEventMsg:
		return m.handleInitEvent(msg)
	case extensionIntentMsg:
		return m.handleExtensionIntent(msg)
	case providerDebugMsg:
		return m.handleProviderDebug(msg)
	case restartMsg:
		execRestart()
		return m, tea.Quit
	case agentThinkingMsg:
		return m.handleAgentThinking(msg)
	case resetCtrlCCountMsg:
		return m.handleResetCtrlCCount()
	case agentTextMsg:
		return m.handleAgentText(msg)
	case agentToolCallMsg:
		return m.handleAgentToolCall(msg)
	case agentToolResultMsg:
		return m.handleAgentToolResult(msg)
	case agentTraceMsg:
		return m.handleAgentTrace(msg)
	case agentDoneMsg:
		return m.handleAgentDone(msg)
	case loginSSOResultMsg:
		return m.handleLoginSSOResult(msg)
	case commitGeneratedMsg:
		return m.handleCommitGenerated(msg)
	case commitDoneMsg:
		return m.handleCommitDone(msg)
	case modelsFetchedMsg:
		if m.modelPicker != nil {
			m.modelPicker.loading = false
			if msg.err != nil {
				m.modelPicker.err = msg.err
			} else {
				m.modelPicker.all = msg.entries
				m.modelPicker.applyFilter()
				m.modelPicker.selectCurrent()
			}
		}
		return m, nil
	case pingDoneMsg:
		content := msg.output
		if msg.err != nil {
			content += fmt.Sprintf("\n\n✗ Ping failed: %v", msg.err)
		}
		if len(m.chatModel.Messages) > 0 && m.chatModel.Messages[len(m.chatModel.Messages)-1].content == "Pinging model..." {
			m.chatModel.Messages[len(m.chatModel.Messages)-1].content = content
		} else {
			m.chatModel.Messages = append(m.chatModel.Messages, message{role: "assistant", content: content})
		}
		return m, nil
	case extensionLifecycleResultMsg:
		if m.extensionsPanel != nil && m.cfg.ExtensionManager != nil {
			m.extensionsPanel.rows = buildExtensionPanelRows(m.cfg.ExtensionManager.Extensions())
			// Re-clamp cursor.
			if m.extensionsPanel.cursor >= len(m.extensionsPanel.rows) {
				m.extensionsPanel.cursor = len(m.extensionsPanel.rows) - 1
			}
			if m.extensionsPanel.cursor < 0 {
				m.extensionsPanel.cursor = 0
			}
		}
		return m, nil
	}

	if m.running {
		return m, waitForAgent(m.agentCh)
	}
	return m, nil
}

// --- Deferred initialization ---

type initEventMsg struct {
	event InitEvent
	ch    <-chan InitEvent
}

func waitForInitEvent(ch <-chan InitEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return initEventMsg{event: InitEvent{Err: fmt.Errorf("init channel closed unexpectedly")}, ch: ch}
		}
		return initEventMsg{event: ev, ch: ch}
	}
}

func (m *model) handleInitEvent(msg initEventMsg) (tea.Model, tea.Cmd) {
	ev := msg.event
	if ev.Err != nil {
		m.loading = false
		m.loadingItems = nil
		m.initErr = ev.Err
		return m, tea.Quit
	}
	if ev.Item != "" {
		m.loadingItems[ev.Item] = ev.Done
	}
	if ev.Result != nil {
		m.loading = false
		m.loadingItems = nil

		r := ev.Result
		m.cfg.Agent = r.Agent
		m.cfg.SessionID = r.SessionID
		m.cfg.SessionService = r.SessionService
		m.cfg.Logger = r.Logger
		m.cfg.Skills = r.Skills
		m.cfg.SkillDirs = r.SkillDirs
		m.cfg.GenerateCommitMsg = r.GenerateCommitMsg
		m.cfg.TokenTracker = r.TokenTracker
		m.cfg.WrapLLM = r.WrapLLM
		m.cfg.CompactMetrics = r.CompactMetrics
		m.cfg.ExtensionManager = r.ExtensionManager
		m.cfg.ExtensionCommands = r.ExtensionCommands
		m.cfg.RestartCh = r.RestartCh
		m.cfg.Screen = r.Screen
		m.statusModel.GitBranch = r.GitBranch
		m.diffAdded = r.DiffAdded
		m.diffRemoved = r.DiffRemoved
		if m.cfg.SessionService != nil && m.cfg.SessionID != "" {
			_ = m.loadSessionMessages(m.cfg.SessionID)
		}

		m.inputModel.Skills = r.Skills
		m.inputModel.SkillDirs = r.SkillDirs
		m.inputModel.ExtensionManager = r.ExtensionManager
		m.inputModel.ExtensionCommands = r.ExtensionCommands
		m.chatModel.ExtensionManager = r.ExtensionManager
		m.chatModel.ToolDisplay.ExtensionManager = r.ExtensionManager
		m.chatModel.ToolDisplay.RenderMarkdown = m.chatModel.RenderMarkdown
		m.chatModel.ToolDisplay.RenderTimeout = m.chatModel.RenderTimeout
		m.resetExtensionBridge(r.ExtensionManager)

		var cmds []tea.Cmd
		if r.RestartCh != nil {
			cmds = append(cmds, waitForRestart(r.RestartCh))
		}
		if m.extensionIntentCh != nil {
			cmds = append(cmds, waitForExtensionIntent(m.extensionIntentCh))
		}
		return m, tea.Batch(cmds...)
	}

	return m, waitForInitEvent(msg.ch)
}
