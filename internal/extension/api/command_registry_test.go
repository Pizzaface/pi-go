package api

import (
	"errors"
	"testing"
)

func TestCommandRegistry_AddAndList(t *testing.T) {
	r := NewCommandRegistry()
	if err := r.Add("ext-a", CommandSpec{Name: "todo", Label: "Todo"}, "runtime"); err != nil {
		t.Fatal(err)
	}
	entries := r.List()
	if len(entries) != 1 || entries[0].Spec.Name != "todo" || entries[0].Owner != "ext-a" {
		t.Fatalf("list = %+v", entries)
	}
}

func TestCommandRegistry_CollisionRejected(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")
	err := r.Add("ext-b", CommandSpec{Name: "todo"}, "runtime")
	var ce *CommandCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("want CommandCollisionError, got %v", err)
	}
	if ce.ConflictWith != "ext-a" {
		t.Fatalf("conflict = %q", ce.ConflictWith)
	}
}

func TestCommandRegistry_OwnerReplace(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo", Label: "one"}, "runtime")
	if err := r.Add("ext-a", CommandSpec{Name: "todo", Label: "two"}, "runtime"); err != nil {
		t.Fatalf("same-owner replace: %v", err)
	}
	entries := r.List()
	if len(entries) != 1 || entries[0].Spec.Label != "two" {
		t.Fatalf("list = %+v", entries)
	}
}

func TestCommandRegistry_RemoveAllByOwner(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")
	_ = r.Add("ext-a", CommandSpec{Name: "plan"}, "runtime")
	_ = r.Add("ext-b", CommandSpec{Name: "note"}, "runtime")
	r.RemoveAllByOwner("ext-a")
	entries := r.List()
	if len(entries) != 1 || entries[0].Spec.Name != "note" {
		t.Fatalf("after RemoveAllByOwner: %+v", entries)
	}
}

func TestCommandRegistry_RemoveOwnership(t *testing.T) {
	r := NewCommandRegistry()
	_ = r.Add("ext-a", CommandSpec{Name: "todo"}, "runtime")
	if err := r.Remove("ext-b", "todo"); err == nil {
		t.Fatalf("other-owner Remove should error")
	}
	if err := r.Remove("ext-a", "todo"); err != nil {
		t.Fatalf("owner Remove: %v", err)
	}
	if len(r.List()) != 0 {
		t.Fatalf("should be empty")
	}
	if err := r.Remove("ext-a", "todo"); err != nil {
		t.Fatalf("idempotent remove: %v", err)
	}
}
