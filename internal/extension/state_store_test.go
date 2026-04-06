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
