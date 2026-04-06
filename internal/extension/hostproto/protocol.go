package hostproto

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	JSONRPCVersion = "2.0"

	ProtocolVersion = "1.0.0"
	protocolMajor   = 1
)

const (
	MethodHandshake       = "pi.extension/handshake"
	MethodEvent           = "pi.extension/event"
	MethodIntent          = "pi.extension/intent"
	MethodRegisterCommand = "pi.extension/register_command"
	MethodRegisterTool    = "pi.extension/register_tool"
	MethodRender          = "pi.extension/render"
	MethodHealth          = "pi.extension/health"
	MethodShutdown        = "pi.extension/shutdown"
	MethodReload          = "pi.extension/reload"
)

type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type HandshakeRequest struct {
	ProtocolVersion string   `json:"protocol_version"`
	ExtensionID     string   `json:"extension_id"`
	Mode            string   `json:"mode"`
	CapabilityMask  []string `json:"capability_mask,omitempty"`
}

type HandshakeResponse struct {
	ProtocolVersion string `json:"protocol_version"`
	Accepted        bool   `json:"accepted"`
	Message         string `json:"message,omitempty"`
}

func ValidateProtocolCompatibility(version string) error {
	major, err := majorVersion(version)
	if err != nil {
		return err
	}
	if major != protocolMajor {
		return fmt.Errorf("incompatible protocol major version %d (supported: %d)", major, protocolMajor)
	}
	return nil
}

type EventPayload struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

type IntentEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type CommandRegistration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type ToolRegistration struct {
	Name      string `json:"name"`
	Intercept bool   `json:"intercept,omitempty"`
}

type RenderKind string

const (
	RenderKindText     RenderKind = "text"
	RenderKindMarkdown RenderKind = "markdown"
)

type RenderPayload struct {
	Kind    RenderKind `json:"kind"`
	Content string     `json:"content"`
}

func (r RenderPayload) Validate() error {
	switch r.Kind {
	case RenderKindText, RenderKindMarkdown:
		return nil
	default:
		return fmt.Errorf("unsupported render kind %q", r.Kind)
	}
}

type HealthNotification struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ShutdownControl struct {
	Reason string `json:"reason,omitempty"`
}

type ReloadControl struct {
	Reason string `json:"reason,omitempty"`
}

func majorVersion(version string) (int, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0, fmt.Errorf("protocol version is required")
	}
	head := version
	if idx := strings.Index(version, "."); idx >= 0 {
		head = version[:idx]
	}
	major, err := strconv.Atoi(head)
	if err != nil {
		return 0, fmt.Errorf("invalid protocol version %q: %w", version, err)
	}
	return major, nil
}
