package hostproto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHandshake_IncludesModeAndCapabilityMask(t *testing.T) {
	payload := HandshakeRequest{
		ProtocolVersion: ProtocolVersion,
		ExtensionID:     "ext.demo",
		Mode:            "interactive",
		CapabilityMask:  []string{"commands.register", "tools.register"},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"mode":"interactive"`) {
		t.Fatalf("expected mode in handshake payload, got %s", text)
	}
	if !strings.Contains(text, `"capability_mask":["commands.register","tools.register"]`) {
		t.Fatalf("expected capability_mask in handshake payload, got %s", text)
	}
}

func TestProtocol_RejectsIncompatibleMajorVersion(t *testing.T) {
	if err := ValidateProtocolCompatibility("2.1.0"); err == nil {
		t.Fatal("expected incompatible major protocol version to fail")
	}
	if err := ValidateProtocolCompatibility("1.5.0"); err != nil {
		t.Fatalf("expected v1 compatibility, got %v", err)
	}
}
