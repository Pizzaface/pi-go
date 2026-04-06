// Package tui implements the interactive terminal UI using Bubble Tea v2.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
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

// branchPopupState manages the git branch list popup.
type branchPopupState struct {
	branches  []string // list of git branches
	selected  int      // currently selected index
	active    string   // the currently active branch
	height    int      // popup height (number of visible items)
	scrollOff int      // scroll offset when more branches than height
}

// newBranchPopup creates a new branch popup with the list of git branches.
func (m *model) newBranchPopup() {
	branches := listGitBranches(m.cfg.WorkDir)
	if len(branches) == 0 {
		return
	}

	active := m.statusModel.GitBranch
	selected := 0
	for i, b := range branches {
		if b == active {
			selected = i
			break
		}
	}

	popupHeight := len(branches)
	if popupHeight > 8 {
		popupHeight = 8
	}

	m.branchPopup = &branchPopupState{
		branches:  branches,
		selected:  selected,
		active:    active,
		height:    popupHeight,
		scrollOff: 0,
	}
}

// listGitBranches returns a list of all local git branches, with the active one first.
func listGitBranches(workDir string) []string {
	cmd := exec.Command("git", "branch")
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var branches []string
	active := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		// Active branch starts with '*'
		if strings.HasPrefix(line, "* ") {
			active = strings.TrimPrefix(line, "* ")
		} else {
			branches = append(branches, strings.TrimSpace(line))
		}
	}

	// Put active branch first
	if active != "" {
		result := []string{active}
		result = append(result, branches...)
		return result
	}
	return branches
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

	// Load persistent command history from ~/.pi-go/history.jsonl.
	history := loadHistory()
	if history == nil {
		history = make([]HistoryEntry, 0)
	}

	// Initialize theme manager.
	tm := NewThemeManager()
	if err := tm.LoadThemeDirs(extension.DiscoverResourceDirs(cfg.WorkDir).ThemeDirs...); err != nil {
		return err
	}
	if cfg.ThemeName != "" && cfg.ThemeName != "default" {
		_ = tm.SetTheme(cfg.ThemeName) // ignore error, falls back to tokyo-night
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
		m.loadingItems = make(map[string]bool)
		m.initCh = cfg.DeferredInit
	} else {
		m.statusModel.GitBranch = detectBranch(cfg.WorkDir)
	}

	p := tea.NewProgram(&m, tea.WithContext(ctx))
	_, err := p.Run()
	m.closeExtensionBridge()
	drainTerminalResponses()
	if m.initErr != nil {
		return m.initErr
	}
	return err
}

func (m *model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.initCh != nil {
		// Deferred init: start listening for init events.
		// Heavy initialization runs in a background goroutine (started by cli).
		cmds = append(cmds, waitForInitEvent(m.initCh))
	} else {
		// Synchronous init (non-deferred path, used by tests and non-interactive modes).
		m.refreshDiffStats()
	}
	if m.cfg.RestartCh != nil {
		cmds = append(cmds, waitForRestart(m.cfg.RestartCh))
	}
	if m.cfg.DebugTracer != nil {
		cmds = append(cmds, waitForProviderDebug(m.cfg.DebugTracer.Channel()))
	}
	if m.extensionIntentCh != nil {
		cmds = append(cmds, waitForExtensionIntent(m.extensionIntentCh))
	}
	return tea.Batch(cmds...)
}

// waitForRestart returns a Cmd that listens for a restart signal from the agent.
func waitForRestart(ch chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return restartMsg{}
	}
}

type providerDebugMsg struct{ event provider.DebugEvent }

func waitForProviderDebug(ch <-chan provider.DebugEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return providerDebugMsg{event: ev}
	}
}

