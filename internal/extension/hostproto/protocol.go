package hostproto

import "encoding/json"

// ProtocolVersion is the wire contract between go-pi and extensions.
const ProtocolVersion = "2.2"

// Error codes.
const (
	ErrCodeServiceUnsupported = -32001
	ErrCodeMethodNotFound     = -32002
	ErrCodeCapabilityDenied   = -32003
	ErrCodeEventNotSupported  = -32004
	ErrCodeHandlerTimeout     = -32005
	ErrCodeHandshakeFailed    = -32006
	ErrCodeToolNotOwned       = -32097
	ErrCodeToolNotFound       = -32098
	ErrCodeToolNameCollision  = -32099
)

// Error codes (v2.2).
const (
	ErrCodeDialogCancelled      = -32094
	ErrCodeSigilPrefixCollision = -32095
	ErrCodeCommandNameCollision = -32096
)

// Method names.
const (
	MethodHandshake      = "pi.extension/handshake"
	MethodHostCall       = "pi.extension/host_call"
	MethodSubscribeEvent = "pi.extension/subscribe_event"
	MethodExtensionEvent = "pi.extension/extension_event"
	MethodToolUpdate     = "pi.extension/tool_update"
	MethodLog            = "pi.extension/log"
	MethodShutdown       = "pi.extension/shutdown"
)

// HandshakeRequest is the payload the extension sends first.
type HandshakeRequest struct {
	ProtocolVersion   string             `json:"protocol_version"`
	ExtensionID       string             `json:"extension_id"`
	ExtensionVersion  string             `json:"extension_version"`
	RequestedServices []RequestedService `json:"requested_services"`
}

// RequestedService is a single service/method set the extension wants.
type RequestedService struct {
	Service string   `json:"service"`
	Version int      `json:"version"`
	Methods []string `json:"methods"`
}

// HandshakeResponse is the host's reply.
type HandshakeResponse struct {
	ProtocolVersion    string              `json:"protocol_version"`
	GrantedServices    []GrantedService    `json:"granted_services"`
	HostServices       []HostServiceInfo   `json:"host_services"`
	DispatchableEvents []DispatchableEvent `json:"dispatchable_events"`
}

// GrantedService is a post-filter view of a requested service.
type GrantedService struct {
	Service      string   `json:"service"`
	Version      int      `json:"version"`
	Methods      []string `json:"methods"`
	DeniedReason string   `json:"denied_reason,omitempty"`
}

// HostServiceInfo describes a service offered by the host.
type HostServiceInfo struct {
	Service string   `json:"service"`
	Version int      `json:"version"`
	Methods []string `json:"methods"`
}

