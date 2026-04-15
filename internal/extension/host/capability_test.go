package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGate_CompiledInAlwaysAllowed(t *testing.T) {
	g, err := NewGate(filepath.Join(t.TempDir(), "approvals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Allowed("anything", "tools.register", TrustCompiledIn); !ok {
		t.Fatalf("expected compiled-in to be allowed")
	}
	grants := g.Grants("anything", TrustCompiledIn)
	if len(grants) != 1 || grants[0] != StarAll {
		t.Fatalf("expected %q sentinel for compiled-in; got %v", StarAll, grants)
	}
}

func TestGate_ThirdPartyGrantedAndDenied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.json")
	body := `{
      "version": 2,
      "extensions": {
        "hello": {
          "trust_class": "third-party",
          "approved": true,
          "granted_capabilities": ["tools.register", "events.tool_execute"],
          "denied_capabilities": ["events.session_start"]
        }
      }
    }`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := NewGate(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Allowed("hello", "tools.register", TrustThirdParty); !ok {
		t.Fatalf("expected tools.register granted")
	}
	if ok, reason := g.Allowed("hello", "events.session_start", TrustThirdParty); ok {
		t.Fatalf("expected events.session_start denied; got allowed (%s)", reason)
	}
	if ok, reason := g.Allowed("ghost", "tools.register", TrustThirdParty); ok {
		t.Fatalf("unknown extension should not be allowed; got %s", reason)
	}
	grants := g.Grants("hello", TrustThirdParty)
	if len(grants) != 2 {
		t.Fatalf("expected 2 grants; got %v", grants)
	}
}

func TestGate_MissingFileStartsEmpty(t *testing.T) {
	g, err := NewGate(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if ok, _ := g.Allowed("x", "tools.register", TrustThirdParty); ok {
		t.Fatalf("empty gate should deny third-party")
	}
}
