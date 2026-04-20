package piext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

func newTestAPI(granted []GrantedService) (*rpcAPI, *Transport) {
	tr := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{&bytes.Buffer{}})
	api := newRPCAPI(tr, piapi.Metadata{Name: "t", Version: "0.1"}, granted)
	return api, tr
}

func TestRPCAPI_Stubs_ReturnNotImplemented(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	checks := []struct {
		name string
		err  error
	}{
		{"RegisterShortcut", api.RegisterShortcut("x", piapi.ShortcutDescriptor{})},
		{"RegisterFlag", api.RegisterFlag("x", piapi.FlagDescriptor{})},
		{"RegisterProvider", api.RegisterProvider("x", piapi.ProviderDescriptor{})},
		{"UnregisterProvider", api.UnregisterProvider("x")},
		{"RegisterMessageRenderer", api.RegisterMessageRenderer("x", piapi.RendererDescriptor{})},
		{"SetActiveTools", api.SetActiveTools(nil)},
		{"SetThinkingLevel", api.SetThinkingLevel(piapi.ThinkingLow)},
	}
	for _, c := range checks {
		if !errors.Is(c.err, piapi.ErrNotImplementedSentinel) {
			t.Errorf("%s: got %v; want ErrNotImplemented", c.name, c.err)
		}
	}

	if _, err := api.SetModel(piapi.ModelRef{}); !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Errorf("SetModel: got %v; want ErrNotImplemented", err)
	}
}

func TestRPCAPI_Getters_ReturnZeroValues(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	if api.Name() != "t" {
		t.Errorf("Name=%q", api.Name())
	}
	if api.Version() != "0.1" {
		t.Errorf("Version=%q", api.Version())
	}
	if api.GetSessionName() != "" {
		t.Errorf("GetSessionName should be empty")
	}
	if api.GetActiveTools() != nil {
		t.Errorf("GetActiveTools should be nil")
	}
	if api.GetAllTools() != nil {
		t.Errorf("GetAllTools should be nil")
	}
	if api.GetCommands() != nil {
		t.Errorf("GetCommands should be nil")
	}
	if api.GetFlag("x") != nil {
		t.Errorf("GetFlag should be nil")
	}
	if api.GetThinkingLevel() != piapi.ThinkingOff {
		t.Errorf("GetThinkingLevel=%v", api.GetThinkingLevel())
	}
}

func TestRPCAPI_On_UnknownEvent_NotImplemented(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	err := api.On("random_event", func(_ piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
		return piapi.EventResult{}, nil
	})
	if !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Fatalf("got %v; want ErrNotImplemented", err)
	}
}

func TestRPCAPI_On_SessionStart_Denied(t *testing.T) {
	api, tr := newTestAPI(nil) // no events grant
	defer tr.Close()

	err := api.On(piapi.EventSessionStart, func(_ piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
		return piapi.EventResult{}, nil
	})
	if !errors.Is(err, piapi.ErrCapabilityDeniedSentinel) {
		t.Fatalf("got %v; want ErrCapabilityDenied", err)
	}
}

func TestRPCAPI_Exec_Denied(t *testing.T) {
	api, tr := newTestAPI(nil) // no exec grant
	defer tr.Close()

	_, err := api.Exec(context.Background(), "echo", []string{"hi"}, piapi.ExecOptions{})
	if !errors.Is(err, piapi.ErrCapabilityDeniedSentinel) {
		t.Fatalf("got %v; want ErrCapabilityDenied", err)
	}
}

func TestRPCAPI_HandleToolExecute_UnknownTool(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	payload := json.RawMessage(`{"tool_call_id":"c1","name":"nope","args":{}}`)
	result, err := api.handleToolExecute(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := result.(map[string]any)
	if m["is_error"] != true {
		t.Fatalf("expected is_error=true; got %v", m)
	}
}

func TestRPCAPI_HandleToolExecute_Success(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	api.tools["greet"] = piapi.ToolDescriptor{
		Name: "greet", Description: "g", Parameters: json.RawMessage(`{}`),
		Execute: func(_ context.Context, call piapi.ToolCall, onUpdate piapi.UpdateFunc) (piapi.ToolResult, error) {
			onUpdate(piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "partial"}}})
			return piapi.ToolResult{Content: []piapi.ContentPart{{Type: "text", Text: "done"}}}, nil
		},
	}
	payload := json.RawMessage(`{"tool_call_id":"c1","name":"greet","args":{}}`)
	result, err := api.handleToolExecute(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	tr2, ok := result.(piapi.ToolResult)
	if !ok {
		t.Fatalf("result type=%T", result)
	}
	if len(tr2.Content) == 0 || tr2.Content[0].Text != "done" {
		t.Fatalf("content=%v", tr2.Content)
	}
}

