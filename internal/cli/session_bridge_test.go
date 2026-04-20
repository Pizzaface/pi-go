package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestCLISessionBridge_ForkReturnsUnsupported(t *testing.T) {
	b := NewSessionBridge(&bytes.Buffer{}, "", nil)
	_, err := b.Fork("x")
	if !errors.Is(err, piapi.ErrSessionControlUnsupportedInCLISentinel) {
		t.Fatalf("got %v; want ErrSessionControlUnsupportedInCLI", err)
	}
}

func TestCLISessionBridge_AppendEntryWritesToStderr(t *testing.T) {
	var buf bytes.Buffer
	b := NewSessionBridge(&buf, "", nil)
	if err := b.AppendEntry("ext", "info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("[ext/info]")) {
		t.Fatalf("stderr missing prefix: %s", buf.String())
	}
}

func TestCLISessionBridge_ReloadCallsReloadFn(t *testing.T) {
	called := false
	b := NewSessionBridge(nil, "", func(context.Context) error {
		called = true
		return nil
	})
	if err := b.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("reloadFn not called")
	}
}
