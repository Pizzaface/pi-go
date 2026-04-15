package hostproto

import "encoding/json"

// ProtocolVersion is the wire contract between pi-go and extensions.
const ProtocolVersion = "2.1"

// Error codes.
const (
	ErrCodeServiceUnsupported = -32001
	ErrCodeMethodNotFound     = -32002
	ErrCodeCapabilityDenied   = -32003
	ErrCodeEventNotSupported  = -32004
	ErrCodeHandlerTimeout     = -32005
	ErrCodeHandshakeFailed    = -32006
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
