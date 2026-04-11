package hostproto

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHandshakeRequest_RoundTrip(t *testing.T) {
	in := HandshakeRequest{
		ProtocolVersion: ProtocolVersion,
		ExtensionID:     "ext.demo",
		Mode:            "hosted_stdio",
		RequestedServices: []ServiceRequest{
			{Service: "ui", Version: 1, Methods: []string{"status"}},
			{Service: "commands", Version: 1},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"mode":"hosted_stdio"`) {
		t.Fatalf("expected mode in handshake payload, got %s", text)
	}
	if !strings.Contains(text, `"requested_services"`) {
		t.Fatalf("expected requested_services in handshake payload, got %s", text)
	}
	var out HandshakeRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.RequestedServices) != 2 {
		t.Fatalf("expected 2 requested services, got %d", len(out.RequestedServices))
	}
	if out.RequestedServices[0].Service != "ui" || out.RequestedServices[0].Methods[0] != "status" {
		t.Errorf("first service mismatch: %+v", out.RequestedServices[0])
	}
}

func TestHandshakeResponse_RoundTrip(t *testing.T) {
	in := HandshakeResponse{
		ProtocolVersion: ProtocolVersion,
		Accepted:        true,
		Message:         "ok",
		GrantedServices: []ServiceGrant{
			{Service: "ui", Version: 1, Methods: []string{"status", "clear_status"}},
		},
		DeniedServices: []ServiceDenial{
			{Service: "tools", Version: 1, Reason: "capability not granted"},
		},
		HostServices: []HostServiceInfo{
			{Service: "ui", Version: 1, Methods: []string{"status", "clear_status"}},
			{Service: "commands", Version: 1, Methods: []string{"register", "unregister"}},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HandshakeResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Accepted {
		t.Error("expected accepted=true")
	}
	if len(out.GrantedServices) != 1 || out.GrantedServices[0].Service != "ui" {
		t.Errorf("granted services mismatch: %+v", out.GrantedServices)
	}
	if len(out.HostServices) != 2 {
		t.Errorf("host services mismatch: %+v", out.HostServices)
	}
}

func TestProtocol_RejectsIncompatibleMajorVersion(t *testing.T) {
	if err := ValidateProtocolCompatibility("3.0.0"); err == nil {
		t.Fatal("expected incompatible major protocol version to fail")
	}
	if err := ValidateProtocolCompatibility("1.5.0"); err == nil {
		t.Fatal("expected v1 to be rejected by v2 host")
	}
	if err := ValidateProtocolCompatibility("2.1.0"); err != nil {
		t.Fatalf("expected v2 minor compatibility, got %v", err)
	}
}

func TestProtocol_VersionIsTwoZeroZero(t *testing.T) {
	if ProtocolVersion != "2.0.0" {
		t.Errorf("ProtocolVersion = %q, want %q", ProtocolVersion, "2.0.0")
	}
}

func TestHostCallParams_RoundTrip(t *testing.T) {
	in := HostCallParams{
		Service: "ui",
		Method:  "status",
		Version: 1,
		Payload: json.RawMessage(`{"text":"hello"}`),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HostCallParams
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Service != "ui" || out.Method != "status" || out.Version != 1 {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	if string(out.Payload) != `{"text":"hello"}` {
		t.Errorf("payload mismatch: %s", string(out.Payload))
	}
}

func TestErrorCodes_AreStable(t *testing.T) {
	if ErrCodeMethodNotFound != -32601 {
		t.Errorf("ErrCodeMethodNotFound = %d, want -32601", ErrCodeMethodNotFound)
	}
	if ErrCodeInvalidParams != -32602 {
		t.Errorf("ErrCodeInvalidParams = %d, want -32602", ErrCodeInvalidParams)
	}
	if ErrCodeServiceError != -32000 {
		t.Errorf("ErrCodeServiceError = %d, want -32000", ErrCodeServiceError)
	}
	if ErrCodeCapabilityDenied != -32001 {
		t.Errorf("ErrCodeCapabilityDenied = %d, want -32001", ErrCodeCapabilityDenied)
	}
	if ErrCodeServiceUnsupported != -32002 {
		t.Errorf("ErrCodeServiceUnsupported = %d, want -32002", ErrCodeServiceUnsupported)
	}
}
