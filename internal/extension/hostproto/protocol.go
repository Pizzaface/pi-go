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
	MethodHandshake = "pi.extension/handshake"
	MethodShutdown  = "pi.extension/shutdown"
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
