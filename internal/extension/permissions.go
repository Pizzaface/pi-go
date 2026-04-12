package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type TrustClass string

const (
	TrustClassDeclarative      TrustClass = "declarative"
	TrustClassCompiledIn       TrustClass = "compiled_in"
	TrustClassHostedFirstParty TrustClass = "hosted_first_party"
	TrustClassHostedThirdParty TrustClass = "hosted_third_party"
)

type Capability string

const (
	CapabilityCommandRegister Capability = "commands.register"
	CapabilityToolRegister    Capability = "tools.register"
	CapabilityToolIntercept   Capability = "tools.intercept"
	CapabilityUIStatus        Capability = "ui.status"
	CapabilityUIWidget        Capability = "ui.widget"
	CapabilityUIDialog        Capability = "ui.dialog"
	CapabilityRenderText      Capability = "render.text"
	CapabilityRenderMarkdown  Capability = "render.markdown"

	// v2 capabilities — one per new service namespace.
	CapabilitySessionRead    Capability = "session.read"
	CapabilitySessionWrite   Capability = "session.write"
	CapabilityAgentMode      Capability = "agent.mode"
	CapabilityStateRead      Capability = "state.read"
	CapabilityStateWrite     Capability = "state.write"
	CapabilityChatAppend     Capability = "chat.append"
	CapabilitySigilsRegister Capability = "sigils.register"
	CapabilitySigilsResolve  Capability = "sigils.resolve"
	CapabilitySigilsAction   Capability = "sigils.action"
)

type ApprovalRecord struct {
	ExtensionID         string       `json:"extension_id"`
	TrustClass          TrustClass   `json:"trust_class,omitempty"`
	GrantedCapabilities []Capability `json:"granted_capabilities,omitempty"`
	HostedRequired      bool         `json:"hosted_required,omitempty"`
	ApprovedAt          time.Time    `json:"approved_at,omitempty"`
}

type approvalFile struct {
	Approvals []ApprovalRecord `json:"approvals"`
}

// Permissions stores persisted trust/capability approvals.
type Permissions struct {
	approvals map[string]ApprovalRecord
}

func NewPermissions(records []ApprovalRecord) *Permissions {
	approvals := make(map[string]ApprovalRecord, len(records))
	for _, record := range records {
		id := strings.TrimSpace(record.ExtensionID)
		if id == "" {
			continue
		}
		record.ExtensionID = id
		approvals[id] = record
	}
	return &Permissions{approvals: approvals}
}

func EmptyPermissions() *Permissions {
	return &Permissions{approvals: map[string]ApprovalRecord{}}
}

func DefaultApprovalsPath() string {
	home, err := discoverHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi-go", "extensions", "approvals.json")
}

func LoadPermissions(path string) (*Permissions, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return EmptyPermissions(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyPermissions(), nil
		}
		return nil, fmt.Errorf("reading extension approvals %s: %w", path, err)
	}
	var file approvalFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing extension approvals %s: %w", path, err)
	}
	return NewPermissions(file.Approvals), nil
}

func SavePermissions(path string, p *Permissions) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("approvals path is required")
	}
	if p == nil {
		p = EmptyPermissions()
	}
	records := make([]ApprovalRecord, 0, len(p.approvals))
	for _, record := range p.approvals {
		records = append(records, record)
	}
	payload := approvalFile{Approvals: records}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding approvals: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating approvals dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing approvals %s: %w", path, err)
	}
	return nil
}

// Upsert adds or replaces an approval record and persists the full set
// to path atomically.
func (p *Permissions) Upsert(path string, record ApprovalRecord) error {
	if p == nil {
		return fmt.Errorf("permissions is nil")
	}
	id := strings.TrimSpace(record.ExtensionID)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	record.ExtensionID = id
	p.approvals[id] = record
	return savePermissionsAtomic(path, p)
}

// Delete removes an approval record and persists the remaining set.
// Deleting an unknown id is a no-op.
func (p *Permissions) Delete(path, id string) error {
	if p == nil {
		return fmt.Errorf("permissions is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("extension_id is required")
	}
	delete(p.approvals, id)
	return savePermissionsAtomic(path, p)
}

// savePermissionsAtomic writes approvals to a temp file then renames
// into place to avoid partial writes.
func savePermissionsAtomic(path string, p *Permissions) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("approvals path is required")
	}
	records := make([]ApprovalRecord, 0, len(p.approvals))
	for _, record := range p.approvals {
		records = append(records, record)
	}
	payload := approvalFile{Approvals: records}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding approvals: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating approvals dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".approvals-*.json")
	if err != nil {
		return fmt.Errorf("creating temp approvals file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp approvals file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp approvals file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp approvals file: %w", err)
	}
	return nil
}

