package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtensionLogFile_RotatesAtSizeCap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ext.log")
	l := newExtensionLogFile(logPath)
	t.Cleanup(func() {
		l.mu.Lock()
		if l.f != nil {
			_ = l.f.Close()
			l.f = nil
		}
		l.mu.Unlock()
	})

	// Write enough data to exceed the 10 MB cap.
	// Each message is 1024 bytes; 12*1024 messages = ~12 MB.
	payload := strings.Repeat("x", 1024)
	ts := time.Now()
	for i := 0; i < 12*1024; i++ {
		if err := l.Write("ext1", "info", payload, nil, ts); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// After rotation, ext.log.1 must exist.
	rotated := logPath + ".1"
	if _, err := os.Stat(rotated); err != nil {
		t.Fatalf("rotated file %s not found: %v", rotated, err)
	}
	// The current log file must also exist (reopened after rotation).
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("current log file %s not found after rotation: %v", logPath, err)
	}
}

func TestExtensionLogFile_TruncatesDeepFields(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ext.log")
	l := newExtensionLogFile(logPath)

	// Build a deeply-nested map (depth 10, well beyond the 6-level cap).
	nested := map[string]any{"k": "leaf"}
	for i := 0; i < 9; i++ {
		nested = map[string]any{"inner": nested}
	}

	ts := time.Now()
	if err := l.Write("ext1", "debug", "deep field test", nested, ts); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Close the file before reading.
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("no line in log file")
	}
	var entry map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Walk the fields tree and confirm depth is capped (no deeper than 6 levels).
	maxDepth := func(v any) int {
		var walk func(any, int) int
		walk = func(v any, d int) int {
			switch t := v.(type) {
			case map[string]any:
				max := d
				for _, vv := range t {
					if sub := walk(vv, d+1); sub > max {
						max = sub
					}
				}
				return max
			case []any:
				max := d
				for _, vv := range t {
					if sub := walk(vv, d+1); sub > max {
						max = sub
					}
				}
				return max
			default:
				return d
			}
		}
		return walk(v, 0)
	}

	fields, ok := entry["fields"]
	if !ok {
		t.Fatal("fields key missing from log entry")
	}
	depth := maxDepth(fields)
	if depth > 7 { // 6-level cap produces at most depth 7 (root map + 6 nested)
		t.Fatalf("fields depth = %d; want <= 7 (cap is 6 levels)", depth)
	}
}