func TestRPCAPI_HandleToolExecute_ExecError(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	api.tools["bad"] = piapi.ToolDescriptor{
		Name: "bad", Description: "x", Parameters: json.RawMessage(`{}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, errors.New("boom")
		},
	}
	payload := json.RawMessage(`{"tool_call_id":"c1","name":"bad","args":{}}`)
	result, err := api.handleToolExecute(payload)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := result.(map[string]any)
	if m["is_error"] != true {
		t.Fatalf("expected is_error=true; got %v", m)
	}
}

func TestRPCAPI_HandleToolExecute_BadPayload(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	_, err := api.handleToolExecute(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for bad payload")
	}
}

func TestRPCAPI_OnEvent_SessionStart(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	called := false
	api.handlers[piapi.EventSessionStart] = []piapi.EventHandler{
		func(evt piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
			called = true
			if evt.EventName() != piapi.EventSessionStart {
				t.Errorf("event name=%q", evt.EventName())
			}
			return piapi.EventResult{}, nil
		},
	}
	params := json.RawMessage(`{"event":"session_start","version":1,"payload":{"reason":"boot"}}`)
	if _, err := api.onEvent(context.Background(), params); err != nil {
		t.Fatalf("onEvent err: %v", err)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestRPCAPI_OnEvent_ToolExecute_Dispatches(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	api.tools["greet"] = piapi.ToolDescriptor{
		Name: "greet", Description: "g", Parameters: json.RawMessage(`{}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, nil
		},
	}
	params := json.RawMessage(`{"event":"tool_execute","version":1,"payload":{"tool_call_id":"c","name":"greet","args":{}}}`)
	if _, err := api.onEvent(context.Background(), params); err != nil {
		t.Fatalf("onEvent err: %v", err)
	}
}

func TestRPCAPI_OnEvent_UnknownEvent_NoopControl(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	params := json.RawMessage(`{"event":"random","version":1,"payload":{}}`)
	result, err := api.onEvent(context.Background(), params)
	if err != nil {
		t.Fatalf("onEvent err: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok || m["control"] != nil {
		t.Fatalf("expected {control: nil}; got %v", result)
	}
}

func TestRPCAPI_OnEvent_BadPayload(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	_, err := api.onEvent(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for bad params")
	}
}

func TestRPCAPI_OnEvent_SessionStart_BadPayload(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	params := json.RawMessage(`{"event":"session_start","version":1,"payload":"not-an-object"}`)
	_, err := api.onEvent(context.Background(), params)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestRPCAPI_Events_NoopBus(t *testing.T) {
	api, tr := newTestAPI(nil)
	defer tr.Close()

	bus := api.Events()
	if err := bus.On("x", func(any) {}); !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Errorf("On: got %v", err)
	}
	if err := bus.Emit("x", nil); !errors.Is(err, piapi.ErrNotImplementedSentinel) {
		t.Errorf("Emit: got %v", err)
	}
}

func TestHandshakeError_Error(t *testing.T) {
	err := &handshakeError{got: "1.0"}
	if !strings.Contains(err.Error(), "1.0") {
		t.Fatalf("Error() should mention version; got %q", err.Error())
	}
}

func TestLog_WriterReturnsNonNil(t *testing.T) {
	w := Log()
	if w == nil {
		t.Fatal("Log() returned nil")
	}
	n, err := w.Write([]byte(""))
	if err != nil {
		t.Fatalf("Write err: %v", err)
	}
	if n != 0 {
		t.Errorf("Write n=%d; want 0", n)
	}
}

func TestSplitCap_NoDot(t *testing.T) {
	svc, method := splitCap("bare")
	if svc != "bare" || method != "" {
		t.Fatalf("splitCap(bare) = %q, %q", svc, method)
	}
}

func TestTransport_HandleInbound_RequestWithHandler(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	tr := newTransport(extIn, extOut)
	defer tr.Close()

	tr.HandleRequest("pi.ext/ping", func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"pong": true}, nil
	})

	go func() {
		_, _ = hostOut.Write([]byte(`{"jsonrpc":"2.0","id":42,"method":"pi.ext/ping","params":{}}` + "\n"))
	}()

	buf := make([]byte, 4096)
	n, _ := hostIn.Read(buf)
	var resp map[string]any
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("resp not JSON: %v; %s", err, buf[:n])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok || result["pong"] != true {
		t.Fatalf("expected pong:true; got %v", resp)
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
}

