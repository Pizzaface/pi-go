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
