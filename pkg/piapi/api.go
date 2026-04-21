package piapi

import "context"

// CustomMessage is the payload for API.SendMessage.
type CustomMessage struct {
	CustomType string
	Content    string
	Display    bool
	Details    map[string]any
}

// UserMessage is the payload for API.SendUserMessage.
type UserMessage struct {
	Content []ContentPart
}

// SendOptions controls message delivery.
type SendOptions struct {
	DeliverAs   string // "steer" | "followUp" | "nextTurn"
	TriggerTurn bool
}

// CommandDescriptor — spec #2.
type CommandDescriptor struct {
	Description            string
	Handler                func(args string, ctx CommandContext) error
	GetArgumentCompletions func(prefix string) []AutocompleteItem
}

// AutocompleteItem — spec #2.
type AutocompleteItem struct {
	Value string
	Label string
}

// ShortcutDescriptor — spec #6.
type ShortcutDescriptor struct {
	Description string
	Handler     func(ctx CommandContext) error
}

// FlagDescriptor — spec #6.
type FlagDescriptor struct {
	Description string
	Type        string
	Default     any
}

// ProviderDescriptor — spec #6.
type ProviderDescriptor struct {
	BaseURL string
	APIKey  string
	API     string
	// Remaining fields land in spec #6.
}

// RendererDescriptor — spec #6.
type RendererDescriptor struct {
	Kind    string // "text" | "markdown"
	Handler any    // placeholder; shape finalized in spec #6
}

// CommandInfo is the read shape returned by API.GetCommands (spec #2).
type CommandInfo struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Source      string     `json:"source"`
	SourceInfo  SourceInfo `json:"source_info"`
}

// ExecOptions controls API.Exec.
type ExecOptions struct {
	Timeout int // milliseconds; 0 means no timeout
}

// ExecResult is the output of API.Exec.
type ExecResult struct {
	Stdout string
	Stderr string
	Code   int
	Killed bool
}

// EventBus is the inter-extension pubsub (spec #3 stub).
type EventBus interface {
	On(event string, handler func(any)) error
	Emit(event string, payload any) error
}

// API is the surface every extension uses. Compiled-in extensions
// receive a direct implementation; hosted extensions receive an
// RPC-backed implementation.
//
// Methods tagged "spec #N" return ErrNotImplemented until that spec
// lands. Callers should check with errors.Is(err, ErrNotImplementedSentinel).
type API interface {
	// Identity — set during Register, read-only thereafter.
	Name() string
	Version() string

	// Registrations.
	RegisterTool(ToolDescriptor) error                        // spec #1
	RegisterCommand(string, CommandDescriptor) error          // spec #2
	RegisterShortcut(string, ShortcutDescriptor) error        // spec #6
	RegisterFlag(string, FlagDescriptor) error                // spec #6
	RegisterProvider(string, ProviderDescriptor) error        // spec #6
	UnregisterProvider(string) error                          // spec #6
	RegisterMessageRenderer(string, RendererDescriptor) error // spec #6

	// Tool teardown + explicit readiness (spec: 2026-04-20-hosted-tool-invocation).
	UnregisterTool(name string) error
	Ready() error

	// Event subscription (spec #1 supports session_start; others in spec #3).
	On(eventName string, handler EventHandler) error

	// Inter-extension bus (spec #3).
	Events() EventBus

	// Messaging & state (spec #5).
	SendMessage(CustomMessage, SendOptions) error
	SendUserMessage(UserMessage, SendOptions) error
	AppendEntry(kind string, payload any) error
	SetSessionName(string) error
	GetSessionName() string
	SetLabel(entryID, label string) error

	// Tool & model management (spec #3).
	GetActiveTools() []string
	GetAllTools() []ToolInfo
	SetActiveTools([]string) error
	SetModel(ModelRef) (bool, error)
	GetThinkingLevel() ThinkingLevel
	SetThinkingLevel(ThinkingLevel) error

	// Utilities.
	Exec(ctx context.Context, cmd string, args []string, opts ExecOptions) (ExecResult, error) // spec #1
	GetCommands() []CommandInfo                                                                // spec #2
	GetFlag(name string) any                                                                   // spec #6

	// v2.2 services: state / commands / ui / sigils / session-metadata.
	APIv22
}

// Register is the entrypoint signature every compiled-in and hosted-Go
// extension implements.
type Register func(pi API) error
