package api

import (
	"encoding/json"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

func TestHostedToolset_SnapshotsRegistry(t *testing.T) {
	r := NewHostedToolRegistry()
	desc := piapi.ToolDescriptor{
		Name:        "greet",
		Description: "x",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
	reg := &host.Registration{ID: "ext-a", Trust: host.TrustThirdParty}
	if err := r.Add("ext-a", desc, reg, nil); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ts := NewHostedToolset(r)
	if ts.Name() != "go-pi-hosted-extensions" {
		t.Fatalf("Name: %q", ts.Name())
	}
	// With the Task 4 stub adapter returning (nil,nil), Tools filters it out.
	// Task 5 will replace this expectation with len == 1.
	got, err := ts.Tools(nil)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if got == nil {
		t.Fatal("Tools returned nil slice; expected non-nil empty")
	}
}