func (m *model) handleProviderDebug(msg providerDebugMsg) (tea.Model, tea.Cmd) {
	summary, detail := provider.FormatDebugEvent(msg.event)
	kind := msg.event.Kind
	m.chatModel.TraceLog = append(m.chatModel.TraceLog, traceEntry{
		time:    msg.event.Time,
		kind:    kind,
		summary: summary,
		detail:  detail,
	})
	return m, waitForProviderDebug(m.cfg.DebugTracer.Channel())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		mainWidth := m.layoutMainWidth()
		m.statusModel.Width = mainWidth
		m.chatModel.UpdateRenderer(mainWidth)

	case tea.PasteMsg:
		if !m.running {
			m.inputModel.InsertText(msg.Content)
		}

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		// Only handle left click events
		switch msg := msg.(type) {
		case tea.MouseClickMsg:
			return m.handleMouseClick(msg)
		}
		return m, nil

	case InputSubmitMsg:
		if strings.HasPrefix(msg.Text, "/") {
			return m.handleSlashCommand(msg.Text)
		}
		return m.submitPrompt(msg.Text, msg.Mentions)

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
				m.modelPicker.applyFilter() // respects hidden set + any pre-existing filter
				m.modelPicker.selectCurrent()
			}
		}
		return m, nil

	case pingDoneMsg:
		content := msg.output
		if msg.err != nil {
			content += fmt.Sprintf("\n\n✗ Ping failed: %v", msg.err)
		}
		// Replace the "Pinging model..." placeholder.
		if len(m.chatModel.Messages) > 0 && m.chatModel.Messages[len(m.chatModel.Messages)-1].content == "Pinging model..." {
			m.chatModel.Messages[len(m.chatModel.Messages)-1].content = content
		} else {
			m.chatModel.Messages = append(m.chatModel.Messages, message{role: "assistant", content: content})
		}
		return m, nil
	}

	// Keep the agent listener alive for any unhandled message types.
	if m.running {
		return m, waitForAgent(m.agentCh)
	}
	return m, nil
}