// DispatchableEvent is an event the host is willing to dispatch.
type DispatchableEvent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// HostCallParams is the payload for host_call.
type HostCallParams struct {
	Service string          `json:"service"`
	Version int             `json:"version"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

// SubscribeEventParams is the payload for subscribe_event.
type SubscribeEventParams struct {
	Events []EventSubscription `json:"events"`
}

// EventSubscription identifies one event the extension wants dispatched.
type EventSubscription struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// ExtensionEventParams is the payload for extension_event (host → ext).
type ExtensionEventParams struct {
	Event      string          `json:"event"`
	Version    int             `json:"version"`
	Payload    json.RawMessage `json:"payload"`
	Context    json.RawMessage `json:"context,omitempty"`
	DeadlineMs int             `json:"deadline_ms,omitempty"`
}

// Service names (spec #5+).
const (
	ServiceSession        = "session"
	ServiceSessionControl = "session_control"
	ServiceToolStream     = "tool_stream"
	ServiceLog            = "log"
	ServiceTools          = "tools"
	ServiceEvents         = "events"
	ServiceHooks          = "hooks"
	ServiceExt            = "ext"
)

// Service names (v2.2).
const (
	ServiceState    = "state"
	ServiceCommands = "commands"
	ServiceUI       = "ui"
	ServiceSigils   = "sigils"
)

// Method names within services (spec #5).
const (
	MethodSessionAppendEntry       = "append_entry"
	MethodSessionSendCustomMessage = "send_custom_message"
	MethodSessionSendUserMessage   = "send_user_message"
	MethodSessionSetTitle          = "set_title"
	MethodSessionGetTitle          = "get_title"
	MethodSessionSetEntryLabel     = "set_entry_label"

	MethodSessionControlWaitIdle = "wait_idle"
	MethodSessionControlNew      = "new"
	MethodSessionControlFork     = "fork"
	MethodSessionControlNavigate = "navigate"
	MethodSessionControlSwitch   = "switch"
	MethodSessionControlReload   = "reload"

	MethodToolStreamUpdate = "update"
	MethodLogAppend        = "append"

	MethodToolsRegister   = "register"
	MethodToolsUnregister = "unregister"
	MethodExtReady        = "ready"
)

// Method names for v2.2 services.
const (
	// state
	MethodStateGet    = "get"
	MethodStateSet    = "set"
	MethodStatePatch  = "patch"
	MethodStateDelete = "delete"

	// commands
	MethodCommandsRegister   = "register"
	MethodCommandsUnregister = "unregister"
	MethodCommandsList       = "list"

	// ui
	MethodUIStatus      = "status"
	MethodUIClearStatus = "clear_status"
	MethodUIWidget      = "widget"
	MethodUIClearWidget = "clear_widget"
	MethodUINotify      = "notify"
	MethodUIDialog      = "dialog"

	// sigils
	MethodSigilsRegister   = "register"
	MethodSigilsUnregister = "unregister"
	MethodSigilsList       = "list"

	// session metadata (added to existing session service)
	MethodSessionGetMetadata = "get_metadata"
	MethodSessionSetName     = "set_name"
	MethodSessionSetTags     = "set_tags"
)

// Payload shapes for the new services.

type SessionAppendEntryParams struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type SessionSendCustomMessageParams struct {
	CustomType  string         `json:"custom_type"`
	Content     string         `json:"content"`
	Display     bool           `json:"display"`
	Details     map[string]any `json:"details,omitempty"`
	DeliverAs   string         `json:"deliver_as,omitempty"`
	TriggerTurn bool           `json:"trigger_turn,omitempty"`
}

type ContentPartProto struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type SessionSendUserMessageParams struct {
	Content     []ContentPartProto `json:"content"`
	DeliverAs   string             `json:"deliver_as,omitempty"`
	TriggerTurn bool               `json:"trigger_turn,omitempty"`
}

type SessionSetTitleParams struct {
	Title string `json:"title"`
}

type SessionGetTitleResult struct {
	Title string `json:"title"`
}

type SessionSetEntryLabelParams struct {
	EntryID string `json:"entry_id"`
	Label   string `json:"label"`
}

type SessionControlForkParams struct {
	EntryID string `json:"entry_id"`
}

type SessionControlForkResult struct {
	BranchID    string `json:"branch_id"`
	BranchTitle string `json:"branch_title"`
	Cancelled   bool   `json:"cancelled"`
}

type SessionControlNewResult struct {
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled"`
}

type SessionControlNavigateParams struct {
	TargetID string `json:"target_id"`
}

type SessionControlNavigateResult struct {
	BranchID  string `json:"branch_id"`
	Cancelled bool   `json:"cancelled"`
}

type SessionControlSwitchParams struct {
	SessionPath string `json:"session_path"`
}

type SessionControlSwitchResult struct {
	SessionID string `json:"session_id"`
	Cancelled bool   `json:"cancelled"`
}

type ToolStreamUpdateParams struct {
	ToolCallID string          `json:"tool_call_id"`
	Partial    json.RawMessage `json:"partial"`
}

type LogParams struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
	Ts      string         `json:"ts,omitempty"`
}

// Payload structs for v2.2 services.

// state

