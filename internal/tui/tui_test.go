package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/provider"
	pisession "github.com/dimetron/pi-go/internal/session"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestHandleSlashCommandHelp(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
	}

	newM, cmd := m.handleSlashCommand("/help")
	mm := newM.(*model)

	if cmd != nil {
		t.Error("expected nil cmd for /help")
	}
	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if mm.chatModel.Messages[0].role != "assistant" {
		t.Errorf("expected assistant role, got %q", mm.chatModel.Messages[0].role)
	}
}

func TestHandleSlashCommandDebug(t *testing.T) {
	m := &model{
		width:     120,
		chatModel: ChatModel{Messages: make([]message, 0)},
	}

	newM, cmd := m.handleSlashCommand("/debug")
	mm := newM.(*model)

	if cmd != nil {
		t.Error("expected nil cmd for /debug")
	}
	if !mm.debugPanel {
		t.Error("expected /debug to enable debug panel")
	}
	if len(mm.chatModel.Messages) == 0 || !strings.Contains(mm.chatModel.Messages[len(mm.chatModel.Messages)-1].content, "toggled **on**") {
		t.Error("expected confirmation message for /debug on")
	}

	newM, _ = mm.handleSlashCommand("/debug")
	mm = newM.(*model)
	if mm.debugPanel {
		t.Error("expected second /debug to disable debug panel")
	}
}

func TestHandleSlashCommandClear(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/clear"},
		chatModel: ChatModel{
			Messages: []message{
				{role: "user", content: "hello"},
				{role: "assistant", content: "hi"},
			},
		},
	}

	newM, _ := m.handleSlashCommand("/clear")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 0 {
		t.Errorf("expected 0 messages after /clear, got %d", len(mm.chatModel.Messages))
	}
}

func TestHandleSlashCommandModel(t *testing.T) {
	reg := provider.NewRegistry()
	reg.AddBuiltins()
	m := &model{
		inputModel: InputModel{Text: "/model"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{ModelName: "test-model", ProviderRegistry: reg},
	}

	newM, cmd := m.handleSlashCommand("/model")
	mm := newM.(*model)

	// /model now opens the model picker popup instead of printing info.
	if mm.modelPicker == nil {
		t.Fatal("expected model picker to be opened")
	}
	if !mm.modelPicker.loading {
		t.Error("expected picker to be in loading state")
	}
	if mm.modelPicker.current != "test-model" {
		t.Errorf("expected current=%q, got %q", "test-model", mm.modelPicker.current)
	}
	if cmd == nil {
		t.Error("expected a tea.Cmd for async model fetch")
	}
}

func TestHandleSlashCommandModelNoRegistry(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/model"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{ModelName: "test-model"},
	}

	newM, _ := m.handleSlashCommand("/model")
	mm := newM.(*model)

	if mm.modelPicker != nil {
		t.Error("expected no picker when registry is nil")
	}
	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "No provider registry") {
		t.Errorf("unexpected content: %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandModelShowsRoles(t *testing.T) {
	// formatModelInfo still returns role information (used by /settings).
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		cfg: Config{
			ModelName:  "claude-sonnet-4-6",
			ActiveRole: "default",
			Roles: map[string]config.RoleConfig{
				"default": {Model: "claude-sonnet-4-6"},
				"smol":    {Model: "gemini-2.5-flash"},
				"slow":    {Model: "claude-opus-4-6", Provider: "anthropic"},
			},
		},
	}

	content := m.formatModelInfo()
	if !strings.Contains(content, "Configured roles:") {
		t.Errorf("expected roles section, got %q", content)
	}
	if !strings.Contains(content, "smol") {
		t.Errorf("expected smol role listed, got %q", content)
	}
	if !strings.Contains(content, "slow") {
		t.Errorf("expected slow role listed, got %q", content)
	}
	if !strings.Contains(content, "[anthropic]") {
		t.Errorf("expected provider annotation for slow role, got %q", content)
	}
}

func TestHandleSlashCommandModelShowsActiveRole(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		cfg: Config{
			ModelName:  "gemini-2.5-flash",
			ActiveRole: "smol",
			Roles: map[string]config.RoleConfig{
				"default": {Model: "claude-sonnet-4-6"},
				"smol":    {Model: "gemini-2.5-flash"},
			},
		},
	}

	content := m.formatModelInfo()
	if !strings.Contains(content, "(role: smol)") {
		t.Errorf("expected active role indicator, got %q", content)
	}
}

