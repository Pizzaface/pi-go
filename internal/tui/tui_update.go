package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// spinnerTickMsg drives the Agent active spinner animation.
type spinnerTickMsg time.Time

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		mainWidth := m.layoutMainWidth()
		m.statusModel.Width = mainWidth
		m.chatModel.UpdateRenderer(mainWidth)
		if m.lifecycle != nil {
			m.extensionPanel.SetViews(m.lifecycle.List())
			m.refreshExtensionToast()
		}
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
	case ExtensionEntryMsg:
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "extension",
			content: formatExtensionPayload(msg.Kind, msg.Payload),
			extID:   msg.ExtensionID,
			kind:    msg.Kind,
		})
		return m, nil
	case ExtensionSendCustomMsg:
		if !msg.Message.Display {
			return m, nil
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:       "extension-custom",
			content:    msg.Message.Content,
			extID:      msg.ExtensionID,
			customType: msg.Message.CustomType,
		})
		if msg.Options.TriggerTurn {
			return m, m.startTurnWithText("")
		}
		return m, nil
	case ExtensionSendUserMsg:
		text := joinContent(msg.Message.Content)
		if msg.Options.DeliverAs == "steer" {
			m.abortCurrentTurn()
		}
		if msg.Options.TriggerTurn {
			return m, m.startTurnWithText(text)
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "user",
			content: text,
			extID:   msg.ExtensionID,
		})
		return m, nil
	case ExtensionSetTitleMsg:
		if m.cfg.Agent != nil && m.cfg.SessionID != "" {
			_ = m.cfg.Agent.SetSessionTitle(m.ctx, m.cfg.SessionID, msg.Title)
		}
		return m, nil
	case ExtensionSetLabelMsg:
		if m.cfg.Agent != nil && msg.EntryID != "" {
			_ = m.cfg.Agent.SetSessionTitle(m.ctx, msg.EntryID, msg.Label)
		}
		return m, nil

	case ExtensionNewSessionReq:
		if m.cfg.Agent == nil {
			msg.Done <- ExtensionNewSessionReply{Err: fmt.Errorf("agent not configured")}
			return m, nil
		}
		id, err := m.cfg.Agent.CreateSession(m.ctx)
		if err != nil {
			msg.Done <- ExtensionNewSessionReply{Err: err}
			return m, nil
		}
		m.cfg.SessionID = id
		m.chatModel.Messages = nil
		msg.Done <- ExtensionNewSessionReply{Result: piapi.NewSessionResult{ID: id}}
		return m, nil

	case ExtensionForkReq:
		if m.cfg.SessionService == nil {
			msg.Done <- ExtensionForkReply{Err: fmt.Errorf("session service not configured")}
			return m, nil
		}
		name := "fork-" + time.Now().UTC().Format("20060102T150405.000")
		err := m.cfg.SessionService.CreateBranch(m.cfg.SessionID, agent.AppName, agent.DefaultUserID, name)
		if err != nil {
			msg.Done <- ExtensionForkReply{Err: err}
			return m, nil
		}
		msg.Done <- ExtensionForkReply{Result: piapi.ForkResult{BranchID: name, BranchTitle: name}}
		return m, nil

	case ExtensionNavigateReq:
		if err := m.loadSessionMessages(msg.TargetID); err != nil {
			msg.Done <- ExtensionNavigateReply{Err: piapi.ErrBranchNotFound{ID: msg.TargetID}}
			return m, nil
		}
		msg.Done <- ExtensionNavigateReply{Result: piapi.NavigateResult{BranchID: msg.TargetID}}
		return m, nil

	case ExtensionSwitchReq:
		id := strings.TrimPrefix(msg.SessionPath, "sessions/")
		if err := m.loadSessionMessages(id); err != nil {
			msg.Done <- ExtensionSwitchReply{Err: piapi.ErrSessionNotFound{ID: id}}
			return m, nil
		}
		msg.Done <- ExtensionSwitchReply{Result: piapi.SwitchResult{SessionID: id}}
		return m, nil

	case ExtensionReloadReq:
		msg.Done <- m.reloadExtensions()
		return m, nil

	case SteeringSubmitMsg:
		return m.handleSteeringSubmit(msg)
	case FollowUpSubmitMsg:
		return m.handleFollowUpSubmit(msg)
	case initEventMsg:
		return m.handleInitEvent(msg)
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
	case spinnerTickMsg:
		if m.running {
			m.chatModel.ToolDisplay.SpinnerFrame++
			return m, spinnerTick()
		}
		return m, nil
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
	case extensionEventMsg:
		if m.lifecycle != nil {
			m.extensionPanel.SetViews(m.lifecycle.List())
			m.refreshExtensionToast()
		}
		if m.extensionEventCh != nil {
			return m, waitForExtensionEvent(m.extensionEventCh)
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
		m.cfg.ExtensionCommands = r.ExtensionCommands
		m.cfg.RestartCh = r.RestartCh
		m.cfg.Screen = r.Screen
		m.lifecycle = r.Lifecycle
		m.statusModel.GitBranch = r.GitBranch
		m.diffAdded = r.DiffAdded
		m.diffRemoved = r.DiffRemoved
		if m.cfg.SessionService != nil && m.cfg.SessionID != "" {
			_ = m.loadSessionMessages(m.cfg.SessionID)
		}

		m.inputModel.Skills = r.Skills
		m.inputModel.SkillDirs = r.SkillDirs
		m.inputModel.ExtensionCommands = r.ExtensionCommands
		m.chatModel.ToolDisplay.RenderMarkdown = m.chatModel.RenderMarkdown
		m.chatModel.ToolDisplay.RenderTimeout = m.chatModel.RenderTimeout

		var cmds []tea.Cmd
		if r.RestartCh != nil {
			cmds = append(cmds, waitForRestart(r.RestartCh))
		}
		if m.lifecycle != nil {
			cmds = append(cmds, m.startExtensionEventBridge())
			m.refreshExtensionToast()
		}
		return m, tea.Batch(cmds...)
	}

	return m, waitForInitEvent(msg.ch)
}

// reloadExtensions asks the extension runtime to reload all extensions.
// Runtime wiring is added in Task 16; until then this is a no-op.
func (m *model) reloadExtensions() error {
	// TODO(Task 16): call m.cfg.Runtime.Reload(m.ctx) once Runtime field is wired.
	return nil
}
