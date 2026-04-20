package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func newWiredHandler(t *testing.T, extID string) (*HostedAPIHandler, *HostedToolRegistry, *Readiness) {
	t.Helper()
	gate := host.NewGateInMemory(map[string]map[string][]string{
		extID: {
			"tools":  {"register", "unregister"},
			"events": {"tool_execute"},
		},
	}, host.TrustThirdParty)
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: extID, Mode: "hosted-go", Trust: host.TrustThirdParty}
	if err := mgr.Register(reg); err != nil {
		t.Fatalf("register extension: %v", err)
	}
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	registry := NewHostedToolRegistry()
	readiness := NewReadiness()
	h.SetRegistry(registry)
	h.SetReadiness(readiness)
	return h, registry, readiness
}

func hostCall(t *testing.T, service, method string, payload any) json.RawMessage {
	t.Helper()
	pb, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	params, err := json.Marshal(hostproto.HostCallParams{
		Service: service,
		Version: 1,
		Method:  method,
		Payload: pb,
	})
	if err != nil {
		t.Fatalf("marshal host_call: %v", err)
	}
	return params
}

func TestHostedHandler_RegisterTool_LandsInRegistry(t *testing.T) {
	h, registry, readiness := newWiredHandler(t, "ext-a")
	readiness.Track("ext-a")

	params := hostCall(t, hostproto.ServiceTools, hostproto.MethodToolsRegister, map[string]any{
		"name":        "greet",
		"label":       "Greet",
		"description": "say hi",
		"parameters":  json.RawMessage(`{"type":"object"}`),
	})
	if _, err := h.Handle(hostproto.MethodHostCall, params); err != nil {
		t.Fatalf("register: %v", err)
	}
	snap := registry.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 registry entry, got %d", len(snap))
	}
	if snap[0].ExtID != "ext-a" || snap[0].Desc.Name != "greet" {
		t.Fatalf("unexpected entry: %+v", snap[0])
	}
}

func TestHostedHandler_UnregisterTool(t *testing.T) {
	h, registry, readiness := newWiredHandler(t, "ext-a")
	readiness.Track("ext-a")

	regParams := hostCall(t, hostproto.ServiceTools, hostproto.MethodToolsRegister, map[string]any{
		"name":       "greet",
		"parameters": json.RawMessage(`{"type":"object"}`),
	})
	if _, err := h.Handle(hostproto.MethodHostCall, regParams); err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(registry.Snapshot()) != 1 {
		t.Fatalf("registry not populated before unregister")
	}

	unregParams := hostCall(t, hostproto.ServiceTools, hostproto.MethodToolsUnregister, map[string]any{
		"name": "greet",
	})
	if _, err := h.Handle(hostproto.MethodHostCall, unregParams); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if snap := registry.Snapshot(); len(snap) != 0 {
		t.Fatalf("want empty snapshot, got %d entries: %+v", len(snap), snap)
	}
}

func TestHostedHandler_ExtReady(t *testing.T) {
	h, _, readiness := newWiredHandler(t, "ext-a")
	readiness.Track("ext-a")

	params := hostCall(t, hostproto.ServiceExt, hostproto.MethodExtReady, map[string]any{})
	if _, err := h.Handle(hostproto.MethodHostCall, params); err != nil {
		t.Fatalf("ext.ready: %v", err)
	}
	if got := readiness.State("ext-a"); got != ReadinessReady {
		t.Fatalf("want ReadinessReady, got %s", got)
	}
}