func TestHandleSlashCommandExit(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/exit"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
	}

	newM, cmd := m.handleSlashCommand("/exit")
	mm := newM.(*model)

	if !mm.quitting {
		t.Error("expected quitting to be true after /exit")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd")
	}
}

func TestHandleSlashCommandUnknown(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/unknown"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
	}

	newM, _ := m.handleSlashCommand("/unknown")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if mm.chatModel.Messages[0].content != "Unknown command: `/unknown`. Type `/help` for available commands." {
		t.Errorf("unexpected content: %q", mm.chatModel.Messages[0].content)
	}
}

func TestUpdateWindowSize(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
	}

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := newM.(*model)

	if mm.width != 80 {
		t.Errorf("expected width 80, got %d", mm.width)
	}
	if mm.height != 24 {
		t.Errorf("expected height 24, got %d", mm.height)
	}
}

func TestAgentTextMsg(t *testing.T) {
	m := &model{
		running: true,
		chatModel: ChatModel{
			Streaming: "",
			Messages: []message{
				{role: "user", content: "hello"},
				{role: "assistant", content: ""},
			},
		},
		agentCh: make(chan agentMsg, 1),
	}

	newM, _ := m.Update(agentTextMsg{text: "Hello "})
	mm := newM.(*model)

	if mm.chatModel.Streaming != "Hello " {
		t.Errorf("expected streaming %q, got %q", "Hello ", mm.chatModel.Streaming)
	}
	if mm.chatModel.Messages[1].content != "Hello " {
		t.Errorf("expected message content %q, got %q", "Hello ", mm.chatModel.Messages[1].content)
	}
}

func TestAgentDoneMsg(t *testing.T) {
	m := &model{
		running: true,
		chatModel: ChatModel{
			Streaming: "accumulated text",
			Messages:  make([]message, 0),
		},
		agentCh: make(chan agentMsg, 1),
	}

	newM, _ := m.Update(agentDoneMsg{})
	mm := newM.(*model)

	if mm.running {
		t.Error("expected running to be false after agentDoneMsg")
	}
	if mm.chatModel.Streaming != "" {
		t.Errorf("expected streaming to be cleared, got %q", mm.chatModel.Streaming)
	}
}

func TestAgentToolCallMsg(t *testing.T) {
	m := &model{
		running:   true,
		chatModel: ChatModel{Messages: make([]message, 0)},
		agentCh:   make(chan agentMsg, 1),
	}

	newM, _ := m.Update(agentToolCallMsg{name: "read"})
	mm := newM.(*model)

	if mm.statusModel.ActiveTool != "read" {
		t.Errorf("expected activeTool %q, got %q", "read", mm.statusModel.ActiveTool)
	}
}

func TestAgentToolResultMsg(t *testing.T) {
	m := &model{
		running:     true,
		statusModel: StatusModel{ActiveTool: "read"},
		chatModel:   ChatModel{Messages: make([]message, 0)},
		agentCh:     make(chan agentMsg, 1),
	}

	newM, _ := m.Update(agentToolResultMsg{name: "read"})
	mm := newM.(*model)

	if mm.statusModel.ActiveTool != "" {
		t.Errorf("expected activeTool to be empty, got %q", mm.statusModel.ActiveTool)
	}
}

func TestHistoryNavigation(t *testing.T) {
	m := &model{
		inputModel: InputModel{
			History:    []HistoryEntry{{Text: "first"}, {Text: "second"}, {Text: "third"}},
			HistoryIdx: -1,
			CyclingIdx: -1,
		},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}

	// Press Up → should get "third" (last entry)
	newM, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	mm := newM.(*model)
	if mm.inputModel.Text != "third" {
		t.Errorf("expected %q, got %q", "third", mm.inputModel.Text)
	}

	// Press Up again → should get "second"
	newM, _ = mm.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	mm = newM.(*model)
	if mm.inputModel.Text != "second" {
		t.Errorf("expected %q, got %q", "second", mm.inputModel.Text)
	}

	// Press Down → should get "third"
	newM, _ = mm.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	mm = newM.(*model)
	if mm.inputModel.Text != "third" {
		t.Errorf("expected %q, got %q", "third", mm.inputModel.Text)
	}

	// Press Down again → should clear input
	newM, _ = mm.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	mm = newM.(*model)
	if mm.inputModel.Text != "" {
		t.Errorf("expected empty input, got %q", mm.inputModel.Text)
	}
}