// handleMouseClick processes mouse click events.
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

	// Handle setup alert modal — dismiss on Enter or Esc.
	if m.setupAlert {
		switch {
		case key.Code == tea.KeyEnter, key.Code == tea.KeyEsc, key.Code == ' ':
			m.setupAlert = false
			return m, nil
		default:
			return m, nil
		}
	}

	// Handle commit confirmation mode first.
	if !m.running && m.commit != nil && m.commit.phase == "confirming" {
		switch {
		case key.Code == tea.KeyEnter:
			return m.handleCommitConfirm()
		case key.Code == tea.KeyEsc:
			return m.handleCommitCancel()
		case key.Code == 'c' && key.Mod == tea.ModCtrl:
			return m.handleCommitCancel()
		default:
			return m, nil
		}
	}

	// Handle login flow.
	if !m.running && m.login != nil {
		switch {
		case key.Code == tea.KeyEsc:
			return m.handleLoginCancel()
		case key.Code == 'c' && key.Mod == tea.ModCtrl:
			return m.handleLoginCancel()
		case key.Code == tea.KeyEnter && m.login.phase == "waiting":
			apiKey := strings.TrimSpace(m.inputModel.Text)
			if apiKey == "" {
				return m, nil
			}
			m.inputModel.Clear()
			return m.handleLoginSave(apiKey)
		}
		if m.login.phase != "waiting" {
			return m, nil
		}
	}

	// Handle skill-create overwrite confirmation.
	if !m.running && m.pendingSkillCreate != nil {
		switch {
		case key.Code == tea.KeyEnter:
			return m.handleSkillCreateConfirm()
		case key.Code == tea.KeyEsc:
			return m.handleSkillCreateCancel()
		case key.Code == 'c' && key.Mod == tea.ModCtrl:
			return m.handleSkillCreateCancel()
		default:
			return m, nil
		}
	}

	// Handle extension dialog modal.
	if m.extensionDialog != nil {
		switch {
		case key.Code == tea.KeyEsc:
			m.extensionDialog = nil
		case key.Code == tea.KeyEnter:
			m.extensionDialog = nil
		case key.Code == 'c' && key.Mod == tea.ModCtrl:
			m.extensionDialog = nil
		}
		return m, nil
	}

	// Handle branch popup.
	if m.branchPopup != nil {
		switch key.Code {
		case tea.KeyEsc:
			m.branchPopup = nil
			return m, nil
		case tea.KeyEnter:
			return m.handleBranchSelect()
		case tea.KeyUp:
			if m.branchPopup.selected > 0 {
				m.branchPopup.selected--
				if m.branchPopup.selected < m.branchPopup.scrollOff {
					m.branchPopup.scrollOff--
				}
			}
			return m, nil
		case tea.KeyDown:
			if m.branchPopup.selected < len(m.branchPopup.branches)-1 {
				m.branchPopup.selected++
				if m.branchPopup.selected >= m.branchPopup.scrollOff+m.branchPopup.height {
					m.branchPopup.scrollOff++
				}
			}
			return m, nil
		default:
			// Any other key dismisses the popup
			m.branchPopup = nil
			return m, nil
		}
	}

	// Handle model picker popup.
	if m.modelPicker != nil {
		switch {
		case key.Code == tea.KeyEsc:
			m.modelPicker = nil
			return m, nil
		case key.Code == tea.KeyEnter:
			return m.handleModelSelect()
		case key.Code == tea.KeyUp:
			m.modelPicker.moveUp()
			return m, nil
		case key.Code == tea.KeyDown:
			m.modelPicker.moveDown()
			return m, nil
		case key.Code == tea.KeyBackspace:
			if len(m.modelPicker.filter) > 0 {
				m.modelPicker.filter = m.modelPicker.filter[:len(m.modelPicker.filter)-1]
				m.modelPicker.applyFilter()
			}
			return m, nil
		case key.String() == "H":
			// H — toggle hide/unhide the selected model.
			if id := m.modelPicker.toggleHidden(); id != "" {
				saveHiddenModels(m.modelPicker.hidden)
				m.modelPicker.applyFilter()
			}
			return m, nil
		case key.String() == "S":
			// S — toggle show-hidden mode.
			m.modelPicker.showHidden = !m.modelPicker.showHidden
			m.modelPicker.applyFilter()
			return m, nil
		default:
			// Printable runes → type-to-filter.
			if key.Text != "" && key.Mod == 0 {
				m.modelPicker.filter += key.Text
				m.modelPicker.applyFilter()
				return m, nil
			}
			// Non-printable keys dismiss the picker.
			m.modelPicker = nil
			return m, nil
		}
	}

	// Handle login picker popup.
	if m.loginPicker != nil {
		switch {
		case key.Code == tea.KeyEsc:
			m.loginPicker = nil
			return m, nil
		case key.Code == tea.KeyEnter:
			return m.handleLoginPickerSelect()
		case key.Code == tea.KeyUp:
			m.loginPicker.moveUp()
			return m, nil
		case key.Code == tea.KeyDown:
			m.loginPicker.moveDown()
			return m, nil
		default:
			m.loginPicker = nil
			return m, nil
		}
	}

	// Handle slash command overlay.
	if m.slashOverlay != nil {
		switch {
		case key.Code == tea.KeyEsc:
			m.slashOverlay = nil
			return m, nil
		case key.Code == tea.KeyEnter:
			if row, ok := m.slashOverlay.SelectedRow(); ok {
				m.inputModel.Text = row.Name
				m.inputModel.CursorPos = len(m.inputModel.Text)
				m.inputModel.CyclingIdx = -1
				m.slashOverlay = nil
			}
			return m, nil
		case key.Code == tea.KeyUp:
			m.slashOverlay.Move(-1)
			return m, nil
		case key.Code == tea.KeyDown:
			m.slashOverlay.Move(1)
			return m, nil
		case key.Code == tea.KeyTab && key.Mod == tea.ModShift:
			return m, nil
		case key.Code == tea.KeyTab:
			return m, nil
		default:
			cmd := m.inputModel.HandleKey(msg)
			if m.inputModel.Text != "/" {
				m.slashOverlay = nil
			}
			return m, cmd
		}
	}

	if key.Code == tea.KeyTab && m.inputModel.Text == "/" {
		m.openSlashCommandOverlay()
		if m.slashOverlay != nil {
			return m, nil
		}
	}

	// Esc / Ctrl+C: dismiss completion, cancel agent, or quit.
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
		// First press: show warning and reset count after 2 seconds
		m.chatModel.AppendWarning("\nCtrl+C again to quit (or wait 2s)...")
		return m, resetCtrlCCount(m)

	case key.Code == tea.KeyF12:
		m.toggleDebugPanel()
		return m, nil
	}

	if m.running || m.loading {
		return m, nil
	}

	// Ctrl+O: toggle compact/expanded tool output.
	if key.Code == 'o' && key.Mod == tea.ModCtrl {
		m.chatModel.ToolDisplay.CompactTools = !m.chatModel.ToolDisplay.CompactTools
		return m, nil
	}

	// Ctrl+B: toggle branch popup.
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

	// Scroll keys stay in root model.
	switch key.Code {
	case tea.KeyPgUp:
		m.chatModel.ScrollUp(5, m.height)
		return m, nil

	case tea.KeyPgDown:
		m.chatModel.ScrollDown(5)
		return m, nil
	}

	// Delegate all other keys to InputModel.
	cmd := m.inputModel.HandleKey(msg)
	if key.Text == "/" && m.inputModel.Text == "/" && m.slashOverlay == nil {
		m.openSlashCommandOverlay()
	}
	return m, cmd
}

