package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/extension"
)

func TestExtensionsPanel_BuildsGroupedRows(t *testing.T) {
	rows := buildExtensionPanelRows([]extension.ExtensionInfo{
		{ID: "ext.alpha", State: extension.StateRunning, TrustClass: extension.TrustClassHostedThirdParty},
		{ID: "ext.bravo", State: extension.StatePending, TrustClass: extension.TrustClassHostedThirdParty},
		{ID: "ext.charlie", State: extension.StateErrored, LastError: "boom"},
	})
	var headers []string
	for _, r := range rows {
		if r.isGroup {
			headers = append(headers, r.label)
		}
	}
	want := []string{"Pending approval", "Running", "Errored"}
	if strings.Join(headers, ",") != strings.Join(want, ",") {
		t.Fatalf("headers = %v, want %v", headers, want)
	}
}

func TestExtensionsPanel_RendersEmpty(t *testing.T) {
	out := renderExtensionsPanel(&extensionsPanelState{}, 80)
	if !strings.Contains(out, "no extensions registered") {
		t.Fatalf("empty panel should show placeholder, got %q", out)
	}
}

func TestExtensionsPanel_EscClosesPanel(t *testing.T) {
	mm := &model{}
	mm.extensionsPanel = &extensionsPanelState{}
	handled, _, _ := mm.handleExtensionsPanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if mm.extensionsPanel != nil {
		t.Fatal("expected panel to be closed on Esc")
	}
}

func TestExtensionsPanel_CursorSkipsGroups(t *testing.T) {
	rows := buildExtensionPanelRows([]extension.ExtensionInfo{
		{ID: "ext.one", State: extension.StatePending},
		{ID: "ext.two", State: extension.StateRunning},
	})
	mm := &model{}
	mm.extensionsPanel = &extensionsPanelState{rows: rows, cursor: 1} // ext.one (after Pending group header)

	// Move down should skip the Running group header and land on ext.two
	mm.moveExtPanelCursor(+1)
	if mm.extensionsPanel.cursor == 2 {
		// cursor 2 is the "Running" group header, should have skipped
		t.Fatal("cursor should skip group headers")
	}
	row, ok := mm.selectedExtRow()
	if !ok {
		t.Fatal("expected a selectable row")
	}
	if row.info.ID != "ext.two" {
		t.Fatalf("expected ext.two, got %s", row.info.ID)
	}
}

func TestExtensionsPanel_NilReturnsNotHandled(t *testing.T) {
	mm := &model{}
	handled, _, _ := mm.handleExtensionsPanelKey(tea.Key{Code: tea.KeyEsc})
	if handled {
		t.Fatal("nil panel should not handle keys")
	}
}

func TestExtensionsPanel_RenderNilIsEmpty(t *testing.T) {
	out := renderExtensionsPanel(nil, 80)
	if out != "" {
		t.Fatalf("nil state should render empty, got %q", out)
	}
}

func TestExtensionsPanel_SubDialogEscCancels(t *testing.T) {
	mm := &model{}
	mm.extensionsPanel = &extensionsPanelState{
		subDialog: &extensionApprovalDialogState{
			id:     "ext.foo",
			action: extensionDialogApprove,
		},
	}
	handled, _, _ := mm.handleExtensionsPanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected handled")
	}
	if mm.extensionsPanel.subDialog != nil {
		t.Fatal("expected subDialog to be cleared on Esc")
	}
	// Panel itself should still be open.
	if mm.extensionsPanel == nil {
		t.Fatal("panel should remain open when subDialog is cancelled")
	}
}
