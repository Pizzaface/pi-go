package host

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildChildEnv_InjectsGoWorkOffForHostedGo(t *testing.T) {
	reg := &Registration{ID: "h", Mode: "hosted-go"}
	env := buildChildEnv(reg, []string{"go", "run", "."}, []string{"PATH=/usr/bin"})
	if !hasEnv(env, "GOWORK=off") {
		t.Fatalf("expected GOWORK=off in child env; got %v", env)
	}
}

func TestBuildChildEnv_RespectsExistingGOWORK(t *testing.T) {
	reg := &Registration{ID: "h", Mode: "hosted-go"}
	env := buildChildEnv(reg, []string{"go", "run", "."}, []string{"GOWORK=/custom/go.work"})
	if hasEnv(env, "GOWORK=off") {
		t.Fatalf("did not expect override of user-set GOWORK; got %v", env)
	}
	if !hasEnv(env, "GOWORK=/custom/go.work") {
		t.Fatalf("expected user GOWORK preserved; got %v", env)
	}
}

func TestBuildChildEnv_SkipsForNonGoCommand(t *testing.T) {
	reg := &Registration{ID: "h", Mode: "hosted-go"}
	env := buildChildEnv(reg, []string{"/opt/ext/prebuilt", "--flag"}, []string{"PATH=/usr/bin"})
	if hasEnv(env, "GOWORK=off") {
		t.Fatalf("GOWORK=off should only be injected for go-toolchain invocations; got %v", env)
	}
}

func TestBuildChildEnv_SkipsForHostedTS(t *testing.T) {
	reg := &Registration{ID: "h", Mode: "hosted-ts"}
	env := buildChildEnv(reg, []string{"node", "host.js"}, []string{"PATH=/usr/bin"})
	if hasEnv(env, "GOWORK=off") {
		t.Fatalf("GOWORK=off should not be injected for hosted-ts; got %v", env)
	}
}

func TestBuildChildEnv_StripsGOROOTForHostedGo(t *testing.T) {
	reg := &Registration{ID: "h", Mode: "hosted-go"}
	env := buildChildEnv(reg,
		[]string{"go", "run", "."},
		[]string{"PATH=/usr/bin", "GOROOT=/opt/go1.26", "GOTOOLCHAIN=go1.26.1", "HOME=/root"},
	)
	for _, e := range env {
		if strings.HasPrefix(e, "GOROOT=") {
			t.Fatalf("GOROOT should be stripped; got %q", e)
		}
		if strings.HasPrefix(e, "GOTOOLCHAIN=") {
			t.Fatalf("GOTOOLCHAIN should be stripped; got %q", e)
		}
	}
	if !hasEnv(env, "PATH=/usr/bin") || !hasEnv(env, "HOME=/root") {
		t.Fatalf("unrelated env vars should be preserved; got %v", env)
	}
}

func TestBuildChildEnv_KeepsGOROOTForNonGo(t *testing.T) {
	reg := &Registration{ID: "h", Mode: "hosted-go"}
	env := buildChildEnv(reg,
		[]string{"/opt/ext/prebuilt"},
		[]string{"PATH=/usr/bin", "GOROOT=/opt/go1.26"},
	)
	if !hasEnv(env, "GOROOT=/opt/go1.26") {
		t.Fatalf("GOROOT should be preserved for non-go commands; got %v", env)
	}
}

func TestStderrRing_KeepsTail(t *testing.T) {
	r := newStderrRing(16)
	for range 5 {
		_, _ = r.Write([]byte("abcdefgh"))
	}
	got := r.String()
	if len(got) > 16 {
		t.Fatalf("ring exceeded max: len=%d contents=%q", len(got), got)
	}
	if !strings.HasSuffix("abcdefghabcdefghabcdefghabcdefghabcdefgh", got) {
		t.Fatalf("expected tail of stream; got %q", got)
	}
}

func hasEnv(env []string, want string) bool {
	return slices.Contains(env, want)
}
