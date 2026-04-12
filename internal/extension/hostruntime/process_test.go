package hostruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestHostedProcess_CleanShutdown(t *testing.T) {
	command, args := longRunningCommand()
	process, err := StartProcess(context.Background(), ProcessConfig{
		Command: command,
		Args:    args,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = process.Shutdown(ctx)
	// On Windows the long-running command (powershell sleep) does not
	// exit on stdin close and os.Interrupt is a no-op, so Shutdown
	// falls through to Kill after the context deadline — that is the
	// expected behaviour now.
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("expected nil or deadline exceeded, got %v", err)
	}
}

func longRunningCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 60"}
	}
	return "sh", []string{"-c", "sleep 60"}
}

func TestProcessShutdown_ClosesStdinFirst(t *testing.T) {
	// A Go program that blocks reading stdin, exits cleanly on EOF.
	script := `package main
import ("bufio"; "os")
func main() { bufio.NewReader(os.Stdin).ReadByte(); os.Exit(0) }`
	bin := buildTestBinary(t, script)

	p, err := StartProcess(context.Background(), ProcessConfig{Command: bin})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown took %v, expected <500ms (stdin close should cause clean exit)", elapsed)
	}
}

func TestProcessShutdown_KillsOnTimeout(t *testing.T) {
	// A program that ignores EOF on stdin and sleeps forever — tests the
	// Kill-on-timeout branch.
	script := `package main
import ("os/signal"; "syscall"; "time")
func main() {
	signal.Ignore(syscall.SIGINT)
	for { time.Sleep(time.Hour) }
}`
	bin := buildTestBinary(t, script)

	p, err := StartProcess(context.Background(), ProcessConfig{Command: bin})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = p.Shutdown(ctx)
	elapsed := time.Since(start)
	if elapsed > 800*time.Millisecond {
		t.Fatalf("shutdown did not respect timeout budget: %v", elapsed)
	}
	if err == nil {
		t.Fatal("expected ctx.Err() when kill path is taken")
	}
}

func TestProcessShutdown_IdempotentAfterExit(t *testing.T) {
	script := `package main
func main() {}`
	bin := buildTestBinary(t, script)

	p, err := StartProcess(context.Background(), ProcessConfig{Command: bin})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for natural exit.
	_ = p.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown on exited process: %v", err)
	}
}

// buildTestBinary compiles a tiny Go program to a temp binary for use in tests.
func buildTestBinary(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binName := "bin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dir, binName)
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	return binPath
}
