package loader

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
)

func TestReload_EmptyCwdReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	gate, err := host.NewGate(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := host.NewManager(gate)
	got, err := Reload(context.Background(), manager, proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 candidates; got %d", len(got))
	}
}
