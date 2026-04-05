package tui

import (
	"context"
	"sync"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/logger"
	"github.com/dimetron/pi-go/internal/provider"
	pisession "github.com/dimetron/pi-go/internal/session"
	llmmodel "google.golang.org/adk/model"
)

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
	// CompactMetrics tracks output compaction statistics. May be nil.
	CompactMetrics CompactStatsProvider
	// ThemeName is the configured theme name from config. Empty or "default" uses tokyo-night.
	ThemeName string

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
	CompactMetrics    CompactStatsProvider
	RestartCh         chan struct{}
	Screen            *Screen
	GitBranch         string
	DiffAdded         int
	DiffRemoved       int
}

// CompactStatsProvider provides compaction statistics for TUI display.
type CompactStatsProvider interface {
	FormatStats() string
}

// TokenTracker provides read access to daily token usage for the status bar.
type TokenTracker interface {
	Limit() int64
	Remaining() int64     // -1 if unlimited
	PercentUsed() float64 // 0-100+
	TotalUsed() int64     // total tokens consumed today
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
