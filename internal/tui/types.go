package tui

import (
	"context"
	"sync"
	"time"

	llmmodel "google.golang.org/adk/model"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/extension/lifecycle"
	"github.com/dimetron/pi-go/internal/logger"
	"github.com/dimetron/pi-go/internal/provider"
	pisession "github.com/dimetron/pi-go/internal/session"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// ExtensionEntryMsg appends an extension-authored entry to the
// transcript. Dispatched by tuiSessionBridge.AppendEntry.
type ExtensionEntryMsg struct {
	ExtensionID string
	Kind        string
	Payload     any
}

// ExtensionSendCustomMsg dispatches a CustomMessage from an extension.
type ExtensionSendCustomMsg struct {
	ExtensionID string
	Message     piapi.CustomMessage
	Options     piapi.SendOptions
}

// ExtensionSendUserMsg dispatches a user-authored message from an extension.
type ExtensionSendUserMsg struct {
	ExtensionID string
	Message     piapi.UserMessage
	Options     piapi.SendOptions
}

// ExtensionSetTitleMsg updates the current session's title.
type ExtensionSetTitleMsg struct {
	Title string
}

// ExtensionSetLabelMsg relabels a branch.
type ExtensionSetLabelMsg struct {
	EntryID string
	Label   string
}

// Config holds configuration for the TUI.
type Config struct {
	Agent            *agent.Agent
	LLM              llmmodel.LLM // The active LLM, used by /ping.
	SessionID        string
	ModelName        string
	ProviderName     string
	ActiveRole       string
	Roles            map[string]config.RoleConfig
	ProviderRegistry *provider.Registry
	DebugTracer      *provider.DebugTracer
	SessionService   *pisession.FileService
	WorkDir          string
	// GenerateCommitMsg is called by /commit to generate a conventional commit message from diffs.
	// If nil, /commit is disabled.
	GenerateCommitMsg func(ctx context.Context, diffs string) (string, error)
	// Logger is the session logger. If nil, logging is disabled.
	Logger *logger.Logger
	// Screen receives screen content updates for the screen tool.
	// If nil, the screen tool won't have access to TUI content.
	Screen *Screen
	// Skills is loaded from skill directories for command completion.
	Skills []extension.Skill
	// SkillDirs are the directories to re-scan for skills on each completion.
	SkillDirs []string
	// ExtensionCommands are narrow slash-command contributions from extensions.
	ExtensionCommands []extension.SlashCommand
	// RestartCh receives a signal when the agent calls the restart tool.
	RestartCh chan struct{}
	// TokenTracker tracks daily token usage and enforces limits. May be nil.
	TokenTracker TokenTracker
	// WrapLLM wraps an LLM with the active token/usage tracker.
	// Used by model-switching to ensure the new LLM is also tracked.
	// May be nil (no wrapping).
	WrapLLM func(llmmodel.LLM) llmmodel.LLM
	// CompactMetrics tracks output compaction statistics. May be nil.
	CompactMetrics CompactStatsProvider
	// ThemeName is the configured theme name from config. Empty or "default" uses tokyo-night.
	ThemeName string
	// EffortLevel is the initial reasoning/thinking effort level.
	EffortLevel provider.EffortLevel

	// Runtime is the extension runtime. Used to fire lifecycle hooks.
	// May be nil when extensions are disabled or not yet wired.
	Runtime *extension.Runtime

	// NoModelConfigured is true when no API key / model is available at startup.
	// The TUI shows a setup alert directing the user to /login.
	NoModelConfigured bool

	// DeferredInit, if non-nil, is a channel of InitEvent messages.
	// When set, the TUI starts immediately in loading state and receives
	// initialization progress updates. The final event carries the fully
	// initialized subsystems in its Result field.
	DeferredInit <-chan InitEvent
}

// InitEvent reports progress from deferred initialization.
type InitEvent struct {
	Item   string      // subsystem name (e.g. "tools", "git", "skills")
	Done   bool        // true when this item finished loading
	Result *InitResult // set on the final event when all init is complete
	Err    error       // fatal initialization error
}

// InitResult holds the fully initialized subsystems delivered by deferred init.
type InitResult struct {
	Agent             *agent.Agent
	SessionID         string
	SessionService    *pisession.FileService
	Logger            *logger.Logger
	Skills            []extension.Skill
	SkillDirs         []string
	ExtensionCommands []extension.SlashCommand
	GenerateCommitMsg func(context.Context, string) (string, error)
	TokenTracker      TokenTracker
	WrapLLM           func(llmmodel.LLM) llmmodel.LLM
	CompactMetrics    CompactStatsProvider
	RestartCh         chan struct{}
	Screen            *Screen
	Lifecycle         lifecycle.Service
	GitBranch         string
	DiffAdded         int
	DiffRemoved       int
}

// CompactStatsProvider provides compaction statistics for TUI display.
type CompactStatsProvider interface {
	FormatStats() string
}

// TokenTracker provides read access to daily token usage and context window
// usage for the status bar, sidebar, and /context command.
type TokenTracker interface {
	Limit() int64
	Remaining() int64     // -1 if unlimited
	PercentUsed() float64 // 0-100+
	TotalUsed() int64     // total tokens consumed today

	// ContextUsed returns the most recent prompt token count from the
	// provider, representing the current context window size. Returns 0
	// before the first provider response.
	ContextUsed() int64

	// ContextLimit returns the max context window size in tokens.
	// Returns 0 if unknown.
	ContextLimit() int64
}

// Screen provides thread-safe access to the current TUI screen content.
// It implements tools.ScreenProvider so the LLM can read what the user sees.
type Screen struct {
	mu      sync.Mutex
	content string
}

// ScreenContent returns the current screen content.
func (s *Screen) ScreenContent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.content
}

func (s *Screen) update(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.content = content
}

// ExtensionLogMsg routes an extension log line to the trace panel.
type ExtensionLogMsg struct {
	ExtensionID string
	Level       string
	Message     string
	Fields      map[string]any
	Ts          time.Time
}

// ExtensionToolStreamMsg delivers a partial ToolResult update from an extension
// for a long-running tool. Dispatched by tuiSessionBridge.EmitToolUpdate.
type ExtensionToolStreamMsg struct {
	ToolCallID string
	Partial    piapi.ToolResult
}

// Session-control request/reply messages dispatched through the tea program.

type ExtensionNewSessionReq struct {
	Done chan ExtensionNewSessionReply
}
type ExtensionNewSessionReply struct {
	Result piapi.NewSessionResult
	Err    error
}
type ExtensionForkReq struct {
	EntryID string
	Done    chan ExtensionForkReply
}
type ExtensionForkReply struct {
	Result piapi.ForkResult
	Err    error
}
type ExtensionNavigateReq struct {
	TargetID string
	Done     chan ExtensionNavigateReply
}
type ExtensionNavigateReply struct {
	Result piapi.NavigateResult
	Err    error
}
type ExtensionSwitchReq struct {
	SessionPath string
	Done        chan ExtensionSwitchReply
}
type ExtensionSwitchReply struct {
	Result piapi.SwitchResult
	Err    error
}
type ExtensionReloadReq struct {
	Done chan error
}
