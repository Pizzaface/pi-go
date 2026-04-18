package piext

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

// hostCallCapture spins up a goroutine that reads one JSON-RPC request
// from the transport's host-side pipe, captures its method+params, and
// replies with the given result.
func hostCallCapture(t *testing.T, hostIn io.Reader, hostOut io.Writer, result any) *sync.WaitGroup {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 8192)
		n, err := hostIn.Read(buf)
		if err != nil {
			return
		}
		var req map[string]any
		if err := json.Unmarshal(buf[:n], &req); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		resp, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
		_, _ = hostOut.Write(append(resp, '\n'))
	}()
	return wg
}

func TestRPCAPI_AppendEntrySendsHostCall(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	api := newRPCAPI(transport, piapi.Metadata{Name: "t"}, []GrantedService{
		{Service: "session", Version: 1, Methods: []string{"append_entry"}},
	})
	wg := hostCallCapture(t, hostIn, hostOut, map[string]any{})

	if err := api.AppendEntry("info", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}
	wg.Wait()
	_ = hostIn.Close()
	_ = hostOut.Close()
	_ = transport.Close()
}

func TestRPCAPI_AppendEntryRejectsInvalidKind(t *testing.T) {
	transport := newTransport(io.NopCloser(strings.NewReader("")), writeCloser{})
	api := newRPCAPI(transport, piapi.Metadata{Name: "t"}, nil)
	err := api.AppendEntry("Bad!", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("expected ErrInvalidKind, got %v", err)
	}
}

func TestTransportLogWriterEmitsNotify(t *testing.T) {
	extIn, hostOut := io.Pipe()
	hostIn, extOut := io.Pipe()
	transport := newTransport(extIn, extOut)
	api := newRPCAPI(transport, piapi.Metadata{Name: "t"}, nil)

	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := hostIn.Read(buf)
		done <- buf[:n]
	}()

	w := transportLogWriter{api: api}
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}

	raw := <-done
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	if msg["method"] != "pi.extension/host_call" {
		t.Fatalf("method = %v; want host_call", msg["method"])
	}
	_ = hostIn.Close()
	_ = hostOut.Close()
	_ = extOut
	_ = transport.Close()
}
