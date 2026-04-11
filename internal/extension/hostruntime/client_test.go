package hostruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

func TestHostedClient_PerformsHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		defer close(serverErr)
		decoder := json.NewDecoder(serverConn)
		encoder := json.NewEncoder(serverConn)
		var request hostproto.RPCRequest
		if err := decoder.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if request.Method != hostproto.MethodHandshake {
			serverErr <- context.Canceled
			return
		}
		var handshake hostproto.HandshakeRequest
		if err := json.Unmarshal(request.Params, &handshake); err != nil {
			serverErr <- err
			return
		}
		if handshake.Mode != "interactive" || len(handshake.RequestedServices) != 2 {
			serverErr <- context.Canceled
			return
		}
		result, err := json.Marshal(hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
		})
		if err != nil {
			serverErr <- err
			return
		}
		serverErr <- encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      request.ID,
			Result:  result,
		})
	}()

	client := NewClient(clientConn, clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.Handshake(ctx, hostproto.HandshakeRequest{
		ExtensionID: "ext.demo",
		Mode:        "interactive",
		RequestedServices: []hostproto.ServiceRequest{
			{Service: "commands", Version: 1},
			{Service: "tools", Version: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Accepted {
		t.Fatalf("expected accepted handshake response, got %+v", response)
	}
	if !client.IsHealthy() {
		t.Fatal("expected client to be healthy after handshake")
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server side failed: %v", err)
	}
}

func TestHostedClient_TimeoutsSlowHandshake(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		decoder := json.NewDecoder(serverConn)
		var request hostproto.RPCRequest
		_ = decoder.Decode(&request)
		time.Sleep(500 * time.Millisecond)
	}()

	client := NewClient(clientConn, clientConn)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Handshake(ctx, hostproto.HandshakeRequest{
		ExtensionID: "ext.slow",
		Mode:        "interactive",
	})
	if err == nil {
		t.Fatal("expected handshake timeout to fail")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestHostedClient_MarksUnhealthyOnCrash(t *testing.T) {
	command, args := immediateExitCommand()
	process, err := StartProcess(context.Background(), ProcessConfig{
		Command: command,
		Args:    args,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := NewClientFromProcess(process)
	waitUntil(t, 2*time.Second, func() bool { return !client.IsHealthy() })
}

func immediateExitCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit 0"}
	}
	return "sh", []string{"-c", "exit 0"}
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// fakeDispatcher implements Dispatcher in tests.
type fakeDispatcher struct {
	calls []hostproto.HostCallParams
}

func (f *fakeDispatcher) Dispatch(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error) {
	f.calls = append(f.calls, params)
	return json.RawMessage(`{"ok":true}`), nil
}

// codedRPCErr satisfies the rpcCoder interface so we can test the
// extractRPCError code path without importing the services package.
type codedRPCErr struct {
	code    int
	message string
}

func (e *codedRPCErr) Error() string { return e.message }
func (e *codedRPCErr) RPCCode() int  { return e.code }

type errorDispatcher struct{}

func (errorDispatcher) Dispatch(string, hostproto.HostCallParams) (json.RawMessage, error) {
	return nil, &codedRPCErr{code: hostproto.ErrCodeInvalidParams, message: "bad"}
}

func mustMarshalRPC(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestServeInbound_DispatchesHostCall(t *testing.T) {
	extWrites := &bytes.Buffer{}
	hostWrites := &bytes.Buffer{}

	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      7,
		Method:  hostproto.MethodHostCall,
		Params: mustMarshalRPC(t, hostproto.HostCallParams{
			Service: "ui",
			Method:  "status",
			Version: 1,
			Payload: json.RawMessage(`{"text":"hi"}`),
		}),
	}
	data, _ := json.Marshal(req)
	extWrites.Write(append(data, '\n'))

	client := NewClient(extWrites, hostWrites)
	dispatcher := &fakeDispatcher{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- client.ServeInbound(ctx, "ext.demo", dispatcher)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("ServeInbound did not exit on EOF")
	}

	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher.calls = %d, want 1", len(dispatcher.calls))
	}
	if dispatcher.calls[0].Service != "ui" || dispatcher.calls[0].Method != "status" {
		t.Errorf("unexpected call: %+v", dispatcher.calls[0])
	}

	var resp hostproto.RPCResponse
	if err := json.Unmarshal(hostWrites.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != 7 {
		t.Errorf("response ID = %d, want 7", resp.ID)
	}
	if string(resp.Result) != `{"ok":true}` {
		t.Errorf("response result = %s", string(resp.Result))
	}
}

func TestServeInbound_DispatcherErrorBecomesRPCError(t *testing.T) {
	extWrites := &bytes.Buffer{}
	hostWrites := &bytes.Buffer{}

	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      11,
		Method:  hostproto.MethodHostCall,
		Params:  mustMarshalRPC(t, hostproto.HostCallParams{Service: "ui", Method: "status", Version: 1}),
	}
	data, _ := json.Marshal(req)
	extWrites.Write(append(data, '\n'))

	client := NewClient(extWrites, hostWrites)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.ServeInbound(ctx, "ext.demo", errorDispatcher{}) }()
	<-done

	var resp hostproto.RPCResponse
	if err := json.Unmarshal(hostWrites.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Code != hostproto.ErrCodeInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, hostproto.ErrCodeInvalidParams)
	}
}

func TestServeInbound_UnknownMethodIsError(t *testing.T) {
	extWrites := &bytes.Buffer{}
	hostWrites := &bytes.Buffer{}

	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      5,
		Method:  "pi.extension/nope",
	}
	data, _ := json.Marshal(req)
	extWrites.Write(append(data, '\n'))

	client := NewClient(extWrites, hostWrites)
	dispatcher := &fakeDispatcher{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.ServeInbound(ctx, "ext.demo", dispatcher) }()
	<-done

	if len(dispatcher.calls) != 0 {
		t.Error("dispatcher should not be called for unknown method")
	}
	var resp hostproto.RPCResponse
	if err := json.Unmarshal(hostWrites.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != hostproto.ErrCodeMethodNotFound {
		t.Errorf("unexpected response error: %+v", resp.Error)
	}
}