func (m *model) View() tea.View {
	if m.quitting {
		os.Exit(0)
	}

	if m.width == 0 {
		return tea.NewView("Loading...")
	}

	// Layout: a single full-width main pane, with the optional debug panel on the right.
	debugTraceWidth := m.debugTraceWidth()
	mainWidth := m.layoutMainWidth()

	// Render components.
	messagesView := m.chatModel.RenderMessages(m.running)
	statusBar := m.statusModel.Render(m.statusRenderInput())
	inputArea := m.inputModel.View(m.running || m.loading)
	widgetAbove := m.renderExtensionWidget(m.extensionWidgetAbove)
	widgetBelow := m.renderExtensionWidget(m.extensionWidgetBelow)

	// Calculate available height for messages.
	statusLines := strings.Count(statusBar, "\n") + 1
	inputLines := strings.Count(inputArea, "\n") + 1
	widgetAboveLines := 0
	if widgetAbove != "" {
		widgetAboveLines = strings.Count(widgetAbove, "\n") + 1
	}
	widgetBelowLines := 0
	if widgetBelow != "" {
		widgetBelowLines = strings.Count(widgetBelow, "\n") + 1
	}

	// Account for: 1 separator line + 1 newline between sections.
	availableHeight := m.height - statusLines - inputLines - 2
	availableHeight -= widgetAboveLines + widgetBelowLines
	// Reserve a line for the scroll indicator when scrolled up.
	if m.chatModel.Scroll > 0 {
		availableHeight--
	}
	// Reserve space for the branch popup overlay when open.
	if m.branchPopup != nil {
		// popup lines: 1 header + visible branches + 2 border + 1 footer + 2 newlines
		popupLines := m.branchPopup.height + 6
		availableHeight -= popupLines
	}
	// Reserve space for the model picker popup when open.
	if m.modelPicker != nil {
		pickerLines := m.modelPicker.height + 6
		availableHeight -= pickerLines
	}
	// Reserve space for the login picker popup when open.
	if m.loginPicker != nil {
		pickerLines := m.loginPicker.height + 6
		availableHeight -= pickerLines
	}
	if availableHeight < 1 {
		availableHeight = 1
	}

	// Truncate messages to fit viewport.
	msgLines := strings.Split(messagesView, "\n")
	totalLines := len(msgLines)

	startLine := totalLines - availableHeight - m.chatModel.Scroll
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + availableHeight
	if endLine > totalLines {
		endLine = totalLines
	}

	visibleMessages := strings.Join(msgLines[startLine:endLine], "\n")

	// Pad to fill available space.
	visibleLineCount := strings.Count(visibleMessages, "\n") + 1
	for visibleLineCount < availableHeight {
		visibleMessages += "\n"
		visibleLineCount++
	}

	// Constrain chat area to main width.
	chatStyle := lipgloss.NewStyle().Width(mainWidth)
	visibleMessages = chatStyle.Render(visibleMessages)
	if m.slashOverlay != nil {
		maxOverlayHeight := availableHeight - 1
		if maxOverlayHeight < 1 {
			m.slashOverlay = nil
		} else {
			if m.slashOverlay.Height <= 0 || m.slashOverlay.Height > maxOverlayHeight {
				m.slashOverlay.Height = minInt(8, maxOverlayHeight)
			}
			m.slashOverlay.EnsureSelectionVisible()
			if !m.slashOverlay.HasVisibleSelectableRow() {
				m.slashOverlay = nil
			} else {
				visibleMessages = overlaySlashCommandBlock(visibleMessages, m.slashOverlay.render(mainWidth))
			}
		}
	}
	if m.extensionDialog != nil {
		visibleMessages = overlaySlashCommandBlock(visibleMessages, m.renderExtensionDialog(mainWidth))
	}

	var b strings.Builder
	b.WriteString(visibleMessages)
	b.WriteString("\n")

	// Render branch popup if open.
	if m.branchPopup != nil {
		popupView := m.renderBranchPopup()
		b.WriteString(popupView)
		b.WriteString("\n")
	}

	// Render model picker popup if open.
	if m.modelPicker != nil {
		pickerView := m.renderModelPicker()
		b.WriteString(pickerView)
		b.WriteString("\n")
	}

	// Render login picker popup if open.
	if m.loginPicker != nil {
		pickerView := m.renderLoginPicker()
		b.WriteString(pickerView)
		b.WriteString("\n")
	}

	// Scroll indicator — show when the user has scrolled up.
	if m.chatModel.Scroll > 0 {
		scrollDim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		scrollIndicator := scrollDim.Render(fmt.Sprintf(" ↑ scrolled %d lines (PgDn to return)", m.chatModel.Scroll))
		b.WriteString(scrollIndicator)
		b.WriteString("\n")
	}

	// Thin separator line between chat area and status bar.
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	b.WriteString(sepStyle.Render(strings.Repeat("─", mainWidth)))
	b.WriteString("\n")

	b.WriteString(statusBar)
	b.WriteString("\n")
	if widgetAbove != "" {
		b.WriteString(widgetAbove)
		b.WriteString("\n")
	}
	b.WriteString(inputArea)
	if widgetBelow != "" {
		b.WriteString("\n")
		b.WriteString(widgetBelow)
	}

	leftPanel := b.String()

	// Update screen provider so the screen tool can read current content.
	if m.cfg.Screen != nil {
		m.cfg.Screen.update(visibleMessages)
	}

	var final string
	if m.debugPanel && debugTraceWidth > 0 {
		traceContent := m.chatModel.RenderTracePanel(debugTraceWidth-4, m.height)
		traceLines := strings.Split(traceContent, "\n")
		// Show the last N lines that fit the terminal height.
		if len(traceLines) > m.height {
			traceLines = traceLines[len(traceLines)-m.height:]
		}
		for len(traceLines) < m.height {
			traceLines = append(traceLines, "")
		}
		traceContent = strings.Join(traceLines, "\n")

		borderFg := lipgloss.Color("245")
		traceBox := lipgloss.NewStyle().
			Width(debugTraceWidth).
			BorderStyle(lipgloss.Border{Left: "│"}).
			BorderLeft(true).
			BorderForeground(borderFg)

		final = lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, traceBox.Render(traceContent))
	} else {
		final = leftPanel
	}

	// Overlay the setup alert modal when no model is configured.
	if m.setupAlert {
		final = overlaySetupAlert(final, m.width, m.height)
	}

	v := tea.NewView(final)
	v.AltScreen = true
	return v
}