func TestTextInput(t *testing.T) {
	m := &model{
		inputModel: InputModel{CyclingIdx: -1},
		chatModel:  ChatModel{Messages: make([]message, 0)},
	}

	// Type "hi"
	newM, _ := m.handleKey(tea.KeyPressMsg(tea.Key{Text: "h", Code: 'h'}))
	mm := newM.(*model)
	newM, _ = mm.handleKey(tea.KeyPressMsg(tea.Key{Text: "i", Code: 'i'}))
	mm = newM.(*model)

	if mm.inputModel.Text != "hi" {
		t.Errorf("expected %q, got %q", "hi", mm.inputModel.Text)
	}
	if mm.inputModel.CursorPos != 2 {
		t.Errorf("expected cursorPos 2, got %d", mm.inputModel.CursorPos)
	}

	// Backspace
	newM, _ = mm.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	mm = newM.(*model)
	if mm.inputModel.Text != "h" {
		t.Errorf("expected %q after backspace, got %q", "h", mm.inputModel.Text)
	}
}

func TestRenderMessagesEmpty(t *testing.T) {
	m := &model{ //nolint:govet // width/height needed for valid model
		width:     80,
		height:    24,
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	output := m.chatModel.RenderMessages(m.running)
	if output == "" {
		t.Error("expected welcome message for empty conversation")
	}
}

func TestViewLoading(t *testing.T) {
	m := &model{
		width:  0,
		height: 0,
	}
	v := m.View()
	if v.Content != "Loading..." {
		t.Errorf("expected loading message, got %q", v.Content)
	}
}

func TestViewWideDoesNotRenderSidebar(t *testing.T) {
	m := &model{
		cfg:       Config{ProviderName: "openai", ModelName: "gpt-4o"},
		chatModel: ChatModel{Messages: make([]message, 0)},
	}

	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	mm := newM.(*model)
	v := mm.View()

	if !strings.Contains(v.Content, "openai") || !strings.Contains(v.Content, "gpt-4o") {
		t.Fatalf("expected footer metadata in view, got %q", v.Content)
	}
	for _, unexpected := range []string{"\n  Context", "\n  Model", "\n  Git"} {
		if strings.Contains(v.Content, unexpected) {
			t.Errorf("did not expect sidebar section %q in wide view", unexpected)
		}
	}
}

func TestMaxScrollEmpty(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		height:    24,
	}
	if max := m.chatModel.MaxScroll(m.height); max != 0 {
		t.Errorf("expected 0, got %d", max)
	}
}

