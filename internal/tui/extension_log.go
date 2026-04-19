package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	logFileSizeCap = 10 * 1024 * 1024 // 10 MB
	logKeepCount   = 3
)

type extensionLogFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func newExtensionLogFile(path string) *extensionLogFile {
	return &extensionLogFile{path: path}
}

func (l *extensionLogFile) Write(extID, level, msg string, fields map[string]any, ts time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.path == "" {
		return nil // nil-path = silent (used in tests pre-Task-16)
	}

	if err := l.ensureOpen(); err != nil {
		return err
	}
	if err := l.rotateIfNeeded(); err != nil {
		return err
	}

	msg = truncateRunes(msg, 8*1024, "")
	entry := map[string]any{
		"ts":    ts.UTC().Format(time.RFC3339Nano),
		"ext":   extID,
		"level": level,
		"msg":   msg,
	}
	if fields != nil {
		entry["fields"] = truncateDepth(fields, 6)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(l.f, string(data))
	return err
}

func (l *extensionLogFile) ensureOpen() error {
	if l.f != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	l.f = f
	return nil
}

func (l *extensionLogFile) rotateIfNeeded() error {
	info, err := l.f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < logFileSizeCap {
		return nil
	}
	_ = l.f.Close()
	l.f = nil

	for i := logKeepCount; i >= 1; i-- {
		from := l.path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", l.path, i-1)
		}
		to := fmt.Sprintf("%s.%d", l.path, i)
		_ = os.Remove(to)
		_ = os.Rename(from, to)
	}

	return l.ensureOpen()
}

func truncateDepth(v any, depth int) any {
	if depth == 0 {
		return "..."
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = truncateDepth(vv, depth-1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = truncateDepth(vv, depth-1)
		}
		return out
	default:
		return v
	}
}
