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

func TestEventPayload_RoundTrip(t *testing.T) {
	in := EventPayload{
		Type: "session_start",
		Data: map[string]any{"session_id": "abc123", "mode": "interactive"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out EventPayload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type {
		t.Fatalf("expected type %q, got %q", in.Type, out.Type)
	}
	if out.Data["session_id"] != "abc123" {
		t.Fatalf("expected session_id roundtrip, got %+v", out.Data)
	}
}

func TestHealthNotification_Serialization(t *testing.T) {
	note := HealthNotification{
		Status:  "unhealthy",
		Message: "process exited",
		Error:   "exit code 1",
	}
	data, err := json.Marshal(note)
	if err != nil {
		t.Fatal(err)
	}
	var out HealthNotification
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "unhealthy" || out.Error != "exit code 1" {
		t.Fatalf("unexpected health notification roundtrip: %+v", out)
	}
}

func TestRenderPayload_OnlyAllowsSupportedKinds(t *testing.T) {
	if err := (RenderPayload{Kind: RenderKindText, Content: "ok"}).Validate(); err != nil {
		t.Fatalf("expected text kind to be allowed, got %v", err)
	}
	if err := (RenderPayload{Kind: RenderKindMarkdown, Content: "**ok**"}).Validate(); err != nil {
		t.Fatalf("expected markdown kind to be allowed, got %v", err)
	}
	if err := (RenderPayload{Kind: RenderKind("ansi"), Content: "bad"}).Validate(); err == nil {
		t.Fatal("expected unsupported render kind to fail")
	}
}
