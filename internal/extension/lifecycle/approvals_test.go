package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadApprovals_MissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	got, err := readApprovals(path)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Version == 0 {
		got.Version = 2
	}
	if len(got.Extensions) != 0 {
		t.Fatalf("expected empty map; got %d", len(got.Extensions))
	}
}

func TestAtomicWrite_PreservesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	initial := `{"version":2,"extensions":{"ext-a":{"approved":true,"hash":"abc123"}}}`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	file, err := readApprovals(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, file); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	exts := back["extensions"].(map[string]any)
	entry := exts["ext-a"].(map[string]any)
	if entry["hash"] != "abc123" {
		t.Fatalf("lost unknown field: %#v", entry)
	}
	tmpGlob, _ := filepath.Glob(path + ".tmp*")
	if len(tmpGlob) != 0 {
		t.Fatalf("expected no .tmp droppings; got %v", tmpGlob)
	}
}

func TestMutateApprovals_ApproveNewEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	err := mutateApprovals(path, "ext-a", func(entry map[string]any) map[string]any {
		if entry == nil {
			entry = map[string]any{}
		}
		entry["approved"] = true
		entry["granted_capabilities"] = []any{"tools.register"}
		return entry
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := readApprovals(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Extensions["ext-a"]; !ok {
		t.Fatal("expected ext-a entry")
	}
}

func TestMutateApprovals_DeleteEntryOnNilReturn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	seed := `{"version":2,"extensions":{"ext-a":{"approved":true},"ext-b":{"approved":false}}}`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	err := mutateApprovals(path, "ext-a", func(map[string]any) map[string]any { return nil })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := readApprovals(path)
	if _, ok := got.Extensions["ext-a"]; ok {
		t.Fatal("expected ext-a deleted")
	}
	if _, ok := got.Extensions["ext-b"]; !ok {
		t.Fatal("ext-b should survive")
	}
}

func TestMutateApprovals_MalformedFileFailsBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	err := mutateApprovals(path, "ext-a", func(e map[string]any) map[string]any { return e })
	if err == nil {
		t.Fatal("expected parse error")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file mutated despite parse failure")
	}
}
