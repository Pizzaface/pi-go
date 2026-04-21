package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

func TestHandleHostCall_UI_StatusWidgetNotifyDialog(t *testing.T) {
	gate, _ := host.NewGate("")
	mgr := host.NewManager(gate)
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustCompiledIn}
	_ = mgr.Register(reg)
	ui := NewUIService()
	h := NewHostedHandler(mgr, reg, NoopBridge{})
	h.SetUIService(ui)

	call := func(method string, payload any) (json.RawMessage, error) {
		pb, _ := json.Marshal(payload)
		params, _ := json.Marshal(hostproto.HostCallParams{
			Service: hostproto.ServiceUI, Method: method, Payload: pb,
		})
		res, err := h.Handle(hostproto.MethodHostCall, params)
		if err != nil {
			return nil, err
		}
		rb, _ := json.Marshal(res)
		return rb, nil
	}

	if _, err := call(hostproto.MethodUIStatus, hostproto.UIStatusParams{Text: "hi"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if ui.Status("ext-a") != "hi" {
		t.Fatalf("status not stored")
	}
	if _, err := call(hostproto.MethodUIWidget, hostproto.UIWidgetParams{
		ID: "w1", Lines: []string{"hello"},
		Position: hostproto.Position{Mode: "sticky", Anchor: "top"},
	}); err != nil {
		t.Fatalf("widget: %v", err)
	}
	if len(ui.Widgets("ext-a")) != 1 {
		t.Fatalf("widget not stored")
	}
	if _, err := call(hostproto.MethodUINotify, hostproto.UINotifyParams{Level: "info", Text: "hello"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	dRes, err := call(hostproto.MethodUIDialog, hostproto.UIDialogParams{
		Title: "confirm", Buttons: []hostproto.UIDialogButton{{ID: "ok", Label: "OK"}},
	})
	if err != nil {
		t.Fatalf("dialog: %v", err)
	}
	var dr hostproto.UIDialogResult
	_ = json.Unmarshal(dRes, &dr)
	if dr.DialogID == "" {
		t.Fatalf("dialog returned empty id")
	}
	if owner, ok := ui.DialogOwner(dr.DialogID); !ok || owner != "ext-a" {
		t.Fatalf("dialog owner = %q %v", owner, ok)
	}
}