// overlaySetupAlert renders a centered modal box over the screen content.
func overlaySetupAlert(screen string, width, height int) string {
	// Build the alert box content.
	title := "  No Models Configured  "
	body1 := "  Use /login <provider> to set up an API key.  "
	body2 := "                                                "
	body3 := "  Providers: anthropic, openai, gemini,         "
	body4 := "             groq, mistral, xai, openrouter     "
	footer := "          Press Enter to dismiss                "

	lines := []string{title, body2, body1, body2, body3, body4, body2, footer}

	// Find the widest line for the box.
	maxW := 0
	for _, l := range lines {
		if len(l) > maxW {
			maxW = len(l)
		}
	}

	// Pad all lines to maxW.
	for i, l := range lines {
		if len(l) < maxW {
			lines[i] = l + strings.Repeat(" ", maxW-len(l))
		}
	}

	boxBg := lipgloss.Color("235")
	boxFg := lipgloss.Color("255")
	borderFg := lipgloss.Color("33") // blue accent
	titleFg := lipgloss.Color("214") // yellow/orange for title
	dimFg := lipgloss.Color("243")

	boxStyle := lipgloss.NewStyle().
		Background(boxBg).
		Foreground(boxFg).
		Padding(0, 1)

	titleStyle := lipgloss.NewStyle().
		Background(boxBg).
		Foreground(titleFg).
		Bold(true).
		Padding(0, 1)

	footerStyle := lipgloss.NewStyle().
		Background(boxBg).
		Foreground(dimFg).
		Padding(0, 1)

	topBorder := lipgloss.NewStyle().Foreground(borderFg).Render("╭" + strings.Repeat("─", maxW+2) + "╮")
	botBorder := lipgloss.NewStyle().Foreground(borderFg).Render("╰" + strings.Repeat("─", maxW+2) + "╯")
	sideBorderL := lipgloss.NewStyle().Foreground(borderFg).Render("│")
	sideBorderR := lipgloss.NewStyle().Foreground(borderFg).Render("│")

	var boxLines []string
	boxLines = append(boxLines, topBorder)
	for i, l := range lines {
		var styled string
		if i == 0 {
			styled = titleStyle.Render(l)
		} else if i == len(lines)-1 {
			styled = footerStyle.Render(l)
		} else {
			styled = boxStyle.Render(l)
		}
		boxLines = append(boxLines, sideBorderL+styled+sideBorderR)
	}
	boxLines = append(boxLines, botBorder)

	// Now overlay the box centered on the screen.
	screenLines := strings.Split(screen, "\n")
	boxHeight := len(boxLines)
	startY := (height - boxHeight) / 2
	if startY < 0 {
		startY = 0
	}
	startX := (width - maxW - 4) / 2
	if startX < 0 {
		startX = 0
	}

	for i, boxLine := range boxLines {
		row := startY + i
		if row >= 0 && row < len(screenLines) {
			// Overlay the box line onto the screen line.
			orig := screenLines[row]
			screenLines[row] = overlayLine(orig, boxLine, startX)
		}
	}

	return strings.Join(screenLines, "\n")
}

