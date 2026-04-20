package api

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

func newEntry(extID, tool string) (piapi.ToolDescriptor, *host.Registration) {
	desc := piapi.ToolDescriptor{
		Name:        tool,
		Description: "t",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Execute: func(_ context.Context, _ piapi.ToolCall, _ piapi.UpdateFunc) (piapi.ToolResult, error) {
			return piapi.ToolResult{}, nil
		},
	}
	reg := &host.Registration{ID: extID, Trust: host.TrustThirdParty}
	return desc, reg
}

func TestRegistry_AddAndSnapshot(t *testing.T) {
	r := NewHostedToolRegistry()
	desc, reg := newEntry("ext-a", "greet")
	if err := r.Add("ext-a", desc, reg, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	snap := r.Snapshot()
	if len(snap) != 1 || snap[0].Desc.Name != "greet" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestRegistry_CollisionDifferentExt(t *testing.T) {
	r := NewHostedToolRegistry()
	descA, regA := newEntry("ext-a", "greet")
	descB, regB := newEntry("ext-b", "greet")
	_ = r.Add("ext-a", descA, regA, nil)
	err := r.Add("ext-b", descB, regB, nil)
	var ce *CollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("want CollisionError, got %T: %v", err, err)
	}
	if ce.ConflictWith != "ext-a" {
		t.Fatalf("ConflictWith = %q", ce.ConflictWith)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatal("second add should not have landed")
	}
}

func TestRegistry_ReplaceSameExt(t *testing.T) {
	r := NewHostedToolRegistry()
	desc1, reg := newEntry("ext-a", "greet")
	desc2 := desc1
	desc2.Description = "updated"
	_ = r.Add("ext-a", desc1, reg, nil)
	if err := r.Add("ext-a", desc2, reg, nil); err != nil {
		t.Fatalf("replace: %v", err)
	}
	snap := r.Snapshot()
	if snap[0].Desc.Description != "updated" {
		t.Fatal("descriptor not replaced")
	}
}

func TestRegistry_RemoveOwned(t *testing.T) {
	r := NewHostedToolRegistry()
	desc, reg := newEntry("ext-a", "greet")
	_ = r.Add("ext-a", desc, reg, nil)
	if err := r.Remove("ext-a", "greet"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(r.Snapshot()) != 0 {
		t.Fatal("tool not removed")
	}
}

func TestRegistry_RemoveNotOwned(t *testing.T) {
	r := NewHostedToolRegistry()
	desc, reg := newEntry("ext-a", "greet")
	_ = r.Add("ext-a", desc, reg, nil)
	err := r.Remove("ext-b", "greet")
	if err == nil {
		t.Fatal("Remove across owners must error")
	}
}

func TestRegistry_RemoveMissingIdempotent(t *testing.T) {
	r := NewHostedToolRegistry()
	if err := r.Remove("ext-a", "nope"); err != nil {
		t.Fatalf("Remove missing must be idempotent; got %v", err)
	}
}

func TestRegistry_RemoveExt(t *testing.T) {
	r := NewHostedToolRegistry()
	d1, regA := newEntry("ext-a", "t1")
	d2, _ := newEntry("ext-a", "t2")
	_ = r.Add("ext-a", d1, regA, nil)
	_ = r.Add("ext-a", d2, regA, nil)
	r.RemoveExt("ext-a")
	if len(r.Snapshot()) != 0 {
		t.Fatal("RemoveExt left entries behind")
	}
}

func TestRegistry_ConcurrentAddSnapshot(t *testing.T) {
	r := NewHostedToolRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			d, reg := newEntry("ext", "t_"+strconv.Itoa(i))
			_ = r.Add("ext", d, reg, nil)
		}(i)
		go func() {
			defer wg.Done()
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != 64 {
		t.Fatalf("want 64, got %d", got)
	}
}
