package extension

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissions_HostedThirdPartyCannotUseInterceptByDefault(t *testing.T) {
	p := EmptyPermissions()
	allowed := p.AllowsCapability("ext.demo", TrustClassHostedThirdParty, CapabilityToolIntercept)
	if allowed {
		t.Fatal("expected hosted third-party intercept to be denied by default")
	}
}

func TestPermissions_LoadsApprovalsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.json")
	if err := SavePermissions(path, NewPermissions([]ApprovalRecord{{
		ExtensionID:         "ext.demo",
		TrustClass:          TrustClassHostedThirdParty,
		GrantedCapabilities: []Capability{CapabilityToolRegister},
		HostedRequired:      true,
	}})); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPermissions(path)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := loaded.Approval("ext.demo")
	if !ok {
		t.Fatal("expected approvals file to include ext.demo")
	}
	if !record.HostedRequired {
		t.Fatal("expected hosted_required=true from approvals")
	}
	if len(record.GrantedCapabilities) != 1 || record.GrantedCapabilities[0] != CapabilityToolRegister {
		t.Fatalf("unexpected granted capabilities: %+v", record.GrantedCapabilities)
	}
}

func TestManager_RejectsUnapprovedHostedCapability(t *testing.T) {
	m := NewManager(ManagerOptions{
		Permissions: NewPermissions([]ApprovalRecord{{
			ExtensionID:    "ext.hosted",
			TrustClass:     TrustClassHostedThirdParty,
			HostedRequired: true,
		}}),
	})

	err := m.RegisterManifest(Manifest{
		Name: "ext.hosted",
		Runtime: RuntimeSpec{
			Type:    RuntimeTypeHostedStdioJSONRPC,
			Command: "demo-host",
		},
		Capabilities: []Capability{CapabilityToolIntercept},
	})
	if err == nil {
		t.Fatal("expected unapproved hosted capability to be rejected")
	}
	if !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("expected approval error, got %v", err)
	}
}

func TestPermissions_AllowsService_HostedThirdPartyRequiresGrant(t *testing.T) {
	p := NewPermissions([]ApprovalRecord{
		{
			ExtensionID:         "ext.demo",
			TrustClass:          TrustClassHostedThirdParty,
			GrantedCapabilities: []Capability{CapabilityUIStatus, CapabilityCommandRegister},
		},
	})
	if !p.AllowsService("ext.demo", TrustClassHostedThirdParty, "ui", "status") {
		t.Error("expected ui.status to be allowed (ui.status granted)")
	}
	if !p.AllowsService("ext.demo", TrustClassHostedThirdParty, "commands", "register") {
		t.Error("expected commands.register to be allowed")
	}
	if p.AllowsService("ext.demo", TrustClassHostedThirdParty, "sigils", "register") {
		t.Error("expected sigils.register to be denied (not in grant list)")
	}
}

func TestPermissions_AllowsService_DeclarativeTrustedByDefault(t *testing.T) {
	p := EmptyPermissions()
	if !p.AllowsService("ext.builtin", TrustClassDeclarative, "ui", "status") {
		t.Error("declarative extensions should be trusted by default")
	}
	if !p.AllowsService("ext.builtin", TrustClassCompiledIn, "commands", "register") {
		t.Error("compiled-in extensions should be trusted by default")
	}
}

func TestPermissions_AllowsService_UnknownServiceDenied(t *testing.T) {
	p := NewPermissions([]ApprovalRecord{
		{
			ExtensionID:         "ext.demo",
			TrustClass:          TrustClassHostedThirdParty,
			GrantedCapabilities: []Capability{CapabilityUIStatus},
		},
	})
	if p.AllowsService("ext.demo", TrustClassHostedThirdParty, "mystery", "do") {
		t.Error("unknown service should be denied for hosted extensions")
	}
}

func TestPermissions_AllowsService_GetMethodsNoGate(t *testing.T) {
	// agent.get_mode has no capability gate — always allowed.
	p := EmptyPermissions()
	if !p.AllowsService("ext.demo", TrustClassHostedThirdParty, "agent", "get_mode") {
		t.Error("agent.get_mode should be allowed without a grant")
	}
}
