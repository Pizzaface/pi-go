package api

import (
	"errors"
	"testing"
)

func TestSigilRegistry_AddMulti(t *testing.T) {
	r := NewSigilRegistry()
	if err := r.Add("ext-a", []string{"todo", "plan"}); err != nil {
		t.Fatal(err)
	}
	if o, ok := r.Owner("todo"); !ok || o != "ext-a" {
		t.Fatalf("owner todo = %q %v", o, ok)
	}
	if o, ok := r.Owner("plan"); !ok || o != "ext-a" {
		t.Fatalf("owner plan = %q %v", o, ok)
	}
}

func TestSigilRegistry_Collision(t *testing.T) {
	r := NewSigilRegistry()
	_ = r.Add("ext-a", []string{"todo"})
	err := r.Add("ext-b", []string{"plan", "todo"})
	var ce *SigilPrefixCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("want SigilPrefixCollisionError, got %v", err)
	}
	if ce.Prefix != "todo" {
		t.Fatalf("collision prefix = %q", ce.Prefix)
	}
	if _, ok := r.Owner("plan"); ok {
		t.Fatalf("atomic add broken: plan registered despite collision")
	}
}

func TestSigilRegistry_InvalidPrefix(t *testing.T) {
	r := NewSigilRegistry()
	if err := r.Add("ext-a", []string{"Bad"}); err == nil {
		t.Fatalf("expected invalid-prefix error")
	}
}

func TestSigilRegistry_RemoveAndRemoveAllByOwner(t *testing.T) {
	r := NewSigilRegistry()
	_ = r.Add("ext-a", []string{"todo", "plan"})
	_ = r.Add("ext-b", []string{"note"})
	_ = r.Remove("ext-a", []string{"plan"})
	if _, ok := r.Owner("plan"); ok {
		t.Fatalf("plan should be removed")
	}
	r.RemoveAllByOwner("ext-a")
	if _, ok := r.Owner("todo"); ok {
		t.Fatalf("todo should be gone")
	}
	if _, ok := r.Owner("note"); !ok {
		t.Fatalf("note should survive")
	}
}