func TestHandleSlashCommandSession(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/session"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionID: "test-session-123"},
	}

	newM, _ := m.handleSlashCommand("/session")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if mm.chatModel.Messages[0].content != "Session: `test-session-123`" {
		t.Errorf("unexpected content: %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandBranchNoService(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/branch experiment"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: nil},
	}

	newM, _ := m.handleSlashCommand("/branch experiment")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "not available") {
		t.Errorf("expected 'not available' message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandBranchUsage(t *testing.T) {
	svc := setupTestSessionService(t)
	m := &model{
		inputModel: InputModel{Text: "/branch"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: "s1"},
	}

	newM, _ := m.handleSlashCommand("/branch")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "Usage") {
		t.Errorf("expected usage message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandBranchCreate(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	m := &model{
		inputModel: InputModel{Text: "/branch experiment"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: sessionID},
	}

	newM, _ := m.handleSlashCommand("/branch experiment")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "Created and switched to branch") {
		t.Errorf("expected success message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandBranchList(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	m := &model{
		inputModel: InputModel{Text: "/branch list"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: sessionID},
	}

	newM, _ := m.handleSlashCommand("/branch list")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "main") {
		t.Errorf("expected branch list containing 'main', got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandBranchSwitchNoName(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	m := &model{
		inputModel: InputModel{Text: "/branch switch"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: sessionID},
	}

	newM, _ := m.handleSlashCommand("/branch switch")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "Usage") {
		t.Errorf("expected usage message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandForkAliasesBranchCreate(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	m := &model{
		inputModel: InputModel{Text: "/fork experiment"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: sessionID},
	}

	newM, _ := m.handleSlashCommand("/fork experiment")
	mm := newM.(*model)
	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "Created and switched to branch") {
		t.Fatalf("unexpected content: %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandTreeShowsBranches(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	if err := svc.CreateBranch(sessionID, agent.AppName, agent.DefaultUserID, "experiment"); err != nil {
		t.Fatalf("CreateBranch() error: %v", err)
	}
	m := &model{
		inputModel: InputModel{Text: "/tree"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: sessionID},
	}

	newM, _ := m.handleSlashCommand("/tree")
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "main") || !strings.Contains(mm.chatModel.Messages[0].content, "experiment") {
		t.Fatalf("expected tree output, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandResumeListsSessions(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	m := &model{
		inputModel: InputModel{Text: "/resume"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: sessionID},
	}

	newM, _ := m.handleSlashCommand("/resume")
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "Recent sessions") {
		t.Fatalf("expected recent sessions output, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandResumeLoadsMessages(t *testing.T) {
	svc, sessionID := setupTestSessionWithID(t)
	ctx := t.Context()
	getResp, err := svc.Get(ctx, &session.GetRequest{AppName: agent.AppName, UserID: agent.DefaultUserID, SessionID: sessionID})
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	userEvent := &session.Event{Timestamp: time.Now()}
	userEvent.Content = genai.NewContentFromText("remember me", genai.RoleUser)
	if err := svc.AppendEvent(ctx, getResp.Session, userEvent); err != nil {
		t.Fatalf("AppendEvent(user) error: %v", err)
	}
	assistantEvent := &session.Event{Timestamp: time.Now().Add(time.Second)}
	assistantEvent.Content = genai.NewContentFromText("I remember", genai.RoleModel)
	if err := svc.AppendEvent(ctx, getResp.Session, assistantEvent); err != nil {
		t.Fatalf("AppendEvent(model) error: %v", err)
	}

	m := &model{
		inputModel: InputModel{Text: "/resume " + sessionID},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: svc, SessionID: "different-session"},
	}

	newM, _ := m.handleSlashCommand("/resume " + sessionID)
	mm := newM.(*model)
	if mm.cfg.SessionID != sessionID {
		t.Fatalf("SessionID = %q, want %q", mm.cfg.SessionID, sessionID)
	}
	if len(mm.chatModel.Messages) == 0 {
		t.Fatal("expected restored messages")
	}
	found := false
	for _, msg := range mm.chatModel.Messages {
		if msg.content == "remember me" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected restored user message, got %#v", mm.chatModel.Messages)
	}
}

func TestHandleSlashCommandSettings(t *testing.T) {
	m := &model{
		inputModel:   InputModel{Text: "/settings"},
		chatModel:    ChatModel{Messages: make([]message, 0)},
		themeManager: NewThemeManager(),
		cfg:          Config{WorkDir: t.TempDir(), ProviderName: "openai", ModelName: "gpt-5.4", ActiveRole: "default"},
	}

	newM, _ := m.handleSlashCommand("/settings")
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "Global config") {
		t.Fatalf("expected settings output, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandCompactNoService(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/compact"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
		cfg:        Config{SessionService: nil},
	}

	newM, _ := m.handleSlashCommand("/compact")
	mm := newM.(*model)

	if len(mm.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mm.chatModel.Messages))
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "not available") {
		t.Errorf("expected 'not available' message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommandHelpContainsBranch(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/help"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
	}

	newM, _ := m.handleSlashCommand("/help")
	mm := newM.(*model)

	if !strings.Contains(mm.chatModel.Messages[0].content, "/branch") {
		t.Errorf("expected /help to mention /branch, got %q", mm.chatModel.Messages[0].content)
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "/compact") {
		t.Errorf("expected /help to mention /compact, got %q", mm.chatModel.Messages[0].content)
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "/session") {
		t.Errorf("expected /help to mention /session, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHelpText_DoesNotIncludePlanOrRun(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/help"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
	}

	newM, _ := m.handleSlashCommand("/help")
	mm := newM.(*model)

	content := mm.chatModel.Messages[0].content
	if strings.Contains(content, "/plan") {
		t.Errorf("expected /help to omit /plan, got %q", content)
	}
	if strings.Contains(content, "/run") {
		t.Errorf("expected /help to omit /run, got %q", content)
	}
}

func TestCompleteSlashCommand_SlashOnly_NoGhost(t *testing.T) {
	// Just "/" should NOT produce a ghost completion (Tab shows the list instead).
	result := completeSlashCommand("/")
	if result != "" {
		t.Errorf("expected no ghost completion for '/', got %q", result)
	}
}

func TestCompleteSlashCommand_ExactMatch_NoGhost(t *testing.T) {
	result := completeSlashCommand("/help")
	if result != "" {
		t.Errorf("exact match should not produce ghost, got %q", result)
	}
}

func TestMatchingSlashCommands_All(t *testing.T) {
	matches := matchingSlashCommands("/")
	if len(matches) != len(slashCommands) {
		t.Errorf("expected %d matches for '/', got %d", len(slashCommands), len(matches))
	}
}

func TestMatchingSlashCommands_Partial(t *testing.T) {
	matches := matchingSlashCommands("/c")
	// Should match: /clear, /context, /compact
	if len(matches) != 3 {
		t.Errorf("expected 3 matches for '/c', got %d: %v", len(matches), matches)
	}
	for _, m := range matches {
		if !strings.HasPrefix(m, "/c") {
			t.Errorf("unexpected match %q for '/c'", m)
		}
	}
}

func TestMatchingSlashCommands_NoMatch(t *testing.T) {
	matches := matchingSlashCommands("/z")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for '/z', got %d: %v", len(matches), matches)
	}
}

