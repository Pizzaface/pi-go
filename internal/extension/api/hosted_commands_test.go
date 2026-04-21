package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_Commands_RegisterListUnregister(t *testing.T) {
	gate, _ := host.NewGate("")
	mgr := host.NewManager(gate)
	// TrustCompiledIn bypasses the gate entirely — no explicit grants needed.
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	cr := NewCommandRegistry()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetCommandRegistry(cr)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceCommands, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodCommandsRegister,
		hostproto.CommandsRegisterParams{Name: "todo", Label: "Todo"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	listRes, err := call(hostproto.MethodCommandsList, struct{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list hostproto.CommandsListResult
	_ = json.Unmarshal(listRes, &list)
	if len(list.Commands) != 1 || list.Commands[0].Owner != "ext-a" || list.Commands[0].Source != "runtime" {
		t.Fatalf("list = %+v", list)
	}
	if _, err := call(hostproto.MethodCommandsUnregister,
		hostproto.CommandsUnregisterParams{Name: "todo"}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	listRes2, _ := call(hostproto.MethodCommandsList, struct{}{})
	var list2 hostproto.CommandsListResult
	_ = json.Unmarshal(listRes2, &list2)
	if len(list2.Commands) != 0 {
		t.Fatalf("after unregister: %+v", list2)
	}
}

func TestHandleHostCall_Commands_CollisionAcrossExtensions(t *testing.T) {
	gate, _ := host.NewGate("")
	mgr := host.NewManager(gate)
	regA := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	regB := &host.Registration{ID: "ext-b", Trust: host.TrustCompiledIn}
	_ = mgr.Register(regA)
	_ = mgr.Register(regB)
	cr := NewCommandRegistry()
	hA := NewHostedHandler(mgr, regA, NoopBridge{})
	hA.SetCommandRegistry(cr)
	hB := NewHostedHandler(mgr, regB, NoopBridge{})
	hB.SetCommandRegistry(cr)

	call := func(h *HostedAPIHandler, payload any) error {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceCommands, Method: hostproto.MethodCommandsRegister, Payload: pb,
		})
		_, err := h.Handle(hostproto.MethodHostCall, params)
		return err
	}
	if err := call(hA, hostproto.CommandsRegisterParams{Name: "todo"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := call(hB, hostproto.CommandsRegisterParams{Name: "todo"}); err == nil {
		t.Fatalf("second register should collide")
	}
}
