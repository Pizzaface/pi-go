package hostruntime

import (
	"context"
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
	if err := process.Shutdown(ctx); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}
}

func longRunningCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 60"}
	}
	return "sh", []string{"-c", "sleep 60"}
}
