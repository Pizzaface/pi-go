package hostruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

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
		cancel()
		return nil, fmt.Errorf("opening process stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
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

	// Close stdin first — a well-behaved extension sees EOF on its
	// decoder and exits cleanly. Works on every platform.
	if p.stdin != nil {
		_ = p.stdin.Close()
	}

	// Best-effort interrupt. No-op on Windows (os.Interrupt is unsupported
	// for child processes) but clean on Unix.
	if runtime.GOOS != "windows" && p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(os.Interrupt)
	}

	done := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(done)
	}()

	select {
	case <-done:
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		return nil
	case <-ctx.Done():
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-done
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		return ctx.Err()
	}
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
