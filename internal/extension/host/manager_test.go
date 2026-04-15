package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newTestManager(t *testing.T, approvalsBody string) *Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.json")
	if approvalsBody != "" {
		if err := os.WriteFile(path, []byte(approvalsBody), 0644); err != nil {
			t.Fatal(err)
		}
	}
	g, err := NewGate(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(g)
}

func TestManager_CompiledInReady(t *testing.T) {
	m := newTestManager(t, "")
	reg := &Registration{ID: "hello", Mode: "compiled-in", Trust: TrustCompiledIn}
	if err := m.Register(reg); err != nil {
		t.Fatal(err)
	}
	if reg.State != StateReady {
		t.Fatalf("expected Ready; got %s", reg.State)
	}
}

func TestManager_HostedWithoutApprovalPending(t *testing.T) {
	m := newTestManager(t, "")
	reg := &Registration{ID: "ext-a", Mode: "hosted-go", Trust: TrustThirdParty}
	if err := m.Register(reg); err != nil {
		t.Fatal(err)
	}
	if reg.State != StatePending {
		t.Fatalf("expected Pending; got %s", reg.State)
	}
}

func TestManager_HostedWithApprovalReady(t *testing.T) {
	body := `{
      "version": 2,
      "extensions": {
        "ext-b": {"approved": true, "granted_capabilities": ["tools.register"]}
      }
    }`
	m := newTestManager(t, body)
	reg := &Registration{ID: "ext-b", Mode: "hosted-go", Trust: TrustThirdParty}
	if err := m.Register(reg); err != nil {
		t.Fatal(err)
	}
	if reg.State != StateReady {
		t.Fatalf("expected Ready; got %s", reg.State)
	}
}

func TestManager_DuplicateRejected(t *testing.T) {
	m := newTestManager(t, "")
	reg := &Registration{ID: "x", Mode: "compiled-in", Trust: TrustCompiledIn}
	if err := m.Register(reg); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(&Registration{ID: "x", Mode: "compiled-in", Trust: TrustCompiledIn}); err == nil {
		t.Fatal("expected duplicate ID to error")
	}
}

func TestManager_Shutdown(t *testing.T) {
	m := newTestManager(t, "")
	reg := &Registration{ID: "z", Mode: "compiled-in", Trust: TrustCompiledIn}
	if err := m.Register(reg); err != nil {
		t.Fatal(err)
	}
	m.Shutdown(context.Background())
	if reg.State != StateStopped {
		t.Fatalf("expected Stopped; got %s", reg.State)
	}
}

func TestManager_ListSorted(t *testing.T) {
	m := newTestManager(t, "")
	for _, id := range []string{"zeta", "alpha", "mu"} {
		if err := m.Register(&Registration{ID: id, Mode: "compiled-in", Trust: TrustCompiledIn}); err != nil {
			t.Fatal(err)
		}
	}
	list := m.List()
	if len(list) != 3 || list[0].ID != "alpha" || list[1].ID != "mu" || list[2].ID != "zeta" {
		t.Fatalf("expected alpha,mu,zeta; got %+v", list)
	}
}
