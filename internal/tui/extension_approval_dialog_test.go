package tui

import (
	"strings"
	"testing"
)

func TestApprovalDialog_StartsWithAllChecked(t *testing.T) {
	d := newApprovalDialog("x", "0.1", "desc", []string{"tools.register", "events.session_start"})
	for _, c := range d.Capabilities() {
		if !c.Checked {
			t.Fatalf("expected all pre-ticked; %q unchecked", c.Name)
		}
	}
}

func TestApprovalDialog_ToggleUnchecks(t *testing.T) {
	d := newApprovalDialog("x", "0.1", "", []string{"a", "b"})
	d.MoveSelection(1)
	d.Toggle()
	caps := d.Capabilities()
	if caps[1].Checked {
		t.Fatal("expected b unchecked after Toggle")
	}
	if !caps[0].Checked {
		t.Fatal("expected a still checked")
	}
}

func TestApprovalDialog_SelectedGrantsReturnsOnlyChecked(t *testing.T) {
	d := newApprovalDialog("x", "0.1", "", []string{"a", "b"})
	d.MoveSelection(0)
	d.Toggle() // uncheck a
	got := d.SelectedGrants()
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestApprovalDialog_ViewMentionsIDAndVersion(t *testing.T) {
	d := newApprovalDialog("foo-bar", "1.2.3", "great extension", []string{"x.y"})
	out := d.View(80, 24)
	if !strings.Contains(out, "foo-bar") || !strings.Contains(out, "1.2.3") {
		t.Fatalf("expected id + version in view; got %q", out)
	}
}
