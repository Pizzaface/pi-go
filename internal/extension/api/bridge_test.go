package api

import (
	"testing"

	testbridge "github.com/dimetron/pi-go/internal/extension/api/testing"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestFakeBridgeRecordsCalls(t *testing.T) {
	fb := &testbridge.FakeBridge{}
	if err := fb.AppendEntry("ext", "info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if err := fb.SetSessionTitle("hi"); err != nil {
		t.Fatal(err)
	}
	if got := fb.GetSessionTitle(); got != "hi" {
		t.Fatalf("title = %q; want hi", got)
	}
	if len(fb.Calls) != 3 {
		t.Fatalf("calls = %d; want 3", len(fb.Calls))
	}
	if fb.Calls[0].Method != "AppendEntry" || fb.Calls[1].Method != "SetSessionTitle" {
		t.Fatalf("call order wrong: %+v", fb.Calls)
	}
}

func TestNoopBridgeCompiles(t *testing.T) {
	var b SessionBridge = NoopBridge{}
	_ = b.AppendEntry("", "info", nil)
	_, _ = b.Fork("")
	if _, err := b.NewSession(piapi.NewSessionOptions{}); err != nil {
		t.Fatal(err)
	}
}
