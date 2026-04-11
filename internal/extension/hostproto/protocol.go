package hostproto

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	JSONRPCVersion = "2.0"

	ProtocolVersion = "2.0.0"
	protocolMajor   = 2
)

const (
	MethodHandshake = "pi.extension/handshake"
	MethodShutdown  = "pi.extension/shutdown"
	MethodHostCall  = "pi.extension/host_call"
	MethodExtCall   = "pi.extension/ext_call"
)

// JSON-RPC error codes emitted by the host + SDK. These are stable and
// documented as part of the protocol — do not change values.
const (
	ErrCodeMethodNotFound     = -32601 // unknown service or method
	ErrCodeInvalidParams      = -32602 // payload unmarshal / validate failure
	ErrCodeServiceError       = -32000 // handler returned an error
	ErrCodeCapabilityDenied   = -32001 // service used without handshake declaration
	ErrCodeServiceUnsupported = -32002 // host does not implement this service or version
)

// HostCallParams is the envelope for an ext→host RPC dispatched by the
// services registry. Payload is service-defined JSON.
type HostCallParams struct {
	Service string          `json:"service"`
	Method  string          `json:"method"`
	Version int             `json:"version"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ExtCallParams is the mirror envelope: host→ext RPC. Same shape as
// HostCallParams. Used for command invocations, sigil resolves, etc.
type ExtCallParams struct {
	Service string          `json:"service"`
	Method  string          `json:"method"`
	Version int             `json:"version"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

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

type ShutdownControl struct {
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
