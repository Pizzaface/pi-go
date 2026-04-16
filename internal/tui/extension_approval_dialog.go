package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type approvalCapability struct {
	Name    string
	Checked bool
}

type approvalDialogState struct {
	id          string
	version     string
	description string
	caps        []approvalCapability
	selected    int
}

func newApprovalDialog(id, version, description string, requested []string) *approvalDialogState {
	caps := make([]approvalCapability, len(requested))
	for i, c := range requested {
		caps[i] = approvalCapability{Name: c, Checked: true}
	}
	return &approvalDialogState{
		id: id, version: version, description: description, caps: caps,
	}
}

func (s *approvalDialogState) Capabilities() []approvalCapability {
	out := make([]approvalCapability, len(s.caps))
	copy(out, s.caps)
	return out
}

func (s *approvalDialogState) SelectedGrants() []string {
	var out []string
	for _, c := range s.caps {
		if c.Checked {
			out = append(out, c.Name)
		}
	}
	return out
}

func (s *approvalDialogState) MoveSelection(delta int) {
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.caps) {
		s.selected = len(s.caps) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

func (s *approvalDialogState) Toggle() {
	if s.selected >= len(s.caps) {
		return
	}
	s.caps[s.selected].Checked = !s.caps[s.selected].Checked
}

var (
	dialogBorder = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).Padding(0, 1)
	dialogTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

func (s *approvalDialogState) View(width, height int) string {
	_ = width
	_ = height
	var b strings.Builder
	b.WriteString(dialogTitle.Render(fmt.Sprintf("Approve %s?", s.id)))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("%s v%s\n", s.id, s.version))
	if s.description != "" {
		b.WriteString(s.description + "\n")
	}
	b.WriteString("\nRequested capabilities:\n")
	for i, c := range s.caps {
		marker := "[ ]"
		if c.Checked {
			marker = "[x]"
		}
		line := fmt.Sprintf("  %s %s", marker, c.Name)
		if i == s.selected {
			line = dialogTitle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\nSpace toggle · Enter approve · Esc cancel")
	return dialogBorder.Render(b.String())
}
