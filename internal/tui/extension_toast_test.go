package tui

import (
	"strings"
	"testing"
)

func TestExtensionToast_HiddenWhenNoPending(t *testing.T) {
	ts := extensionToastState{pending: 0}
	if got := ts.View(); got != "" {
		t.Fatalf("expected empty view; got %q", got)
	}
}

func TestExtensionToast_RendersCountAndHint(t *testing.T) {
	ts := extensionToastState{pending: 2}
	got := ts.View()
	if !strings.Contains(got, "2 extensions pending") {
		t.Fatalf("expected count; got %q", got)
	}
	if !strings.Contains(got, "press e") {
		t.Fatalf("expected hint; got %q", got)
	}
}

func TestExtensionToast_HiddenAfterDismiss(t *testing.T) {
	ts := extensionToastState{pending: 2}
	ts.Dismiss()
	if ts.View() != "" {
		t.Fatal("expected hidden after Dismiss()")
	}
}

func TestExtensionToast_ReappearsWhenPendingRises(t *testing.T) {
	ts := extensionToastState{pending: 0, dismissed: true}
	ts.SetPending(3)
	if ts.View() == "" {
		t.Fatal("expected toast to reappear when pending rises above 0")
	}
}
