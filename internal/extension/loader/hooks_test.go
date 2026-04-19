package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseHooks_Valid(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "session_start"
command = "ext_announce"
tools = ["*"]
timeout = 5000

[[hooks]]
event = "before_turn"
command = "ext_inject"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	md, cmd, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err != nil {
		t.Fatalf("parsePiToml: %v", err)
	}
	if md.Name != "ext" {
		t.Fatalf("name = %s; want ext", md.Name)
	}
	if len(cmd) == 0 {
		t.Fatalf("command returned empty")
	}
}

func TestParseHooks_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "session_start"
command = "ext_announce"

[[hooks]]
event = "before_turn"
command = "ext_inject"
tools = ["*"]
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	md, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err != nil {
		t.Fatalf("parsePiToml: %v", err)
	}

	if md.Name != "ext" {
		t.Fatalf("name = %s; want ext", md.Name)
	}

	// The first hook omits tools and timeout — defaults must be applied.
	if len(md.Hooks) != 2 {
		t.Fatalf("Hooks len = %d; want 2", len(md.Hooks))
	}
	if got := md.Hooks[0].Tools; len(got) != 1 || got[0] != "*" {
		t.Fatalf("Hooks[0].Tools = %v; want [\"*\"]", got)
	}
	if got := md.Hooks[0].Timeout; got != 5000 {
		t.Fatalf("Hooks[0].Timeout = %d; want 5000", got)
	}
}

func TestParseHooks_InvalidEventRejected(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "made_up"
command = "x"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err == nil {
		t.Fatal("expected error for unknown event")
	}
	if errStr := err.Error(); errStr != "pi.toml [[hooks]][0]: unknown event \"made_up\"" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHooks_MissingEventRejected(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
command = "x"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err == nil {
		t.Fatal("expected error for missing event")
	}
	if errStr := err.Error(); errStr != "pi.toml [[hooks]][0]: event is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHooks_MissingCommandRejected(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "startup"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if errStr := err.Error(); errStr != "pi.toml [[hooks]][0]: command is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHooks_InvalidTimeoutRejected(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "startup"
command = "x"
timeout = 70000
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
	if errStr := err.Error(); errStr != "pi.toml [[hooks]][0]: timeout must be 1..60000 ms" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHooks_NegativeTimeoutRejected(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "startup"
command = "x"
timeout = -100
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
	if errStr := err.Error(); errStr != "pi.toml [[hooks]][0]: timeout must be 1..60000 ms" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHooks_MultipleHooksValidated(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "startup"
command = "x"

[[hooks]]
event = "bad_event"
command = "y"
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err == nil {
		t.Fatal("expected error for bad event in second hook")
	}
	if errStr := err.Error(); errStr != "pi.toml [[hooks]][1]: unknown event \"bad_event\"" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHooks_HooksSurfacedOnMetadata(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "session_start"
command = "ext_announce"

[[hooks]]
event = "after_turn"
command = "ext_summarise"
tools = ["read_file"]
timeout = 2000
`
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	md, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
	if err != nil {
		t.Fatalf("parsePiToml: %v", err)
	}
	if len(md.Hooks) != 2 {
		t.Fatalf("Hooks len = %d; want 2", len(md.Hooks))
	}
	if md.Hooks[0].Event != "session_start" {
		t.Fatalf("Hooks[0].Event = %q; want session_start", md.Hooks[0].Event)
	}
	if md.Hooks[1].Event != "after_turn" {
		t.Fatalf("Hooks[1].Event = %q; want after_turn", md.Hooks[1].Event)
	}
	// Second hook has explicit tools/timeout — confirm they are preserved.
	if got := md.Hooks[1].Tools; len(got) != 1 || got[0] != "read_file" {
		t.Fatalf("Hooks[1].Tools = %v; want [\"read_file\"]", got)
	}
	if md.Hooks[1].Timeout != 2000 {
		t.Fatalf("Hooks[1].Timeout = %d; want 2000", md.Hooks[1].Timeout)
	}
}

func TestParseHooks_AllValidEvents(t *testing.T) {
	events := []string{"startup", "session_start", "before_turn", "after_turn", "shutdown"}
	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			dir := t.TempDir()
			toml := `name = "ext"
version = "0.1"
description = "x"
runtime = "hosted"
command = ["go", "run", "."]

[[hooks]]
event = "` + event + `"
command = "test_cmd"
`
			if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(toml), 0o644); err != nil {
				t.Fatal(err)
			}
			_, _, err := parsePiToml(filepath.Join(dir, "pi.toml"))
			if err != nil {
				t.Fatalf("unexpected error for event %q: %v", event, err)
			}
		})
	}
}
