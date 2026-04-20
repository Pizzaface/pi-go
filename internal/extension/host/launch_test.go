package host

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/hostproto"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestBuildHandshakeResponse_GrantsRequestedMethodsForCompiledIn(t *testing.T) {
	gate, err := NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(gate)
	reg := &Registration{
		ID:       "compiled-x",
		Trust:    TrustCompiledIn,
		Metadata: piapi.Metadata{Name: "compiled-x", Version: "0.1"},
	}
	if err := manager.Register(reg); err != nil {
		t.Fatal(err)
	}

	req := hostproto.HandshakeRequest{
		ProtocolVersion: hostproto.ProtocolVersion,
		ExtensionID:     "compiled-x",
		RequestedServices: []hostproto.RequestedService{
			{Service: "tools", Version: 1, Methods: []string{"register"}},
			{Service: "events", Version: 1, Methods: []string{"session_start"}},
		},
	}
	params, _ := json.Marshal(req)
	out, err := BuildHandshakeResponse(reg, manager, params)
	if err != nil {
		t.Fatalf("expected success; got %v", err)
	}
	resp, ok := out.(hostproto.HandshakeResponse)
	if !ok {
		t.Fatalf("expected HandshakeResponse; got %T", out)
	}
	if resp.ProtocolVersion != hostproto.ProtocolVersion {
		t.Fatalf("expected protocol %s; got %s", hostproto.ProtocolVersion, resp.ProtocolVersion)
	}
	if len(resp.GrantedServices) != 2 {
		t.Fatalf("expected 2 granted services; got %d", len(resp.GrantedServices))
	}
	for _, gs := range resp.GrantedServices {
		if len(gs.Methods) == 0 {
			t.Fatalf("compiled-in expected every method granted; got empty for %s", gs.Service)
		}
	}
}

func TestBuildHandshakeResponse_RejectsProtocolDowngrade(t *testing.T) {
	gate, err := NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(gate)
	reg := &Registration{
		ID:       "any",
		Trust:    TrustCompiledIn,
		Metadata: piapi.Metadata{Name: "any", Version: "0.1"},
	}
	if err := manager.Register(reg); err != nil {
		t.Fatal(err)
	}

	req := hostproto.HandshakeRequest{
		ProtocolVersion: "2.0",
		ExtensionID:     "any",
	}
	params, _ := json.Marshal(req)
	_, err = BuildHandshakeResponse(reg, manager, params)
	if err == nil {
		t.Fatal("expected protocol mismatch error")
	}
	var perr *hostprotoError
	if !errorAs(err, &perr) {
		t.Fatalf("expected *hostprotoError; got %T", err)
	}
	if perr.Code != hostproto.ErrCodeHandshakeFailed {
		t.Fatalf("expected code %d; got %d", hostproto.ErrCodeHandshakeFailed, perr.Code)
	}
	if !strings.Contains(perr.Message, "protocol_version") {
		t.Fatalf("expected message to mention protocol_version; got %q", perr.Message)
	}
}

func TestBuildHandshakeResponse_DeniesUngrantedMethodsForHosted(t *testing.T) {
	gate, err := NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(gate)
	reg := &Registration{
		ID:       "hosted-x",
		Trust:    TrustThirdParty,
		Metadata: piapi.Metadata{Name: "hosted-x", Version: "0.1"},
	}
	if err := manager.Register(reg); err != nil {
		t.Fatal(err)
	}

	req := hostproto.HandshakeRequest{
		ProtocolVersion: hostproto.ProtocolVersion,
		ExtensionID:     "hosted-x",
		RequestedServices: []hostproto.RequestedService{
			{Service: "tools", Version: 1, Methods: []string{"register"}},
		},
	}
	params, _ := json.Marshal(req)
	out, err := BuildHandshakeResponse(reg, manager, params)
	if err != nil {
		t.Fatalf("expected success; got %v", err)
	}
	resp := out.(hostproto.HandshakeResponse)
	if len(resp.GrantedServices) != 1 {
		t.Fatalf("expected 1 granted service; got %d", len(resp.GrantedServices))
	}
	gs := resp.GrantedServices[0]
	if len(gs.Methods) != 0 {
		t.Fatalf("expected hosted+ungranted methods to be empty; got %v", gs.Methods)
	}
	if gs.DeniedReason == "" {
		t.Fatal("expected denied_reason on empty grant")
	}
}

// errorAs is a small helper to avoid pulling in errors.As just for
// type assertion in this tiny test file.
func errorAs(err error, target **hostprotoError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*hostprotoError); ok {
		*target = e
		return true
	}
	return false
}