func TestCompleteSlashCommand_IncludesDynamicCommands(t *testing.T) {
	result := completeSlashCommand("/dem", extension.SlashCommand{Name: "demo"})
	if result != "/demo" {
		t.Fatalf("expected dynamic slash completion /demo, got %q", result)
	}
}

func TestMatchingSlashCommands_IncludesDynamicExtensions(t *testing.T) {
	matches := matchingSlashCommands("/dem", extension.SlashCommand{Name: "demo"})
	if len(matches) != 1 || matches[0] != "/demo" {
		t.Fatalf("expected dynamic slash command match /demo, got %v", matches)
	}
}

func TestShowCommandList(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	m.showCommandList()

	if len(m.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.chatModel.Messages))
	}
	content := m.chatModel.Messages[0].content
	if !strings.Contains(content, "Commands:") {
		t.Error("expected 'Commands:' header")
	}
	// Verify all commands are listed.
	for _, cmd := range slashCommands {
		if !strings.Contains(content, cmd) {
			t.Errorf("command list should contain %q", cmd)
		}
	}
	// Verify descriptions are included.
	if !strings.Contains(content, "Show help") {
		t.Error("expected description for /help")
	}
	if strings.Contains(content, "PDD planning session") {
		t.Error("did not expect /plan description")
	}
}

func TestSlashCommandDesc_AllCommandsHaveDescs(t *testing.T) {
	for _, cmd := range slashCommands {
		desc := slashCommandDesc(cmd)
		if desc == "" {
			t.Errorf("command %q has no description", cmd)
		}
	}
}

func TestTabOnSlash_ShowsCommandList(t *testing.T) {
	m := &model{
		inputModel: InputModel{Text: "/"},
		chatModel:  ChatModel{Messages: make([]message, 0)},
	}

	// Simulate Tab press.
	m.showCommandList()

	if len(m.chatModel.Messages) == 0 {
		t.Fatal("expected command list message")
	}
	content := m.chatModel.Messages[0].content
	if strings.Contains(content, "/plan") {
		t.Error("command list should not include /plan")
	}
	if strings.Contains(content, "/run") {
		t.Error("command list should not include /run")
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{52000, "52.0k"},
		{999999, "1000.0k"},
		{1000000, "1.0M"},
		{5200000, "5.2M"},
		{123456789, "123.5M"},
	}
	for _, tt := range tests {
		got := formatTokenCount(tt.n)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestHandleHistoryCommand_Empty(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	m.handleHistoryCommand(nil)
	if len(m.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.chatModel.Messages))
	}
	if !strings.Contains(m.chatModel.Messages[0].content, "No command history") {
		t.Errorf("expected no history message, got %q", m.chatModel.Messages[0].content)
	}
}

func TestHandleHistoryCommand_WithEntries(t *testing.T) {
	m := &model{
		chatModel:  ChatModel{Messages: make([]message, 0)},
		inputModel: InputModel{History: []HistoryEntry{{Text: "/help"}, {Text: "/model"}, {Text: "/ping"}, {Text: "/clear"}}},
	}
	m.handleHistoryCommand(nil)
	if len(m.chatModel.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.chatModel.Messages))
	}
	content := m.chatModel.Messages[0].content
	if !strings.Contains(content, "/help") || !strings.Contains(content, "/ping") {
		t.Errorf("expected history entries, got %q", content)
	}
}

func TestHandleHistoryCommand_WithFilter(t *testing.T) {
	m := &model{
		chatModel:  ChatModel{Messages: make([]message, 0)},
		inputModel: InputModel{History: []HistoryEntry{{Text: "/help"}, {Text: "/model"}, {Text: "/ping"}, {Text: "/clear"}}},
	}
	m.handleHistoryCommand([]string{"p"})
	content := m.chatModel.Messages[0].content
	if !strings.Contains(content, "/ping") || !strings.Contains(content, "/help") {
		t.Errorf("expected filtered entries with 'p', got %q", content)
	}
	if strings.Contains(content, "/model") {
		t.Errorf("should not contain /model, got %q", content)
	}
}

