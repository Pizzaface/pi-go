package hostruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// shutdownGracePeriod is the maximum time Shutdown waits for a child
// to exit gracefully (via stdin EOF + SIGINT on Unix) before killing
// it. It exists so a Shutdown called with context.Background() or a
// long-timeout context still terminates promptly — critical on
// Windows where os.Interrupt is a no-op.
var shutdownGracePeriod = 2 * time.Second

type ProcessConfig struct {
	Command string
	Args    []string
	Env     map[string]string
	WorkDir string
}

type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer

	// cancel cancels the exec.CommandContext ctx, which causes the
	// go runtime to send a kill signal to the subprocess. On Unix
	// this is SIGKILL (which kills the process but not its children);
	// on Windows it's TerminateProcess (same caveat). For shell-style
	// wrappers like `go run .` this may still leave the compiled
	// binary running — that's acceptable for shutdown since we only
	// need the parent pipe handles closed so cmd.Wait() returns.
	cancel context.CancelFunc

	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

func StartProcess(ctx context.Context, cfg ProcessConfig) (*Process, error) {
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("process command is required")
	}
	// Wrap the caller's ctx in our own so Shutdown can cancel the
	// exec-owned kill signal even when the caller's ctx is
	// context.Background().
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, command, cfg.Args...)
	if strings.TrimSpace(cfg.WorkDir) != "" {
		cmd.Dir = cfg.WorkDir
	}
	cmd.Env = mergeEnv(os.Environ(), cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening process stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening process stdout: %w", err)
	}

	p := &Process{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		cancel:   cancel,
		waitDone: make(chan struct{}),
	}
	cmd.Stderr = &p.stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting process %q: %w", command, err)
	}
	go p.waitLoop()
	return p, nil
}

func (p *Process) waitLoop() {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		close(p.waitDone)
	})
}

func (p *Process) Stdout() io.ReadCloser {
	return p.stdout
}

func (p *Process) Stdin() io.WriteCloser {
	return p.stdin
}

func (p *Process) Stderr() string {
	return p.stderr.String()
}

func (p *Process) Wait() error {
	<-p.waitDone
	return p.waitErr
}

func (p *Process) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	select {
	case <-p.waitDone:
		return nil
	default:
	}

	// Close stdin first. stdio-based child processes (Claude CLI,
	// the hosted-hello SDK, etc.) exit cleanly on stdin EOF, and
	// this is the only shutdown mechanism that works cross-platform:
	// Go's os.Process.Signal(os.Interrupt) returns
	// "not supported by windows" on Windows and is a silent no-op,
	// which previously caused Shutdown to block forever.
	if p.stdin != nil {
		_ = p.stdin.Close()
	}

	// On Unix, also send SIGINT in case the child ignores stdin EOF.
	// Windows doesn't support os.Interrupt so this is a no-op there,
	// and we fall back to Kill() below if the child doesn't exit.
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}

	done := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(done)
	}()

	// Give the child a bounded grace period to exit. This guarantees
	// Shutdown terminates even when the caller passed a never-canceling
	// context (e.g. context.Background()) — critical on Windows where
	// stdin close + os.Interrupt don't affect processes like
	// `go run .` that buffer stdin until their child compiles.
	graceTimer := time.NewTimer(shutdownGracePeriod)
	defer graceTimer.Stop()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Caller ran out of patience first.
	case <-graceTimer.C:
		// Grace period elapsed; caller hasn't canceled yet.
	}

	// Cancel the exec context so Go's exec package runs its own
	// kill-and-reap path, then Kill directly as belt-and-suspenders.
	if p.cancel != nil {
		p.cancel()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}

	// Bounded wait after Kill. On Windows with process-tree parents
	// like `go run .`, the grandchild can keep the stdout/stderr
	// pipes open indefinitely, so cmd.Wait() (which waits for I/O
	// copy goroutines) may never return. We accept orphaning those
	// goroutines rather than blocking Shutdown forever.
	killTimer := time.NewTimer(shutdownGracePeriod)
	defer killTimer.Stop()
	select {
	case <-done:
	case <-killTimer.C:
	}
	return nil
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(extra))
	for _, kv := range base {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		merged[parts[0]] = parts[1]
	}
	for key, value := range extra {
		merged[key] = value
	}
	out := make([]string, 0, len(merged))
	for key, value := range merged {
		out = append(out, key+"="+value)
	}
	return out
}
