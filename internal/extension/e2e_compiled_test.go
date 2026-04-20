package extension

import (
	"context"
	"testing"

	_ "github.com/pizzaface/go-pi/internal/extensions/hello"
)

// TestE2E_CompiledIn exercises the compiled-in registration path end-to-
// end: a blank import of internal/extensions/hello wires its init() into
// the compiled registry, BuildRuntime walks the registry, adapts the
// extension's tool descriptors to the ADK tool.Tool interface, and
// surfaces the "greet" tool in rt.Tools.
func TestE2E_CompiledIn(t *testing.T) {
	rt, err := BuildRuntime(context.Background(), RuntimeConfig{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}

	found := false
	for _, tl := range rt.Tools {
		if tl.Name() == "greet" {
			found = true
			break
		}
	}
	if !found {
		var names []string
		for _, tl := range rt.Tools {
			names = append(names, tl.Name())
		}
		t.Fatalf("expected greet tool in rt.Tools; got %v", names)
	}

	if rt.Manager == nil {
		t.Fatal("expected rt.Manager to be non-nil")
	}
	reg := rt.Manager.Get("hello")
	if reg == nil {
		t.Fatal("expected hello registration in manager")
	}
}