type StateGetResult struct {
	Value  json.RawMessage `json:"value,omitempty"`
	Exists bool            `json:"exists"`
}

type StateSetParams struct {
	Value json.RawMessage `json:"value"`
}

type StatePatchParams struct {
	Patch json.RawMessage `json:"patch"`
}

// commands

type CommandsRegisterParams struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	ArgHint     string `json:"arg_hint,omitempty"`
}

type CommandsUnregisterParams struct {
	Name string `json:"name"`
}

type CommandEntry struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	ArgHint     string `json:"arg_hint,omitempty"`
	Owner       string `json:"owner"`
	Source      string `json:"source"` // "manifest" | "runtime"
}

type CommandsListResult struct {
	Commands []CommandEntry `json:"commands"`
}

type CommandsInvokeEvent struct {
	Name    string `json:"name"`
	Args    string `json:"args"`
	EntryID string `json:"entry_id,omitempty"`
}

type CommandsInvokeResult struct {
	Handled bool   `json:"handled"`
	Message string `json:"message,omitempty"`
	Silent  bool   `json:"silent,omitempty"`
}

// ui

type UIStatusParams struct {
	Text  string `json:"text"`
	Style string `json:"style,omitempty"`
}

type Position struct {
	Mode    string `json:"mode,omitempty"`   // static|relative|absolute|sticky|fixed
	Anchor  string `json:"anchor,omitempty"` // top|bottom|left|right
	OffsetX int    `json:"offset_x,omitempty"`
	OffsetY int    `json:"offset_y,omitempty"`
	Z       int    `json:"z,omitempty"`
}

type UIWidgetParams struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Lines    []string `json:"lines"`
	Style    string   `json:"style,omitempty"`
	Position Position `json:"position"`
}

type UIClearWidgetParams struct {
	ID string `json:"id"`
}

type UINotifyParams struct {
	Level     string `json:"level"`
	Text      string `json:"text"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type UIDialogField struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // text|password|choice|bool
	Label   string   `json:"label,omitempty"`
	Default string   `json:"default,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

type UIDialogButton struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style,omitempty"`
}

type UIDialogParams struct {
	Title   string           `json:"title"`
	Fields  []UIDialogField  `json:"fields,omitempty"`
	Buttons []UIDialogButton `json:"buttons"`
}

type UIDialogResult struct {
	DialogID string `json:"dialog_id"`
}

type UIDialogResolvedEvent struct {
	DialogID  string         `json:"dialog_id"`
	Values    map[string]any `json:"values,omitempty"`
	Cancelled bool           `json:"cancelled"`
	ButtonID  string         `json:"button_id,omitempty"`
}

// sigils

type SigilsRegisterParams struct {
	Prefixes []string `json:"prefixes"`
}

type SigilsUnregisterParams struct {
	Prefixes []string `json:"prefixes"`
}

type SigilPrefixEntry struct {
	Prefix string `json:"prefix"`
	Owner  string `json:"owner"`
}

type SigilsListResult struct {
	Prefixes []SigilPrefixEntry `json:"prefixes"`
}

type SigilResolveEvent struct {
	Prefix  string `json:"prefix"`
	ID      string `json:"id"`
	Context string `json:"context,omitempty"`
}

type SigilResolveResult struct {
	Display string         `json:"display"`
	Style   string         `json:"style,omitempty"`
	Hover   string         `json:"hover,omitempty"`
	Actions []string       `json:"actions,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type SigilActionEvent struct {
	Prefix string `json:"prefix"`
	ID     string `json:"id"`
	Action string `json:"action"`
}

type SigilActionResult struct {
	Handled bool `json:"handled"`
}

// session metadata

type SessionGetMetadataResult struct {
	Name      string   `json:"name,omitempty"`
	Title     string   `json:"title,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"` // RFC3339
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type SessionSetNameParams struct {
	Name string `json:"name"`
}

type SessionSetTagsParams struct {
	Tags []string `json:"tags"`
}