func (p *Permissions) Approval(extensionID string) (ApprovalRecord, bool) {
	if p == nil {
		return ApprovalRecord{}, false
	}
	record, ok := p.approvals[strings.TrimSpace(extensionID)]
	return record, ok
}

func (p *Permissions) ResolveTrust(extensionID string, fallback TrustClass) TrustClass {
	if record, ok := p.Approval(extensionID); ok && record.TrustClass != "" {
		return record.TrustClass
	}
	return fallback
}

func (p *Permissions) HostedApproved(extensionID string, trust TrustClass) bool {
	// Declarative and compiled-in extensions do not require hosted approval.
	if trust != TrustClassHostedThirdParty && trust != TrustClassHostedFirstParty {
		return true
	}
	record, ok := p.Approval(extensionID)
	if !ok {
		return false
	}
	return record.HostedRequired
}

func (p *Permissions) AllowsCapability(extensionID string, trust TrustClass, capability Capability) bool {
	// Default deny for hosted third-party intercept; requires explicit approval.
	if trust == TrustClassHostedThirdParty && capability == CapabilityToolIntercept {
		record, ok := p.Approval(extensionID)
		if !ok {
			return false
		}
		return slices.Contains(record.GrantedCapabilities, capability)
	}

	// Hosted extensions require explicit capability grants.
	if trust == TrustClassHostedThirdParty || trust == TrustClassHostedFirstParty {
		record, ok := p.Approval(extensionID)
		if !ok {
			return false
		}
		return slices.Contains(record.GrantedCapabilities, capability)
	}

	// Declarative/compiled-in are trusted by default.
	return true
}

func ResolveManifestTrust(manifest Manifest) TrustClass {
	switch manifest.runtimeType() {
	case RuntimeTypeCompiledIn:
		return TrustClassCompiledIn
	case RuntimeTypeHostedStdioJSONRPC:
		return TrustClassHostedThirdParty
	default:
		return TrustClassDeclarative
	}
}

// AllowsService reports whether the given (extension, trust) tuple is
// allowed to call a v2 service method. It looks up the capability
// string associated with (service, method) and checks the extension's
// granted_capabilities from approvals.json. Services not in the mapping
// are denied for hosted extensions and allowed for
// declarative/compiled-in extensions.
func (p *Permissions) AllowsService(extensionID string, trust TrustClass, service, method string) bool {
	cap, ok := capabilityForServiceMethod(service, method)
	if !ok {
		// Unknown mapping. Trust declarative/compiled-in implicitly.
		return trust == TrustClassDeclarative || trust == TrustClassCompiledIn
	}
	if cap == "" {
		// Known method with no capability gate (e.g. reads, on_* callbacks).
		return true
	}
	return p.AllowsCapability(extensionID, trust, cap)
}

// capabilityForServiceMethod maps a (service, method) tuple to the
// Capability string required to call it. Returning ("", true) means
// the method has no gate (always allowed); returning (_, false) means
// the mapping is unknown.
func capabilityForServiceMethod(service, method string) (Capability, bool) {
	key := service + "." + method
	switch key {
	// session
	case "session.get_metadata":
		return CapabilitySessionRead, true
	case "session.set_name", "session.set_tags":
		return CapabilitySessionWrite, true

	// agent
	case "agent.get_mode", "agent.list_modes":
		return "", true
	case "agent.set_mode", "agent.register_mode", "agent.unregister_mode":
		return CapabilityAgentMode, true

	// state
	case "state.get":
		return CapabilityStateRead, true
	case "state.set", "state.patch", "state.delete":
		return CapabilityStateWrite, true

	// commands
	case "commands.register", "commands.unregister":
		return CapabilityCommandRegister, true

	// tools
	case "tools.register", "tools.unregister":
		return CapabilityToolRegister, true
	case "tools.intercept":
		return CapabilityToolIntercept, true

	// ui
	case "ui.status", "ui.clear_status":
		return CapabilityUIStatus, true
	case "ui.widget", "ui.clear_widget":
		return CapabilityUIWidget, true
	case "ui.notify":
		return CapabilityUIStatus, true // ui.notification maps to ui.status for v1
	case "ui.dialog":
		return CapabilityUIDialog, true

	// chat
	case "chat.append_message":
		return CapabilityChatAppend, true

	// sigils
	case "sigils.register", "sigils.unregister":
		return CapabilitySigilsRegister, true
	case "sigils.list":
		return "", true
	}
	return "", false
}
