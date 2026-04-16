package lifecycle

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestBuildCommand_HostedGoUsesMetadataCommand(t *testing.T) {
	s := &service{}
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Metadata: piapi.Metadata{Command: []string{"go", "run", "."}}}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd) != 3 || cmd[0] != "go" || cmd[2] != "." {
		t.Fatalf("unexpected cmd: %v", cmd)
	}
}

func TestBuildCommand_HostedGoFallsBackToGoRunDot(t *testing.T) {
	s := &service{}
	reg := &host.Registration{ID: "h", Mode: "hosted-go", Metadata: piapi.Metadata{}}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		t.Fatal(err)
	}
	if cmd[0] != "go" || cmd[1] != "run" || cmd[2] != "." {
		t.Fatalf("unexpected fallback: %v", cmd)
	}
}

func TestBuildCommand_HostedTSWhenNodePresent(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH; skipping success-branch assertion")
	}
	s := &service{}
	reg := &host.Registration{ID: "h", Mode: "hosted-ts", WorkDir: t.TempDir(), Metadata: piapi.Metadata{Entry: "src/index.ts"}}
	cmd, err := s.buildCommand(reg)
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}
	if cmd[0] != "node" || !strings.Contains(cmd[1], "host.bundle.js") {
		t.Fatalf("unexpected cmd: %v", cmd)
	}
	if !filepath.IsAbs(cmd[3]) {
		t.Fatalf("expected absolute entry path; got %q", cmd[3])
	}
}
