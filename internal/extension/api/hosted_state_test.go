package api_test

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension"
	"github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_State_SetGetPatchDelete(t *testing.T) {
	dir := t.TempDir()
	store := extension.NewStateStore(dir, "sess-1")
	gate, _ := host.NewGate("")
	mgr := host.NewManager(gate)
	// TrustCompiledIn bypasses the gate entirely — no explicit grants needed.
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	h := api.NewHostedHandler(mgr, reg, api.NoopBridge{})
	h.SetStateStore(store.HostedView())

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceState, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodStateSet, hostproto.StateSetParams{Value: json.RawMessage(`{"a":1}`)}); err != nil {
		t.Fatalf("set: %v", err)
	}
	getRes, err := call(hostproto.MethodStateGet, struct{}{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got hostproto.StateGetResult
	_ = json.Unmarshal(getRes, &got)
	if !got.Exists {
		t.Fatalf("get got=%+v", got)
	}
	var blob0 map[string]any
	_ = json.Unmarshal(got.Value, &blob0)
	if blob0["a"].(float64) != 1 {
		t.Fatalf("set value mismatch: %v", blob0)
	}
	if _, err := call(hostproto.MethodStatePatch, hostproto.StatePatchParams{Patch: json.RawMessage(`{"b":2}`)}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	getRes2, _ := call(hostproto.MethodStateGet, struct{}{})
	var got2 hostproto.StateGetResult
	_ = json.Unmarshal(getRes2, &got2)
	var blob map[string]any
	_ = json.Unmarshal(got2.Value, &blob)
	if blob["a"].(float64) != 1 || blob["b"].(float64) != 2 {
		t.Fatalf("after patch: %v", blob)
	}
	if _, err := call(hostproto.MethodStateDelete, struct{}{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	getRes3, _ := call(hostproto.MethodStateGet, struct{}{})
	var got3 hostproto.StateGetResult
	_ = json.Unmarshal(getRes3, &got3)
	if got3.Exists {
		t.Fatalf("after delete still exists: %+v", got3)
	}
}
