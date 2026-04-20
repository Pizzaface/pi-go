package host

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// pipePair returns two ends of an in-memory duplex pipe: (a-reader,
// a-writer) used as one side and (b-reader, b-writer) as the other.
func pipePair() (ar io.Reader, aw io.Writer, br io.Reader, bw io.Writer) {
	ar, bw = io.Pipe()
	br, aw = io.Pipe()
	return
}

func TestRPCConn_Call(t *testing.T) {
	hostRead, hostWrite, extRead, extWrite := pipePair()
	// Host side: no inbound handler needed for this test.
	conn := NewRPCConn(hostRead, hostWrite, func(method string, params json.RawMessage) (any, error) {
		t.Fatalf("unexpected inbound: %s", method)
		return nil, nil
	})
	defer conn.Close()

	// Fake extension: read one request, reply with result.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		n, _ := extRead.Read(buf)
		var req map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(buf[:n]))), &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]any{"ok": true},
		}
		data, _ := json.Marshal(resp)
		extWrite.Write(append(data, '\n'))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var out struct {
		OK bool `json:"ok"`
	}
	if err := conn.Call(ctx, "test.method", map[string]int{"x": 1}, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
	<-done
}

func TestRPCConn_Notify(t *testing.T) {
	hostRead, hostWrite, extRead, _ := pipePair()
	conn := NewRPCConn(hostRead, hostWrite, func(string, json.RawMessage) (any, error) { return nil, nil })
	defer conn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	var gotNoID bool
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		n, _ := extRead.Read(buf)
		var msg map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(buf[:n]))), &msg); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		_, hasID := msg["id"]
		gotNoID = !hasID
	}()

	if err := conn.Notify("log", map[string]string{"msg": "hi"}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if !gotNoID {
		t.Fatal("notification must not include id")
	}
}

func TestRPCConn_InboundRequest(t *testing.T) {
	hostRead, hostWrite, extRead, extWrite := pipePair()
	conn := NewRPCConn(hostRead, hostWrite, func(method string, params json.RawMessage) (any, error) {
		if method != "svc.m" {
			t.Fatalf("unexpected method %q", method)
		}
		return map[string]int{"n": 42}, nil
	})
	defer conn.Close()

	// Simulate an inbound request from the extension.
	req := map[string]any{"jsonrpc": "2.0", "id": 7, "method": "svc.m", "params": map[string]any{}}
	data, _ := json.Marshal(req)
	extWrite.Write(append(data, '\n'))

	buf := make([]byte, 4096)
	// Read the host's response.
	n, _ := extRead.Read(buf)
	var resp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(buf[:n]))), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if fmtId(resp["id"]) != "7" {
		t.Fatalf("expected id=7; got %v", resp["id"])
	}
	result, _ := resp["result"].(map[string]any)
	if int(result["n"].(float64)) != 42 {
		t.Fatalf("expected n=42; got %v", resp["result"])
	}
}

func fmtId(v any) string {
	switch x := v.(type) {
	case float64:
		if x == float64(int(x)) {
			return stringFromInt(int(x))
		}
	}
	return "?"
}

func stringFromInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	neg := n < 0
	if neg {
		n = -n
	}
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
