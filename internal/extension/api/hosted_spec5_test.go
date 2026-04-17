package api

import (
	"encoding/json"
	"testing"

	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// newTestManager returns a manager backed by an empty gate (non-existent
// approvals file). Tests register extensions with TrustCompiledIn so the
// gate's allow/deny logic is bypassed entirely.
func newTestManager(t *testing.T) *host.Manager {
	t.Helper()
	gate, err := host.NewGate("")
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}
	return host.NewManager(gate)
}

func TestHosted_SessionServiceRoutesToBridge(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	mgr := newTestManager(t)
	reg := &host.Registration{ID: "e", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "e"}}
	_ = mgr.Register(reg)
	h := NewHostedHandler(mgr, reg, fb)

	raw, _ := json.Marshal(hostproto.HostCallParams{
		Service: hostproto.ServiceSession, Version: 1, Method: hostproto.MethodSessionAppendEntry,
		Payload: json.RawMessage(`{"kind":"info","payload":{"k":"v"}}`),
	})
	if _, err := h.Handle(hostproto.MethodHostCall, raw); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fb.Calls) != 1 || fb.Calls[0].Method != "AppendEntry" {
		t.Fatalf("fake calls = %+v", fb.Calls)
	}
}

func TestHosted_LegacyToolUpdateRoutesThroughBridge(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	mgr := newTestManager(t)
	reg := &host.Registration{ID: "e", Trust: host.TrustCompiledIn, Metadata: piapi.Metadata{Name: "e"}}
	_ = mgr.Register(reg)
	h := NewHostedHandler(mgr, reg, fb)

	raw, _ := json.Marshal(hostproto.ToolStreamUpdateParams{
		ToolCallID: "call-1",
		Partial:    json.RawMessage(`{"content":[{"type":"text","text":"ping 1"}]}`),
	})
	if _, err := h.Handle(hostproto.MethodToolUpdate, raw); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fb.Calls) != 1 || fb.Calls[0].Method != "EmitToolUpdate" {
		t.Fatalf("fake calls = %+v", fb.Calls)
	}
}
