// Package tui implements the interactive terminal UI using Bubble Tea v2.
package tui

import (
	"context"

	"github.com/charmbracelet/glamour"

	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/extension/lifecycle"
	"github.com/dimetron/pi-go/internal/provider"

	tea "charm.land/bubbletea/v2"
)

// model is the Bubble Tea model for the interactive TUI.
type model struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc

	// Per-agent-run cancellation. This must be separate from the root TUI
	// context so canceling one request does not poison future sends.
	runCancel context.CancelFunc

	// UI state.
	width  int
	height int

	// Input sub-model.
	inputModel InputModel

	// Chat sub-model (messages, scroll, rendering).
	chatModel ChatModel

	// Status bar sub-model.
	statusModel StatusModel

	// Theme manager.
	themeManager *ThemeManager

	// Lifecycle service for starting/stopping approved extensions.
	lifecycle lifecycle.Service

	// Extension UI state.
	extensionToast       extensionToastState
	extensionPanel       extensionPanelState
	extensionApproval    *approvalDialogState
	extensionEventCancel func()
	extensionEventCh     <-chan lifecycle.Event

	// Session bridge used by extensions for session-control calls.
	// Set during model construction; nil until wired by BuildRuntime (Task 16).
	bridge *tuiSessionBridge

	// Agent state.
	running bool
	agentCh chan agentMsg // channel for receiving agent events

	// Agent group tracking for accordion nesting. When the LLM invokes an
	// Agent tool, all child tool calls that occur before the Agent result
	// share the same group ID so they can be collapsed together.
	agentGroupStack  []int // stack of active agent group IDs (supports nesting)
	nextAgentGroupID int   // monotonic counter for generating unique group IDs

	// Agent face renderer with mood expressions.
	face *FaceRenderer

	// Debug trace panel toggle (F12).
	debugPanel bool

	// Deferred initialization state.
	loading      bool
	loadingItems map[string]bool // item name -> done?
	initCh       <-chan InitEvent

	// Effort level for provider reasoning/thinking control.
	effortLevel provider.EffortLevel

	// Git diff stats (refreshed after tool completions).
	diffAdded   int
	diffRemoved int

	// Commit flow state.
	commit *commitState

	// Login flow state.
	login *loginState

	// Skill-create pending overwrite confirmation.
	pendingSkillCreate *pendingSkillCreate

	// Branch popup state (shown on status bar click).
	branchPopup *branchPopupState

	// Model picker popup state (shown by /model).
	modelPicker *modelPickerState

	// Login picker popup state (shown by /login with no args).
	loginPicker *loginPickerState

	// Session picker popup state (shown by /resume with no args).
	sessionPicker *sessionPickerState

	// Slash command overlay state (shown for exact `/` + Tab).
	slashOverlay *slashCommandOverlayState

	// Setup alert modal (shown when no model is configured).
	setupAlert bool

	// Message queue for steering and follow-up messages submitted while running.
	messageQueue MessageQueue

	// steeringNotify signals the agent loop that a steering message is available.
	// Buffered channel of size 1; the agent loop selects on this between tool rounds.
	steeringNotify chan struct{}

	// lastViewStartLine is the first visible message line in the last render.
	// Set by View(), used by handleMouseClick() for Agent accordion toggle.
	lastViewStartLine int

	// Quit.
	quitting bool
	initErr  error // fatal init error → propagated from Run()

	// Ctrl+C handling: show warning on first press, quit on second.
	ctrlCCount int
}

func (m *model) openSlashCommandOverlay() {
	rows := buildSlashCommandOverlayRows(m.inputModel.slashCommandInventory())
	if len(rows) == 0 {
		m.slashOverlay = nil
		return
	}
	state := newSlashCommandOverlayState(rows)
	state.Height = len(rows)
	if !state.HasVisibleSelectableRow() {
		m.slashOverlay = nil
		return
	}
	m.branchPopup = nil
	m.modelPicker = nil
	m.inputModel.CyclingIdx = -1
	m.slashOverlay = &state
}

// Run starts the interactive TUI.
func Run(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
		glamour.WithEmoji(),
	)
	if renderer != nil {
		defer func() { _ = renderer.Close() }()
	}

	history := loadHistory()
	if history == nil {
		history = make([]HistoryEntry, 0)
	}

	tm := NewThemeManager()
	if err := tm.LoadThemeDirs(extension.DiscoverResourceDirs(cfg.WorkDir).ThemeDirs...); err != nil {
		return err
	}
	if cfg.ThemeName != "" && cfg.ThemeName != "default" {
		_ = tm.SetTheme(cfg.ThemeName)
	}

	im := NewInputModel(history, cfg.Skills, cfg.SkillDirs, cfg.WorkDir)
	im.ExtensionCommands = cfg.ExtensionCommands

	chat := NewChatModel(renderer)
	chat.ToolDisplay.RenderMarkdown = chat.RenderMarkdown
	chat.ToolDisplay.RenderTimeout = chat.RenderTimeout
	chat.ToolDisplay.CollapsedTools = loadCollapsedTools()

	// Resolve the concrete bridge type so agent_loop can call markBusy/markIdle.
	var tuiBridge *tuiSessionBridge
	if b, ok := cfg.Bridge.(*tuiSessionBridge); ok {
		tuiBridge = b
	}

	m := model{
		cfg:          cfg,
		ctx:          ctx,
		cancel:       cancel,
		inputModel:   im,
		chatModel:    chat,
		statusModel:  StatusModel{},
		themeManager: tm,
		face:         NewFaceRenderer(),
		effortLevel:  cfg.EffortLevel,
		setupAlert:   cfg.NoModelConfigured,
		bridge:       tuiBridge,
	}
	if cfg.DeferredInit != nil {
		m.loading = true
		m.loadingItems = map[string]bool{}
		m.initCh = cfg.DeferredInit
	} else {
		m.statusModel.GitBranch = detectBranch(cfg.WorkDir)
	}

	p := tea.NewProgram(&m, tea.WithContext(ctx))
	if tuiBridge != nil {
		tuiBridge.AttachProgram(p)
	}
	_, err := p.Run()
	if m.initErr != nil {
		return m.initErr
	}
	drainTerminalResponses()
	return err
}

func (m *model) Init() tea.Cmd {
	m.refreshDiffStats()
	var cmds []tea.Cmd
	if m.cfg.RestartCh != nil {
		cmds = append(cmds, waitForRestart(m.cfg.RestartCh))
	}
	if m.cfg.DebugTracer != nil {
		cmds = append(cmds, waitForProviderDebug(m.cfg.DebugTracer.Channel()))
	}
	if m.initCh != nil {
		cmds = append(cmds, waitForInitEvent(m.initCh))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

type providerDebugMsg struct {
	event provider.DebugEvent
	done  bool
}
