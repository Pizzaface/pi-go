package tui

import (
	"testing"
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