func TestTransport_HandleInbound_UnknownMethod_ReturnsError(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	tr := newTransport(extIn, extOut)
	defer tr.Close()

	go func() {
		_, _ = hostOut.Write([]byte(`{"jsonrpc":"2.0","id":7,"method":"pi.ext/unknown","params":{}}` + "\n"))
	}()

	buf := make([]byte, 4096)
	n, _ := hostIn.Read(buf)
	var resp map[string]any
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("resp not JSON: %v", err)
	}
	if resp["error"] == nil {
		t.Fatalf("expected error; got %v", resp)
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
}

func TestTransport_Call_RPCError(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	tr := newTransport(io.NopCloser(in), writeCloser{out})
	defer tr.Close()

	in.WriteString(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"denied"}}` + "\n")
	err := tr.Call(context.Background(), "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("Call err=%v; want rpc error", err)
	}
}

func TestTransport_Call_ContextCancelled(t *testing.T) {
	tr := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{&bytes.Buffer{}})
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := tr.Call(ctx, "x", nil, nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Call err=%v; want context.Canceled", err)
	}
}

func TestTransport_Close_Idempotent(t *testing.T) {
	tr := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{&bytes.Buffer{}})
	if err := tr.Close(); err != nil {
		t.Fatalf("first close err: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second close err: %v", err)
	}
}

func TestRPCAPI_On_SessionStart_Subscribes(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	tr := newTransport(extIn, extOut)
	defer tr.Close()

	api := newRPCAPI(tr, piapi.Metadata{Name: "t", Version: "0.1"}, []GrantedService{
		{Service: "events", Version: 1, Methods: []string{"session_start"}},
	})

	go func() {
		buf := make([]byte, 4096)
		n, _ := hostIn.Read(buf)
		var req map[string]any
		_ = json.Unmarshal(buf[:n], &req)
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{}})
		_, _ = hostOut.Write(append(resp, '\n'))
	}()

	err := api.On(piapi.EventSessionStart, func(_ piapi.Event, _ piapi.Context) (piapi.EventResult, error) {
		return piapi.EventResult{}, nil
	})
	if err != nil {
		t.Fatalf("On err: %v", err)
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
}

func TestRunInternal_InvalidMetadata(t *testing.T) {
	in, _ := io.Pipe()
	_, out := io.Pipe()
	err := runInternal(in, out, piapi.Metadata{}, func(piapi.API) error { return nil })
	if err == nil {
		t.Fatal("expected metadata validation error")
	}
}

func TestRunInternal_HandshakeProtocolMismatch(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()

	meta := piapi.Metadata{Name: "test", Version: "0.1.0", RequestedCapabilities: []string{"tools.register"}}
	done := make(chan error, 1)
	go func() {
		done <- runInternal(extIn, extOut, meta, func(piapi.API) error {
			t.Error("register should not be called on mismatch")
			return nil
		})
	}()

	buf := make([]byte, 4096)
	n, _ := hostIn.Read(buf)
	var req map[string]any
	_ = json.Unmarshal(buf[:n], &req)
	resp := `{"jsonrpc":"2.0","id":` + toString(req["id"]) + `,"result":{"protocol_version":"9.9","granted_services":[],"host_services":[],"dispatchable_events":[]}}` + "\n"
	_, _ = hostOut.Write([]byte(resp))

	err := <-done
	if err == nil {
		t.Fatal("expected protocol mismatch error")
	}
	if !strings.Contains(err.Error(), "protocol version mismatch") {
		t.Fatalf("err=%v; want protocol version mismatch", err)
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
}

func TestRunInternal_RegisterError(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()

	meta := piapi.Metadata{Name: "test", Version: "0.1.0", RequestedCapabilities: []string{"tools.register"}}
	done := make(chan error, 1)
	go func() {
		done <- runInternal(extIn, extOut, meta, func(piapi.API) error {
			return errors.New("register boom")
		})
	}()

	buf := make([]byte, 4096)
	n, _ := hostIn.Read(buf)
	var req map[string]any
	_ = json.Unmarshal(buf[:n], &req)
	resp := `{"jsonrpc":"2.0","id":` + toString(req["id"]) + `,"result":{"protocol_version":"2.1","granted_services":[],"host_services":[],"dispatchable_events":[]}}` + "\n"
	_, _ = hostOut.Write([]byte(resp))

	err := <-done
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v; want register boom", err)
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
}

func TestTransport_HandleInbound_Notification(t *testing.T) {
	extIn, hostOut := io.Pipe()
	_, extOut := io.Pipe()
	tr := newTransport(extIn, extOut)
	defer tr.Close()

	called := make(chan struct{})
	tr.HandleRequest("pi.ext/notif", func(_ context.Context, _ json.RawMessage) (any, error) {
		close(called)
		return nil, nil
	})

	go func() {
		_, _ = hostOut.Write([]byte(`{"jsonrpc":"2.0","method":"pi.ext/notif","params":{}}` + "\n"))
	}()

	<-called
	_ = hostOut.Close()
}
