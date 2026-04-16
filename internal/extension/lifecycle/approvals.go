package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type approvalsFile struct {
	Version    int                        `json:"version"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

func readApprovals(path string) (approvalsFile, error) {
	out := approvalsFile{Extensions: map[string]json.RawMessage{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			out.Version = 2
			return out, nil
		}
		return out, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		out.Version = 2
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse %s: %w", path, err)
	}
	if out.Extensions == nil {
		out.Extensions = map[string]json.RawMessage{}
	}
	if out.Version == 0 {
		out.Version = 2
	}
	return out, nil
}

func atomicWrite(path string, file approvalsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "approvals-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close tmp: %w", err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

func mutateApprovals(path, id string, op func(entry map[string]any) map[string]any) error {
	file, err := readApprovals(path)
	if err != nil {
		return err
	}
	var entry map[string]any
	if raw, ok := file.Extensions[id]; ok {
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("decode entry %s: %w", id, err)
		}
	}
	updated := op(entry)
	if updated == nil {
		delete(file.Extensions, id)
	} else {
		raw, err := json.Marshal(updated)
		if err != nil {
			return fmt.Errorf("encode entry %s: %w", id, err)
		}
		file.Extensions[id] = raw
	}
	return atomicWrite(path, file)
}