// overlayLine places overlay text on top of a screen line at the given column.
func overlayLine(orig, overlay string, startX int) string {
	// Simple approach: replace characters at the given position.
	// Since we're dealing with ANSI-styled strings, just prepend padding and append.
	padding := ""
	if startX > 0 {
		// Extract the first startX visible characters from the original line.
		runes := []rune(orig)
		if startX < len(runes) {
			padding = string(runes[:startX])
		} else {
			padding = orig + strings.Repeat(" ", startX-len(runes))
		}
	}
	return padding + overlay
}

// drainTerminalResponses discards any pending terminal response sequences
// (e.g. cursor position reports, DECRQM replies) that may arrive after the
// TUI exits. Without this, late responses leak into the shell prompt as garbage
// like "[14;1R[?2026;2$y".
func drainTerminalResponses() {
	f := os.Stdin
	// Switch stdin to non-blocking so we can read without waiting.
	if err := setNonBlock(f); err != nil {
		return
	}
	defer setBlock(f) //nolint:errcheck

	buf := make([]byte, 256)
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, _ := f.Read(buf)
		if n == 0 {
			break
		}
	}
}

func (m *model) eyes() string {
	if m.face != nil {
		return m.face.Eyes()
	}
	return MoodIdle.Eyes()
}

func overlaySlashCommandBlock(base, overlay string) string {
	if overlay == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	if len(overlayLines) > len(baseLines) {
		overlayLines = overlayLines[len(overlayLines)-len(baseLines):]
	}
	start := len(baseLines) - len(overlayLines)
	if start < 0 {
		start = 0
	}
	for i, line := range overlayLines {
		idx := start + i
		if idx >= 0 && idx < len(baseLines) && strings.TrimSpace(line) != "" {
			baseLines[idx] = line
		}
	}
	return strings.Join(baseLines, "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *model) debugTraceWidth() int {
	if !m.debugPanel {
		return 0
	}
	width := m.width / 2
	if width < 40 {
		width = 40
	}
	if width > m.width-30 {
		width = m.width - 30
	}
	if width < 0 {
		width = 0
	}
	return width
}

func (m *model) layoutMainWidth() int {
	mainWidth := m.width
	if m.debugPanel {
		mainWidth -= m.debugTraceWidth()
	}
	if mainWidth < 20 {
		mainWidth = 20
	}
	return mainWidth
}

// refreshDiffStats updates the git diff line counts.
func (m *model) refreshDiffStats() {
	cwd := m.cwd()
	if cwd == "" {
		return
	}
	cmd := exec.Command("git", "diff", "--numstat", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return
	}
	var added, removed int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var a, r int
		if _, err := fmt.Sscanf(line, "%d\t%d\t", &a, &r); err == nil {
			added += a
			removed += r
		}
	}
	added += countUntrackedLines(cwd)
	m.diffAdded = added
	m.diffRemoved = removed
}

