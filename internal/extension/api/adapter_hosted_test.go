package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// fakeConn captures the last Call parameters and returns a configured reply.
type fakeConn struct {
	lastMethod string
	lastParams hostproto.ExtensionEventParams
	reply      any
	err        error
}

func (f *fakeConn) Call(_ context.Context, method string, params any, result any) error {
	f.lastMethod = method
	b, _ := json.Marshal(params)
	_ = json.Unmarshal(b, &f.lastParams)
	if f.err != nil {
		return f.err
	}
	if result != nil && f.reply != nil {
		rb, _ := json.Marshal(f.reply)
		return json.Unmarshal(rb, result)
	}
	return nil
}

func TestHostedAdapter_SendsExtensionEvent(t *testing.T) {
	conn := &fakeConn{
		reply: map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Hello, pi!"}},
		},
	}
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty, Conn: host.NewRPCConnFromCaller(conn)}
	mgr := host.NewManager(host.NewGateInMemory(map[string]map[string][]string{
		"ext-a": {"events": {"tool_execute"}},
	}, host.TrustThirdParty))

	entry := HostedToolEntry{
		ExtID: "ext-a",
		Desc: piapi.ToolDescriptor{
			Name:        "greet",
			Description: "say hi",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
		},
		Reg:     reg,
		Manager: mgr,
	}
	tl, err := NewHostedToolAdapter(entry)
	if err != nil {
		t.Fatalf("adapter build: %v", err)
	}
	res, err := invokeHostedAdapterForTest(tl, context.Background(), "call-1", map[string]any{"name": "pi"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if content, _ := res["content"].([]any); len(content) == 0 {
		t.Fatalf("empty content: %+v", res)
	}
	if conn.lastMethod != hostproto.MethodExtensionEvent {
		t.Fatalf("wrong method: %q", conn.lastMethod)
	}
	if conn.lastParams.Event != "tool_execute" {
		t.Fatalf("wrong event: %q", conn.lastParams.Event)
	}
	if conn.lastParams.Version != 1 {
		t.Fatalf("wrong version: %d", conn.lastParams.Version)
	}
	// Inner payload should include tool_call_id, name, args, timeout_ms.
	var inner map[string]any
	if err := json.Unmarshal(conn.lastParams.Payload, &inner); err != nil {
		t.Fatalf("inner payload unmarshal: %v", err)
	}
	if inner["tool_call_id"] != "call-1" {
		t.Fatalf("wrong tool_call_id: %v", inner["tool_call_id"])
	}
	if inner["name"] != "greet" {
		t.Fatalf("wrong name: %v", inner["name"])
	}
	if _, ok := inner["timeout_ms"]; !ok {
		t.Fatalf("missing timeout_ms: %+v", inner)
	}
}

func TestHostedAdapter_GateDenied(t *testing.T) {
	mgr := host.NewManager(host.NewGateInMemory(nil, host.TrustThirdParty)) // no grants
	entry := HostedToolEntry{
		ExtID:   "ext-a",
		Desc:    piapi.ToolDescriptor{Name: "greet", Description: "x"},
		Reg:     &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty},
		Manager: mgr,
	}
	tl, err := NewHostedToolAdapter(entry)
	if err != nil {
		t.Fatalf("adapter build: %v", err)
	}
	_, err = invokeHostedAdapterForTest(tl, context.Background(), "call-1", map[string]any{})
	if err == nil {
		t.Fatal("want gate denial error")
	}
}

func TestHostedAdapter_RPCError(t *testing.T) {
	conn := &fakeConn{err: errors.New("boom")}
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty, Conn: host.NewRPCConnFromCaller(conn)}
	mgr := host.NewManager(host.NewGateInMemory(map[string]map[string][]string{
		"ext-a": {"events": {"tool_execute"}},
	}, host.TrustThirdParty))
	entry := HostedToolEntry{
		ExtID: "ext-a",
		Desc: piapi.ToolDescriptor{
			Name:        "greet",
			Description: "x",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
		Reg:     reg,
		Manager: mgr,
	}
	tl, err := NewHostedToolAdapter(entry)
	if err != nil {
		t.Fatalf("adapter build: %v", err)
	}
	_, err = invokeHostedAdapterForTest(tl, context.Background(), "c", map[string]any{})
	if err == nil {
		t.Fatal("want rpc error")
	}
}
