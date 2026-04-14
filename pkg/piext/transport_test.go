package piext

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestTransport_SendRecv(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	tr := newTransport(io.NopCloser(in), writeCloser{out})
	defer tr.Close()

	in.WriteString(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n")

	ctx := context.Background()
	var result map[string]any
	if err := tr.Call(ctx, "pi.extension/handshake", map[string]any{"protocol_version": "2.1"}, &result); err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result=%v; want ok:true", result)
	}

	sent := out.String()
	if !strings.Contains(sent, `"method":"pi.extension/handshake"`) {
		t.Fatalf("outgoing missing method; got %q", sent)
	}
	if !strings.Contains(sent, `"protocol_version":"2.1"`) {
		t.Fatalf("outgoing missing params; got %q", sent)
	}
}

func TestTransport_NotifyDoesNotAwaitResponse(t *testing.T) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	tr := newTransport(io.NopCloser(in), writeCloser{out})
	defer tr.Close()

	err := tr.Notify("pi.extension/log", map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("Notify err: %v", err)
	}
	sent := out.String()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(sent)), &parsed); err != nil {
		t.Fatalf("sent not JSON: %v", err)
	}
	if _, has := parsed["id"]; has {
		t.Fatalf("notification must not have id; got %v", parsed)
	}
}

type writeCloser struct{ *bytes.Buffer }

func (w writeCloser) Close() error { return nil }