// countUntrackedLines counts total lines across untracked files.
func countUntrackedLines(cwd string) int {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	total := 0
	for _, file := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if file == "" {
			continue
		}
		wc := exec.Command("wc", "-l", file)
		wc.Dir = cwd
		wcOut, err := wc.Output()
		if err != nil {
			continue
		}
		var lines int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(wcOut)), "%d", &lines); err == nil {
			total += lines
		}
	}
	return total
}

// statusRenderInput builds the StatusRenderInput from the current model state.
func (m *model) statusRenderInput() StatusRenderInput {
	return StatusRenderInput{
		ProviderName:    m.cfg.ProviderName,
		ModelName:       m.cfg.ModelName,
		Running:         m.running,
		EffortLevel:     m.effortLevel.String(),
		Messages:        m.chatModel.Messages,
		TokenTracker:    m.cfg.TokenTracker,
		DiffAdded:       m.diffAdded,
		DiffRemoved:     m.diffRemoved,
		LoadingItems:    m.loadingItems,
		ExtensionStatus: m.statusModel.ExtensionStatus,
	}
}

// detectBranch returns the current git branch name, or empty string.
func detectBranch(workDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// handleBranchSelect switches to the selected branch.
func (m *model) handleBranchSelect() (tea.Model, tea.Cmd) {
	if m.branchPopup == nil || len(m.branchPopup.branches) == 0 {
		m.branchPopup = nil
		return m, nil
	}

	selectedBranch := m.branchPopup.branches[m.branchPopup.selected]

	// Don't switch if already on this branch
	if selectedBranch == m.branchPopup.active {
		m.branchPopup = nil
		return m, nil
	}

	cwd := m.cwd()

	// Run git checkout in the background
	cmd := exec.Command("git", "checkout", selectedBranch)
	if cwd != "" {
		cmd.Dir = cwd
	}

	err := cmd.Run()
	if err != nil {
		m.chatModel.AppendWarning(fmt.Sprintf("Failed to switch branch: %v", err))
	} else {
		m.statusModel.GitBranch = selectedBranch
		m.refreshDiffStats()
	}

	m.branchPopup = nil
	return m, nil
}

// resetCtrlCCount is a tea.Cmd that resets the Ctrl+C counter after a delay.
func resetCtrlCCount(m *model) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(2 * time.Second)
		return resetCtrlCCountMsg{}
	}
}

