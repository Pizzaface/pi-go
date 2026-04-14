package piapi

// Event name constants. Each event has a single stable string name used
// in both the Go API (pi.On("session_start", ...)) and on the wire.
const (
	EventSessionStart = "session_start"
	EventToolExecute  = "tool_execute"
	// Future events (declared here so spec #3 doesn't need renames):
	EventSessionBeforeSwitch  = "session_before_switch"
	EventSessionBeforeFork    = "session_before_fork"
	EventSessionBeforeCompact = "session_before_compact"
	EventSessionCompact       = "session_compact"
	EventSessionBeforeTree    = "session_before_tree"
	EventSessionTree          = "session_tree"
	EventSessionShutdown      = "session_shutdown"
	EventResourcesDiscover    = "resources_discover"
	EventBeforeAgentStart     = "before_agent_start"
	EventAgentStart           = "agent_start"
	EventAgentEnd             = "agent_end"
	EventTurnStart            = "turn_start"
	EventTurnEnd              = "turn_end"
	EventMessageStart         = "message_start"
	EventMessageUpdate        = "message_update"
	EventMessageEnd           = "message_end"
	EventContext              = "context"
	EventBeforeProviderReq    = "before_provider_request"
	EventToolExecutionStart   = "tool_execution_start"
	EventToolExecutionUpdate  = "tool_execution_update"
	EventToolExecutionEnd     = "tool_execution_end"
	EventToolCall             = "tool_call"
	EventToolResult           = "tool_result"
	EventUserBash             = "user_bash"
	EventInput                = "input"
	EventModelSelect          = "model_select"
)

// Event is the interface implemented by all event payload structs.
// Handlers receive the concrete type; subscribers can type-assert.
type Event interface {
	EventName() string
}

// SessionStartEvent fires when a session is started, loaded, or reloaded.
// Spec #1 implements only this event end-to-end.
type SessionStartEvent struct {
	Reason              string `json:"reason"`
	PreviousSessionFile string `json:"previous_session_file,omitempty"`
}

func (SessionStartEvent) EventName() string { return EventSessionStart }

// EventControl carries the return-value shape for events that support
// cancel/block/transform semantics. Each event type documents which
// fields it honors; fields not documented for that event are ignored.
//
// Spec #1 only emits session_start which ignores all control fields;
// the struct is defined now so the wire format is locked.
type EventControl struct {
	Cancel bool   `json:"cancel,omitempty"`
	Block  bool   `json:"block,omitempty"`
	Reason string `json:"reason,omitempty"`
	// TODO(task-5): add Transform *ToolResult when ToolResult is defined
	Action string         `json:"action,omitempty"`
	Extras map[string]any `json:"-"`
}

// EventResult is the return value of an event handler. Nil Control means
// observe-only.
type EventResult struct {
	Control *EventControl `json:"control,omitempty"`
}

// EventHandler is the signature every event subscriber implements.
// TODO(task-6): replace any with Context when Context is defined
type EventHandler func(evt Event, ctx any) (EventResult, error)
