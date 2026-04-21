package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_Sigils_RegisterListUnregister(t *testing.T) {
	gate, _ := host.NewGate("")
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	sr := NewSigilRegistry()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetSigilRegistry(sr)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceSigils, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodSigilsRegister,
		hostproto.SigilsRegisterParams{Prefixes: []string{"todo", "plan"}}); err != nil {
		t.Fatalf("register: %v", err)
	}
	listRes, err := call(hostproto.MethodSigilsList, struct{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var list hostproto.SigilsListResult
	_ = json.Unmarshal(listRes, &list)
	if len(list.Prefixes) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if _, err := call(hostproto.MethodSigilsUnregister,
		hostproto.SigilsUnregisterParams{Prefixes: []string{"plan"}}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	listRes2, _ := call(hostproto.MethodSigilsList, struct{}{})
	var list2 hostproto.SigilsListResult
	_ = json.Unmarshal(listRes2, &list2)
	if len(list2.Prefixes) != 1 || list2.Prefixes[0].Prefix != "todo" {
		t.Fatalf("after unregister: %+v", list2)
	}
}