// msgResetCtrlCCount resets the Ctrl+C counter.
type resetCtrlCCountMsg struct{}

func (m *model) handleResetCtrlCCount() (tea.Model, tea.Cmd) {
	m.ctrlCCount = 0
	return m, nil
}

// --- Deferred initialization ---

// initEventMsg wraps an InitEvent received from the deferred init channel.
type initEventMsg struct {
	event InitEvent
	ch    <-chan InitEvent
}

// waitForInitEvent returns a Cmd that reads the next event from the init channel.
func waitForInitEvent(ch <-chan InitEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return initEventMsg{event: InitEvent{Err: fmt.Errorf("init channel closed unexpectedly")}, ch: ch}
		}
		return initEventMsg{event: ev, ch: ch}
	}
}

// handleInitEvent processes deferred initialization progress.
func (m *model) handleInitEvent(msg initEventMsg) (tea.Model, tea.Cmd) {
	ev := msg.event

	if ev.Err != nil {
		m.loading = false
		m.loadingItems = nil
		m.initErr = ev.Err
		return m, tea.Quit
	}

	// Track item progress.
	if ev.Item != "" {
		m.loadingItems[ev.Item] = ev.Done
	}

	// Final result: apply all initialized subsystems.
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

		// Update input model with loaded skills and extension commands.
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

	// Keep reading init events.
	return m, waitForInitEvent(msg.ch)
}

// renderBranchPopup renders the branch list popup.
func (m *model) renderBranchPopup() string {
	if m.branchPopup == nil {
		return ""
	}

	popup := m.branchPopup
	bg := lipgloss.Color("236")
	border := lipgloss.Color("240")
	selected := lipgloss.Color("33")
	activeFg := lipgloss.Color("35")
	dimFg := lipgloss.Color("243")

	style := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Border(lipgloss.ThickBorder(), true, true, true, true).
		BorderForeground(border).
		Width(m.width - 10)

	// Calculate popup position (centered horizontally, near the bottom)
	popupWidth := m.width - 10

	var b strings.Builder
	b.WriteString("\n")

	// Header
	header := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Bold(true).
		Width(popupWidth).
		Align(lipgloss.Center).
		Render("Git Branches (Enter to switch, Esc to close)")
	b.WriteString(header)
	b.WriteString("\n")

	// Render visible branches
	branches := popup.branches
	height := popup.height
	scrollOff := popup.scrollOff

	if len(branches) > height {
		branches = branches[scrollOff : scrollOff+height]
	}

	for i, branch := range branches {
		actualIndex := i + scrollOff
		isSelected := actualIndex == popup.selected
		isActive := branch == popup.active

		var line string
		if isActive {
			line = fmt.Sprintf("  ● %s (current)", branch)
		} else {
			line = fmt.Sprintf("    %s", branch)
		}

		if isSelected {
			line = "> " + line[2:] // Replace leading spaces with ">"
		}

		var lineStyle lipgloss.Style
		switch {
		case isSelected:
			lineStyle = lipgloss.NewStyle().Background(selected).Foreground(lipgloss.Color("15"))
		case isActive:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(activeFg)
		default:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(dimFg)
		}

		b.WriteString(lineStyle.Width(popupWidth).Render(line))
		b.WriteString("\n")
	}

	// Show scroll indicator if needed
	if len(popup.branches) > popup.height {
		scrollStyle := lipgloss.NewStyle().Background(bg).Foreground(dimFg)
		b.WriteString(scrollStyle.Render("  ↑↓ scroll"))
	}

	return style.Render(b.String())
}