func TestHandleHistoryCommand_FilterNoMatch(t *testing.T) {
	m := &model{
		chatModel:  ChatModel{Messages: make([]message, 0)},
		inputModel: InputModel{History: []HistoryEntry{{Text: "/help"}, {Text: "/model"}}},
	}
	m.handleHistoryCommand([]string{"xyz"})
	if !strings.Contains(m.chatModel.Messages[0].content, "No history matching") {
		t.Errorf("expected no match message, got %q", m.chatModel.Messages[0].content)
	}
}

func TestHandleCommitDone_Success(t *testing.T) {
	m := &model{
		chatModel: ChatModel{
			Messages: []message{
				{role: "assistant", content: "Committing..."},
			},
		},
		commit: &commitState{phase: "committing"},
	}
	newM, _ := m.handleCommitDone(commitDoneMsg{output: "commit abc123"})
	mm := newM.(*model)
	found := false
	for _, msg := range mm.chatModel.Messages {
		if strings.Contains(msg.content, "Committed successfully") {
			found = true
		}
	}
	if !found {
		t.Error("expected success message")
	}
}

func TestHandleCommitDone_Error(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		commit:    &commitState{phase: "committing"},
	}
	newM, _ := m.handleCommitDone(commitDoneMsg{err: fmt.Errorf("git error")})
	mm := newM.(*model)
	found := false
	for _, msg := range mm.chatModel.Messages {
		if strings.Contains(msg.content, "git error") {
			found = true
		}
	}
	if !found {
		t.Error("expected error in messages")
	}
}

func TestRenderStatusBar_WithProvider(t *testing.T) {
	m := &model{
		cfg:         Config{ProviderName: "ollama", ModelName: "qwen3.5:latest"},
		width:       120,
		statusModel: StatusModel{Width: 120},
	}
	bar := m.statusModel.Render(m.statusRenderInput())
	if !strings.Contains(bar, "ollama") {
		t.Errorf("status bar should contain provider, got %q", bar)
	}
	if !strings.Contains(bar, "qwen3.5:latest") {
		t.Errorf("status bar should contain model, got %q", bar)
	}
}

func TestRenderStatusBar_WithoutProvider(t *testing.T) {
	m := &model{
		cfg:         Config{ModelName: "gpt-4o"},
		width:       120,
		statusModel: StatusModel{Width: 120},
	}
	bar := m.statusModel.Render(m.statusRenderInput())
	if !strings.Contains(bar, "gpt-4o") {
		t.Errorf("status bar should contain model, got %q", bar)
	}
}

func TestRenderStatusBar_ContextEstimate(t *testing.T) {
	m := &model{
		cfg: Config{
			ModelName:    "test",
			TokenTracker: &mockTokenTracker{contextUsed: 5000},
		},
		width:       120,
		statusModel: StatusModel{Width: 120},
	}
	bar := m.statusModel.Render(m.statusRenderInput())
	if !strings.Contains(bar, "ctx:") {
		t.Errorf("status bar should show provider-reported context usage, got %q", bar)
	}
}

func TestMaxScroll_EmptyMessages(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: nil},
		height:    40,
	}
	if m.chatModel.MaxScroll(m.height) != 0 {
		t.Error("maxScroll should be 0 for empty messages")
	}
}

func TestMaxScroll_SmallHeight(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{{content: "test"}}},
		height:    0,
	}
	if m.chatModel.MaxScroll(m.height) != 0 {
		t.Error("maxScroll should be 0 for zero height")
	}
}

func TestHandleSlashCommand_Session(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		cfg:       Config{SessionID: "test-session-123"},
	}
	newM, _ := m.handleSlashCommand("/session")
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "test-session-123") {
		t.Errorf("expected session ID, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommand_Unknown(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, _ := m.handleSlashCommand("/nonexistent")
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "Unknown command") {
		t.Errorf("expected unknown command message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommand_Exit(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
	}
	newM, cmd := m.handleSlashCommand("/exit")
	mm := newM.(*model)
	if !mm.quitting {
		t.Error("expected quitting to be true")
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd")
	}
}

func TestHandleSlashCommand_Ping(t *testing.T) {
	mockLLM := &pingMockLLM{name: "test", response: "Pong"}
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		cfg:       Config{LLM: mockLLM},
	}
	newM, cmd := m.handleSlashCommand("/ping")
	mm := newM.(*model)
	if cmd == nil {
		t.Error("expected non-nil cmd for /ping")
	}
	if len(mm.chatModel.Messages) < 1 {
		t.Fatal("expected placeholder message")
	}
}

