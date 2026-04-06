// Package tui implements the interactive terminal UI using Bubble Tea v2.
package tui

import (
	"context"

	"github.com/charmbracelet/glamour"

	"github.com/dimetron/pi-go/internal/extension"
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

	// Agent state.
	running bool
	agentCh chan agentMsg // channel for receiving agent events

	// Agent face renderer with mood expressions.
	face *FaceRenderer

	// Debug trace panel toggle (F12).
	debugPanel bool

	// Deferred initialization state.
	loading      bool
	loadingItems map[string]bool // item name -> done?
	initCh       <-chan InitEvent

	// Extension bridge state.
	extensionBridge      *extensionBridge
	extensionIntentCh    <-chan extension.UIIntentEnvelope
	extensionWidgetAbove *extensionWidgetState
	extensionWidgetBelow *extensionWidgetState
	extensionDialog      *extensionDialogState

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

	// Slash command overlay state (shown for exact `/` + Tab).
	slashOverlay *slashCommandOverlayState

	// Setup alert modal (shown when no model is configured).
	setupAlert bool

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
	state.Height = 8
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
	im.ExtensionManager = cfg.ExtensionManager

	chat := NewChatModel(renderer)
	chat.ExtensionManager = cfg.ExtensionManager
	chat.ToolDisplay.ExtensionManager = cfg.ExtensionManager
	chat.ToolDisplay.RenderMarkdown = chat.RenderMarkdown
	chat.ToolDisplay.RenderTimeout = chat.RenderTimeout

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
	}
	m.resetExtensionBridge(cfg.ExtensionManager)
	if cfg.DeferredInit != nil {
		m.loading = true
		m.loadingItems = map[string]bool{}
		m.initCh = cfg.DeferredInit
	} else {
		m.statusModel.GitBranch = detectBranch(cfg.WorkDir)
	}

	p := tea.NewProgram(&m, tea.WithContext(ctx))
	_, err := p.Run()
	m.closeExtensionBridge()
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
	if m.extensionIntentCh != nil {
		cmds = append(cmds, waitForExtensionIntent(m.extensionIntentCh))
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
