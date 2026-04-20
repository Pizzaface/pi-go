package lifecycle

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/host"
)

// StartApproved launches every hosted extension in StateReady in
// parallel. Returns immediately; per-extension outcomes arrive via
// Subscribe. The returned slice is empty today (kept typed as []error
// so a future synchronous caller can collect).
func (s *service) StartApproved(ctx context.Context) []error {
	for _, reg := range s.mgr.List() {
		if reg.Trust == host.TrustCompiledIn {
			continue
		}
		if reg.State != host.StateReady {
			continue
		}
		if reg.Mode != "hosted-go" && reg.Mode != "hosted-ts" {
			continue
		}
		id := reg.ID
		go func() {
			if err := s.Start(ctx, id); err != nil {
				_ = err
			}
		}()
	}
	return nil
}

// StopAll fires the shutdown hook (if any), then calls Stop on every running
// hosted registration in parallel, bounded by a 3s per-extension wait. Returns
// collected errors.
func (s *service) StopAll(ctx context.Context) []error {
	if s.shutdownHook != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = s.shutdownHook(shutdownCtx, "shutdown", map[string]any{
			"reason": s.stopReason,
		})
		cancel()
	}

	regs := s.mgr.List()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, reg := range regs {
		if reg.Trust == host.TrustCompiledIn {
			continue
		}
		if reg.State != host.StateRunning {
			continue
		}
		id := reg.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := s.Stop(c, id); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errs
}

// hostBundleVersion is the cache key used when extracting the embedded
// Node host bundle. Hard-coded for now; replace with a project version
// string when one is exposed.
var hostBundleVersion = "stable"

// buildCommand returns argv for launching reg.
func (s *service) buildCommand(reg *host.Registration) ([]string, error) {
	switch reg.Mode {
	case "hosted-go":
		if len(reg.Metadata.Command) > 0 {
			return append([]string(nil), reg.Metadata.Command...), nil
		}
		return []string{"go", "run", "."}, nil
	case "hosted-ts":
		if _, err := exec.LookPath("node"); err != nil {
			return nil, fmt.Errorf("node not on PATH: %w", err)
		}
		hostPath, err := host.ExtractedHostPath(hostBundleVersion)
		if err != nil {
			return nil, fmt.Errorf("extract host bundle: %w", err)
		}
		entry := reg.Metadata.Entry
		if entry == "" {
			entry = "src/index.ts"
		}
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(reg.WorkDir, entry)
		}
		return []string{"node", hostPath, "--entry", entry, "--name", reg.ID}, nil
	default:
		return nil, fmt.Errorf("unsupported mode %q", reg.Mode)
	}
}
