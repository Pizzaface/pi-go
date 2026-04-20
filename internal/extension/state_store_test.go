package extension

import "testing"

func TestStateStore_PersistsByExtensionAndSession(t *testing.T) {
	sessionsDir := t.TempDir()

	storeA := NewStateStore(sessionsDir, "session-a")
	storeB := NewStateStore(sessionsDir, "session-b")

	if err := storeA.Namespace("ext.demo").Set(map[string]any{"value": "a"}); err != nil {
		t.Fatal(err)
	}
	if err := storeB.Namespace("ext.demo").Set(map[string]any{"value": "b"}); err != nil {
		t.Fatal(err)
	}

	gotA, ok, err := storeA.Namespace("ext.demo").Get()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || gotA["value"] != "a" {
		t.Fatalf("expected session-a state, got ok=%v value=%+v", ok, gotA)
	}

	gotB, ok, err := storeB.Namespace("ext.demo").Get()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || gotB["value"] != "b" {
		t.Fatalf("expected session-b state, got ok=%v value=%+v", ok, gotB)
	}
}

func TestStateStore_SerializesJSONValues(t *testing.T) {
	store := NewStateStore(t.TempDir(), "session-json")
	value := map[string]any{
		"count": 3,
		"ok":    true,
		"items": []any{"alpha", "beta"},
	}
	if err := store.Namespace("ext.json").Set(value); err != nil {
		t.Fatal(err)
	}

	got, ok, err := store.Namespace("ext.json").Get()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected stored JSON state to exist")
	}
	if got["count"] != float64(3) {
		t.Fatalf("expected count=3, got %+v", got["count"])
	}
	if got["ok"] != true {
		t.Fatalf("expected ok=true, got %+v", got["ok"])
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected items array roundtrip, got %+v", got["items"])
	}
}

func TestStateNamespace_Patch_MergesShallow(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	if err := ns.Set(map[string]any{"a": 1.0, "b": 2.0}); err != nil {
		t.Fatal(err)
	}
	if err := ns.Patch([]byte(`{"b": 99, "c": 3}`)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ns.Get()
	if err != nil || !ok {
		t.Fatalf("Get: err=%v ok=%v", err, ok)
	}
	if got["a"] != 1.0 || got["b"] != 99.0 || got["c"] != 3.0 {
		t.Fatalf("merged state = %v", got)
	}
}

func TestStateNamespace_Patch_NullDeletesKey(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	_ = ns.Set(map[string]any{"a": 1.0, "b": 2.0})
	if err := ns.Patch([]byte(`{"b": null}`)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ns.Get()
	if _, has := got["b"]; has {
		t.Fatalf("key b should be deleted: %v", got)
	}
}

func TestStateNamespace_Patch_ArrayReplaces(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	_ = ns.Set(map[string]any{"items": []any{"x", "y"}})
	if err := ns.Patch([]byte(`{"items": ["z"]}`)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ns.Get()
	arr, ok := got["items"].([]any)
	if !ok || len(arr) != 1 || arr[0] != "z" {
		t.Fatalf("items = %v", got["items"])
	}
}

func TestStateNamespace_Patch_RecursesNested(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	_ = ns.Set(map[string]any{"outer": map[string]any{"a": 1.0, "b": 2.0}})
	if err := ns.Patch([]byte(`{"outer": {"b": 99, "c": 3}}`)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := ns.Get()
	outer := got["outer"].(map[string]any)
	if outer["a"] != 1.0 || outer["b"] != 99.0 || outer["c"] != 3.0 {
		t.Fatalf("nested merge: %v", outer)
	}
}

func TestStateNamespace_Patch_EmptyBaseCreates(t *testing.T) {
	dir := t.TempDir()
	s := NewStateStore(dir, "sess-1")
	ns := s.Namespace("ext-a")
	if err := ns.Patch([]byte(`{"a": 1}`)); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := ns.Get()
	if !ok || got["a"] != 1.0 {
		t.Fatalf("patch on empty: ok=%v got=%v", ok, got)
	}
}

