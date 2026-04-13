package extension

import "time"

const (
	LifecycleEventStartup      = "startup"
	LifecycleEventSessionStart = "session_start"
)

type EventType string

const (
	EventStartup                = EventType(LifecycleEventStartup)
	EventSessionStart           = EventType(LifecycleEventSessionStart)
	EventCommand      EventType = "command_invoked"
	EventToolStart    EventType = "tool_start"
	EventToolResult   EventType = "tool_result"
	EventToolError    EventType = "tool_error"
	EventReload       EventType = "reload"
	EventShutdown     EventType = "shutdown"
)

// Event is a typed extension event payload delivered by the manager.
type Event struct {
	Type      EventType
	Timestamp time.Time
	Data      map[string]any
}
