package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

func TestClient_SendRequest_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	client := NewClient(io.NopCloser(&bytes.Buffer{}), &buf)

	id, err := client.sendRequest(hostproto.MethodHandshake, map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("sendRequest: %v", err)
	}
	if id != 1 {
		t.Errorf("first id = %d, want 1", id)
	}

	var req hostproto.RPCRequest
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Method != hostproto.MethodHandshake {
		t.Errorf("Method = %q", req.Method)
	}
	if req.ID != 1 {
		t.Errorf("ID = %d", req.ID)
	}
}

func TestClient_NextID_Increments(t *testing.T) {
	var buf bytes.Buffer
	client := NewClient(io.NopCloser(&bytes.Buffer{}), &buf)
	id1, _ := client.sendRequest(hostproto.MethodHandshake, nil)
	id2, _ := client.sendRequest(hostproto.MethodHandshake, nil)
	id3, _ := client.sendRequest(hostproto.MethodHandshake, nil)
	if id1 != 1 || id2 != 2 || id3 != 3 {
		t.Errorf("ids = %d, %d, %d, want 1, 2, 3", id1, id2, id3)
	}
}

func TestClient_ReadLoop_DispatchesResponse(t *testing.T) {
	// Use io.Pipe so the decoder can block waiting for input.
	r, w := io.Pipe()
	client := NewClient(io.NopCloser(r), io.Discard)

	// Prime a pending response waiter.
	waiter := client.waitFor(42)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go client.readLoop(ctx)

	// Host writes a response.
	go func() {
		resp := hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      42,
			Result:  json.RawMessage(`{"ok":true}`),
		}
		data, _ := json.Marshal(resp)
		_, _ = w.Write(append(data, '\n'))
	}()

	select {
	case got := <-waiter:
		if got.err != nil {
			t.Fatalf("waiter err: %v", got.err)
		}
		if string(got.result) != `{"ok":true}` {
			t.Errorf("result = %s", string(got.result))
		}
	case <-ctx.Done():
		t.Fatal("waiter did not receive response")
	}
}

func TestClient_HostCall_SendsRequestAndReceivesResult(t *testing.T) {
	// Bidirectional in-memory pipes.
	fromHostR, fromHostW := io.Pipe() // host writes to W, ext reads from R
	toHostR, toHostW := io.Pipe()     // ext writes to W, host reads from R

	client := NewClient(io.NopCloser(fromHostR), toHostW)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go client.readLoop(ctx)

	// Fake host.
	go func() {
		decoder := json.NewDecoder(toHostR)
		encoder := json.NewEncoder(fromHostW)
		var req hostproto.RPCRequest
		if err := decoder.Decode(&req); err != nil {
			t.Errorf("fake host decode: %v", err)
			return
		}
		if req.Method != hostproto.MethodHostCall {
			t.Errorf("Method = %q", req.Method)
		}
		_ = encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      req.ID,
			Result:  json.RawMessage(`{"ok":true}`),
		})
	}()

	result, err := client.HostCall(ctx, "ui", "status", 1, map[string]string{"text": "hi"})
	if err != nil {
		t.Fatalf("HostCall: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Errorf("result = %s", string(result))
	}
}

func TestClient_HostCall_PropagatesRPCError(t *testing.T) {
	fromHostR, fromHostW := io.Pipe()
	toHostR, toHostW := io.Pipe()

	client := NewClient(io.NopCloser(fromHostR), toHostW)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go client.readLoop(ctx)

	go func() {
		decoder := json.NewDecoder(toHostR)
		encoder := json.NewEncoder(fromHostW)
		var req hostproto.RPCRequest
		_ = decoder.Decode(&req)
		_ = encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      req.ID,
			Error: &hostproto.RPCError{
				Code:    hostproto.ErrCodeInvalidParams,
				Message: "text required",
			},
		})
	}()

	_, err := client.HostCall(ctx, "ui", "status", 1, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "rpc error -32602: text required"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestClient_Serve_PerformsHandshake(t *testing.T) {
	fromHostR, fromHostW := io.Pipe()
	toHostR, toHostW := io.Pipe()

	client := NewClient(io.NopCloser(fromHostR), toHostW)

	// Fake host: reads the handshake request and replies accepted=true.
	go func() {
		decoder := json.NewDecoder(toHostR)
		encoder := json.NewEncoder(fromHostW)
		var req hostproto.RPCRequest
		if err := decoder.Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if req.Method != hostproto.MethodHandshake {
			t.Errorf("first method = %q, want handshake", req.Method)
		}
		respBytes, _ := json.Marshal(hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        true,
			HostServices:    []hostproto.HostServiceInfo{{Service: "ui", Version: 1, Methods: []string{"status"}}},
		})
		_ = encoder.Encode(hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      req.ID,
			Result:  respBytes,
		})
	}()

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- client.Serve(ctx, ServeOptions{
			ExtensionID: "ext.demo",
			Mode:        "hosted_stdio",
			RequestedServices: []hostproto.ServiceRequest{
				{Service: "ui", Version: 1, Methods: []string{"status"}},
			},
			OnReady: func(ready HandshakeReady) error {
				if !ready.Response.Accepted {
					t.Error("OnReady called with non-accepted handshake")
				}
				if len(ready.Response.HostServices) != 1 {
					t.Errorf("HostServices = %d, want 1", len(ready.Response.HostServices))
				}
				cancel()
				return nil
			},
		})
	}()

	select {
	case err := <-serveDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("Serve did not complete")
	}
}

func TestClient_Serve_RejectsEmptyExtensionID(t *testing.T) {
	client := NewClient(io.NopCloser(&bytes.Buffer{}), io.Discard)
	err := client.Serve(context.Background(), ServeOptions{})
	if err == nil {
		t.Fatal("expected error for empty ExtensionID")
	}
}

func TestClient_ReadLoop_DispatchesError(t *testing.T) {
	r, w := io.Pipe()
	client := NewClient(io.NopCloser(r), io.Discard)

	waiter := client.waitFor(7)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go client.readLoop(ctx)

	go func() {
		resp := hostproto.RPCResponse{
			JSONRPC: hostproto.JSONRPCVersion,
			ID:      7,
			Error: &hostproto.RPCError{
				Code:    hostproto.ErrCodeInvalidParams,
				Message: "bad payload",
			},
		}
		data, _ := json.Marshal(resp)
		_, _ = w.Write(append(data, '\n'))
	}()

	select {
	case got := <-waiter:
		if got.err == nil {
			t.Fatal("expected error")
		}
		if got.err.Error() != "rpc error -32602: bad payload" {
			t.Errorf("error = %q", got.err.Error())
		}
	case <-ctx.Done():
		t.Fatal("waiter did not receive response")
	}
}