func TestHandleSkillCreateCommand_NoArgs(t *testing.T) {
	m := &model{chatModel: ChatModel{Messages: make([]message, 0)}}
	newM, cmd := m.handleSkillCreateCommand(nil)
	mm := newM.(*model)
	if cmd != nil {
		t.Error("expected nil cmd")
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "Usage:") {
		t.Errorf("expected usage message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSkillCreateCommand_InvalidName(t *testing.T) {
	m := &model{chatModel: ChatModel{Messages: make([]message, 0)}}
	newM, _ := m.handleSkillCreateCommand([]string{"bad name!"})
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "Invalid skill name") {
		t.Errorf("expected invalid name error, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSkillCreateCancel(t *testing.T) {
	m := &model{
		chatModel:          ChatModel{Messages: make([]message, 0)},
		pendingSkillCreate: &pendingSkillCreate{name: "test"},
	}
	newM, _ := m.handleSkillCreateCancel()
	mm := newM.(*model)
	if mm.pendingSkillCreate != nil {
		t.Error("pending should be cleared")
	}
	if !strings.Contains(mm.chatModel.Messages[0].content, "canceled") {
		t.Errorf("expected canceled message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSkillListCommand_Empty(t *testing.T) {
	m := &model{chatModel: ChatModel{Messages: make([]message, 0)}, cfg: Config{}}
	newM, _ := m.handleSkillListCommand()
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "No skills loaded") {
		t.Errorf("expected no skills message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSkillListCommand_WithSkills(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		cfg: Config{
			Skills: []extension.Skill{
				{Name: "test-skill", Description: "A test skill"},
				{Name: "another", Description: "Another one"},
			},
			SkillDirs: []string{"/tmp/skills"},
		},
	}
	newM, _ := m.handleSkillListCommand()
	mm := newM.(*model)
	content := mm.chatModel.Messages[0].content
	if !strings.Contains(content, "/test-skill") {
		t.Errorf("expected skill name, got %q", content)
	}
	if !strings.Contains(content, "A test skill") {
		t.Errorf("expected skill description, got %q", content)
	}
	if !strings.Contains(content, "/tmp/skills") {
		t.Errorf("expected skill dir, got %q", content)
	}
}

func TestHandleSkillLoadCommand_Empty(t *testing.T) {
	m := &model{chatModel: ChatModel{Messages: make([]message, 0)}, cfg: Config{}}
	newM, _ := m.handleSkillLoadCommand()
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "no skills found") {
		t.Errorf("expected no skills message, got %q", mm.chatModel.Messages[0].content)
	}
}

func TestHandleSlashCommand_Model(t *testing.T) {
	reg := provider.NewRegistry()
	reg.AddBuiltins()
	m := &model{
		chatModel: ChatModel{Messages: make([]message, 0)},
		cfg:       Config{ModelName: "gpt-4o", ActiveRole: "default", Roles: map[string]config.RoleConfig{"default": {Model: "gpt-4o"}}, ProviderRegistry: reg},
	}
	newM, _ := m.handleSlashCommand("/model")
	mm := newM.(*model)
	if mm.modelPicker == nil {
		t.Fatal("expected model picker to be opened")
	}
	if mm.modelPicker.current != "gpt-4o" {
		t.Errorf("expected current=%q, got %q", "gpt-4o", mm.modelPicker.current)
	}
}

func TestHandleSlashCommand_Clear(t *testing.T) {
	m := &model{
		chatModel: ChatModel{Messages: []message{{role: "user", content: "hello"}, {role: "assistant", content: "hi"}}},
	}
	newM, _ := m.handleSlashCommand("/clear")
	mm := newM.(*model)
	if len(mm.chatModel.Messages) != 0 {
		t.Errorf("expected 0 messages after /clear, got %d", len(mm.chatModel.Messages))
	}
}

func TestHandleSlashCommand_History(t *testing.T) {
	m := &model{
		chatModel:  ChatModel{Messages: make([]message, 0)},
		inputModel: InputModel{History: []HistoryEntry{{Text: "/help"}, {Text: "/model"}}},
	}
	newM, _ := m.handleSlashCommand("/history")
	mm := newM.(*model)
	if !strings.Contains(mm.chatModel.Messages[0].content, "/help") {
		t.Errorf("expected history output, got %q", mm.chatModel.Messages[0].content)
	}
}

// Test helpers

func setupTestSessionService(t *testing.T) *pisession.FileService {
	t.Helper()
	dir := t.TempDir()
	svc, err := pisession.NewFileService(dir)
	if err != nil {
		t.Fatalf("creating FileService: %v", err)
	}
	return svc
}

func setupTestSessionWithID(t *testing.T) (*pisession.FileService, string) {
	t.Helper()
	svc := setupTestSessionService(t)

	ctx := t.Context()
	resp, err := svc.Create(ctx, &session.CreateRequest{
		AppName: agent.AppName,
		UserID:  agent.DefaultUserID,
	})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	return svc, resp.Session.ID()
}

func TestFormatThemeList(t *testing.T) {
	themes := []Theme{
		{Name: "dark", DisplayName: "Dark Theme", ThemeType: "dark"},
		{Name: "light", DisplayName: "Light Theme", ThemeType: "light"},
	}
	got := formatThemeList(themes, "dark")

	if !strings.Contains(got, "**Current theme:** `dark`") {
		t.Errorf("missing current theme header")
	}
	if !strings.Contains(got, "☀️") {
		t.Errorf("missing light theme icon")
	}
	if !strings.Contains(got, "🌙") {
		t.Errorf("missing dark theme icon")
	}
	if !strings.Contains(got, "* 🌙 `dark`") {
		t.Errorf("missing current theme marker")
	}
}

func TestFormatThemeError(t *testing.T) {
	got := formatThemeError("darq", []string{"dark", "dracula"})
	if !strings.Contains(got, "darq") {
		t.Errorf("missing theme name in error")
	}
	if !strings.Contains(got, "Did you mean") {
		t.Errorf("missing suggestion")
	}
	if !strings.Contains(got, "dark") {
		t.Errorf("missing match suggestion")
	}

	// No matches
	got2 := formatThemeError("xyz", nil)
	if strings.Contains(got2, "Did you mean") {
		t.Errorf("should not suggest when no matches")
	}
}

func TestMigrateHistoryFormat(t *testing.T) {
	lines := []string{"cmd1", "cmd2", "cmd3"}
	entries := migrateHistoryFormat(lines)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Text != "cmd1" {
		t.Errorf("first entry = %q, want cmd1", entries[0].Text)
	}
	if entries[2].Text != "cmd3" {
		t.Errorf("third entry = %q, want cmd3", entries[2].Text)
	}
}

func TestTruncateHistory(t *testing.T) {
	entries := make([]HistoryEntry, 50)
	for i := range entries {
		entries[i] = HistoryEntry{Text: fmt.Sprintf("cmd%d", i)}
	}

	result := truncateHistory(entries, 10)
	if len(result) != 10 {
		t.Fatalf("expected 10, got %d", len(result))
	}
	if result[0].Text != "cmd40" {
		t.Errorf("first entry = %q, want cmd40", result[0].Text)
	}

	// Under limit — no truncation
	small := truncateHistory(entries[:5], 10)
	if len(small) != 5 {
		t.Errorf("expected 5, got %d", len(small))
	}
}

func TestFormatHistoryOutput(t *testing.T) {
	entries := []HistoryEntry{
		{Text: "git status"},
		{Text: "go test ./...", Mentions: []string{"@file.go"}},
		{Text: "git diff"},
	}

	// No filter
	got := formatHistoryOutput(entries, "")
	if !strings.Contains(got, "**Command history**") {
		t.Errorf("missing header")
	}
	if !strings.Contains(got, "git status") {
		t.Errorf("missing entry")
	}

	// With filter
	got2 := formatHistoryOutput(entries, "git")
	if !strings.Contains(got2, "**History matching `git`**") {
		t.Errorf("missing filter header")
	}
	if strings.Contains(got2, "go test") {
		t.Errorf("should not include non-matching entry")
	}

	// With mentions
	if !strings.Contains(got, "@file.go") {
		t.Errorf("missing mention in output")
	}

	// No matches
	got3 := formatHistoryOutput(entries, "xyz")
	if !strings.Contains(got3, "No history matching") {
		t.Errorf("expected no-match message, got %q", got3)
	}

	// Empty history
	got4 := formatHistoryOutput(nil, "")
	if !strings.Contains(got4, "No command history") {
		t.Errorf("expected empty history message")
	}
}

func TestRenderWelcome(t *testing.T) {
	got := renderWelcome()
	// Check for key content (some words may be split by ANSI style codes).
	checks := []string{
		"Welcome to go-pi",
		"coding agent",
		"help",
		"resume",
		"Tab",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("welcome screen missing %q", want)
		}
	}
}
