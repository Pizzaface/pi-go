package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/lifecycle"
)

func TestExtensionPanel_InitiallyHidden(t *testing.T) {
	p := extensionPanelState{}
	if p.Open() {
		t.Fatal("expected hidden state")
	}
}

func TestExtensionPanel_OpenSetsOpen(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	if !p.Open() {
		t.Fatal("expected open after OpenPanel")
	}
}

func TestExtensionPanel_CloseHides(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.Close()
	if p.Open() {
		t.Fatal("expected hidden after Close")
	}
}

func TestExtensionPanel_ViewListsRowsAndHighlightsSelection(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{
		{ID: "a", Mode: "hosted-go", State: host.StatePending, Trust: host.TrustThirdParty},
		{ID: "b", Mode: "hosted-go", State: host.StateRunning, Trust: host.TrustThirdParty},
	})
	got := p.View(80, 24)
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatalf("expected both rows; got %q", got)
	}
	if !strings.Contains(got, "pending") || !strings.Contains(got, "running") {
		t.Fatalf("expected state cells; got %q", got)
	}
}

func TestExtensionPanel_NavigateMovesSelection(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	p.MoveSelection(1)
	if p.selected != 1 {
		t.Fatalf("expected selected=1; got %d", p.selected)
	}
	p.MoveSelection(1)
	p.MoveSelection(1) // clamp at last
	if p.selected != 2 {
		t.Fatalf("expected clamp at 2; got %d", p.selected)
	}
	p.MoveSelection(-5)
	if p.selected != 0 {
		t.Fatalf("expected clamp at 0; got %d", p.selected)
	}
}

func TestExtensionPanel_DetailPaneShowsRequestedCapabilitiesOnPending(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{
		{ID: "a", State: host.StatePending, Requested: []string{"tools.register", "events.session_start"}},
	})
	got := p.View(80, 24)
	if !strings.Contains(got, "tools.register") || !strings.Contains(got, "events.session_start") {
		t.Fatalf("expected requested caps in detail pane; got %q", got)
	}
}

func TestExtensionPanel_FilterMatchesIDAndMode(t *testing.T) {
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews([]lifecycle.View{
		{ID: "alpha", Mode: "hosted-go"},
		{ID: "beta", Mode: "hosted-ts"},
		{ID: "gamma", Mode: "compiled-in"},
	})
	p.SetFilter("ts")
	got := p.View(80, 24)
	if !strings.Contains(got, "beta") {
		t.Fatal("expected beta in filtered output")
	}
	if strings.Contains(got, "alpha") || strings.Contains(got, "gamma") {
		t.Fatalf("filter failed to exclude: %q", got)
	}
}

type fakeLifecycle struct {
	approveCalls []string
	denyCalls    []string
	revokeCalls  []string
	startCalls   []string
	stopCalls    []string
	restartCalls []string
	reloadCalls  int
	views        []lifecycle.View
}

func (f *fakeLifecycle) List() []lifecycle.View { return f.views }
func (f *fakeLifecycle) Get(id string) (lifecycle.View, bool) {
	for _, v := range f.views {
		if v.ID == id {
			return v, true
		}
	}
	return lifecycle.View{}, false
}
func (f *fakeLifecycle) Approve(_ context.Context, id string, _ []string) error {
	f.approveCalls = append(f.approveCalls, id)
	return nil
}
func (f *fakeLifecycle) Deny(_ context.Context, id string, _ string) error {
	f.denyCalls = append(f.denyCalls, id)
	return nil
}
func (f *fakeLifecycle) Revoke(_ context.Context, id string) error {
	f.revokeCalls = append(f.revokeCalls, id)
	return nil
}
func (f *fakeLifecycle) Start(_ context.Context, id string) error {
	f.startCalls = append(f.startCalls, id)
	return nil
}
func (f *fakeLifecycle) Stop(_ context.Context, id string) error {
	f.stopCalls = append(f.stopCalls, id)
	return nil
}
func (f *fakeLifecycle) Restart(_ context.Context, id string) error {
	f.restartCalls = append(f.restartCalls, id)
	return nil
}
func (f *fakeLifecycle) StartApproved(context.Context) []error      { return nil }
func (f *fakeLifecycle) StopAll(context.Context) []error            { return nil }
func (f *fakeLifecycle) Reload(context.Context) error               { f.reloadCalls++; return nil }
func (f *fakeLifecycle) SetShutdownHook(lifecycle.HookFunc, string) {}
func (f *fakeLifecycle) Subscribe() (<-chan lifecycle.Event, func()) {
	ch := make(chan lifecycle.Event)
	return ch, func() {}
}

func TestExtensionPanel_KeysDispatchToService(t *testing.T) {
	f := &fakeLifecycle{}
	f.views = []lifecycle.View{{ID: "x", Mode: "hosted-go", State: host.StatePending, Trust: host.TrustThirdParty}}
	p := extensionPanelState{}
	p.OpenPanel()
	p.SetViews(f.views)
	p.DispatchKey(context.Background(), f, 's')
	p.DispatchKey(context.Background(), f, 'x')
	p.DispatchKey(context.Background(), f, 'r')
	p.DispatchKey(context.Background(), f, 'R')
	p.DispatchKey(context.Background(), f, 'v')
	p.DispatchKey(context.Background(), f, 'd')
	if len(f.startCalls) != 1 || f.startCalls[0] != "x" {
		t.Fatalf("startCalls=%v", f.startCalls)
	}
	if len(f.stopCalls) != 1 {
		t.Fatalf("stopCalls=%v", f.stopCalls)
	}
	if len(f.restartCalls) != 1 {
		t.Fatalf("restartCalls=%v", f.restartCalls)
	}
	if f.reloadCalls != 1 {
		t.Fatalf("reloadCalls=%d", f.reloadCalls)
	}
	if len(f.revokeCalls) != 1 {
		t.Fatalf("revokeCalls=%v", f.revokeCalls)
	}
	if len(f.denyCalls) != 1 {
		t.Fatalf("denyCalls=%v", f.denyCalls)
	}
}
