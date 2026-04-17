package piapi

import "context"

// ModelRef identifies a provider+model pair.
type ModelRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

// ContextUsage reports current token usage for the active model.
type ContextUsage struct {
	Tokens int `json:"tokens"`
}

// ThinkingLevel is the reasoning intensity dial.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

// UI is the spec #4 interactive surface. Spec #1 uses it as a stub;
// every method returns ErrNotImplemented.
type UI interface{}

// SessionView is the spec #5 read-only session accessor. Spec #1 stubs
// it; every method returns ErrNotImplemented.
type SessionView interface{}

// ModelRegistry is the spec #3 model catalog. Spec #1 stubs.
type ModelRegistry interface{}

// CompactOptions controls ctx.Compact (spec #5).
type CompactOptions struct {
	CustomInstructions string
	OnComplete         func(any)
	OnError            func(error)
}

// NewSessionOptions, ForkResult, NavigateOptions, NavigateResult,
// SwitchResult — spec #5 types.
type NewSessionOptions struct{}

type NewSessionResult struct {
	ID        string
	Cancelled bool
}

type ForkResult struct {
	BranchID    string
	BranchTitle string
	Cancelled   bool
}

type NavigateOptions struct{}

type NavigateResult struct {
	BranchID  string
	Cancelled bool
}

type SwitchResult struct {
	SessionID string
	Cancelled bool
}

// Context is the handler-facing side of the extension API. Every event
// handler and tool Execute call receives one.
type Context interface {
	HasUI() bool
	CWD() string
	Signal() <-chan struct{}
	Model() ModelRef
	ModelRegistry() ModelRegistry
	IsIdle() bool
	Abort()
	HasPendingMessages() bool
	Shutdown()
	GetContextUsage() *ContextUsage
	GetSystemPrompt() string
	Compact(CompactOptions)
	UI() UI
	Session() SessionView
}

// CommandContext extends Context with session-control methods that
// would deadlock if called from event handlers. Only command handlers
// receive this.
type CommandContext interface {
	Context
	WaitForIdle(ctx context.Context) error
	NewSession(NewSessionOptions) (NewSessionResult, error)
	Fork(entryID string) (ForkResult, error)
	NavigateTree(targetID string, opts NavigateOptions) (NavigateResult, error)
	SwitchSession(sessionPath string) (SwitchResult, error)
	Reload(ctx context.Context) error
}
