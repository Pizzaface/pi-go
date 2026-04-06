package tui

import "testing"

func TestHasBlockingPopup(t *testing.T) {
	m := &model{}
	if m.hasBlockingPopup() {
		t.Fatal("expected no blocking popup by default")
	}

	m.modelPicker = &modelPickerState{}
	if !m.hasBlockingPopup() {
		t.Fatal("expected model picker to count as blocking popup")
	}

	m.modelPicker = nil
	m.loginPicker = &loginPickerState{}
	if !m.hasBlockingPopup() {
		t.Fatal("expected login picker to count as blocking popup")
	}
}

func TestReservedOverlayLines(t *testing.T) {
	m := &model{
		branchPopup: &branchPopupState{height: 4},
		modelPicker: &modelPickerState{height: 7},
		loginPicker: &loginPickerState{height: 5},
	}

	got := m.reservedOverlayLines()
	want := (4 + 6) + (7 + 6) + (5 + 6)
	if got != want {
		t.Fatalf("reservedOverlayLines() = %d, want %d", got, want)
	}
}
