package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/internal/extension/lifecycle"
)

// extensionPanelState holds the /extensions overlay. Populated from
// lifecycle.Service.List() on open and on lifecycle events.
type extensionPanelState struct {
	open     bool
	views    []lifecycle.View
	selected int
	filter   string
	height   int
}

func (s *extensionPanelState) Open() bool { return s.open }

func (s *extensionPanelState) OpenPanel() { s.open = true }

func (s *extensionPanelState) Close() {
	s.open = false
	s.filter = ""
	s.selected = 0
}

func (s *extensionPanelState) SetViews(views []lifecycle.View) {
	s.views = views
	if s.selected >= len(views) {
		s.selected = len(views) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

func (s *extensionPanelState) MoveSelection(delta int) {
	s.selected += delta
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.views) {
		s.selected = len(s.views) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

var (
	panelBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(0, 1)
	panelHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("242"))
	panelSelectedRow = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	panelDimmedRow   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	panelErrorRow    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

func (s *extensionPanelState) View(width, height int) string {
	_ = width
	_ = height
	if !s.open {
		return ""
	}
	var b strings.Builder
	b.WriteString(panelHeaderStyle.Render(fmt.Sprintf("%-18s %-12s %-10s %-14s", "NAME", "MODE", "STATE", "TRUST")))
	b.WriteString("\n")
	views := s.filteredViews()
	for i, v := range views {
		row := fmt.Sprintf("%-18s %-12s %-10s %-14s", trunc(v.ID, 18), v.Mode, v.State, v.Trust)
		switch {
		case v.State == host.StateErrored:
			b.WriteString(panelErrorRow.Render(row))
		case v.Mode == "compiled-in":
			b.WriteString(panelDimmedRow.Render(row) + "  (implicit)")
		case i == s.selected:
			b.WriteString(panelSelectedRow.Render(row))
		default:
			b.WriteString(row)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(s.detailPane())
	b.WriteString("\n")
	b.WriteString(panelDimmedRow.Render("a approve · d deny · v revoke · s start · x stop · r restart · R reload · / filter · esc close"))
	return panelBorderStyle.Render(b.String())
}

func (s *extensionPanelState) detailPane() string {
	views := s.filteredViews()
	if len(views) == 0 {
		return panelDimmedRow.Render("(no extensions discovered)")
	}
	v := views[s.selected]
	var b strings.Builder
	switch v.State {
	case host.StatePending:
		b.WriteString("Pending approval. Requested capabilities:\n")
		for _, c := range v.Requested {
			b.WriteString("  " + c + "\n")
		}
	case host.StateErrored:
		b.WriteString("Error:\n  " + v.Err)
	default:
		b.WriteString(fmt.Sprintf("State: %s · Granted: %s", v.State, strings.Join(v.Granted, ", ")))
	}
	return b.String()
}

// SetFilter narrows visible rows to those whose ID or mode contain the
// substring (case-insensitive). Empty string clears the filter.
func (s *extensionPanelState) SetFilter(filter string) {
	s.filter = strings.ToLower(filter)
	if s.selected >= len(s.filteredViews()) {
		s.selected = 0
	}
}

func (s *extensionPanelState) filteredViews() []lifecycle.View {
	if s.filter == "" {
		return s.views
	}
	out := make([]lifecycle.View, 0, len(s.views))
	for _, v := range s.views {
		if strings.Contains(strings.ToLower(v.ID), s.filter) ||
			strings.Contains(strings.ToLower(v.Mode), s.filter) ||
			strings.Contains(strings.ToLower(v.State.String()), s.filter) {
			out = append(out, v)
		}
	}
	return out
}

// DispatchKey routes an action key to the supplied lifecycle.Service.
// Approve uses an interactive dialog (handled separately), so 'a' is
// not handled here. Filter is also separate.
func (s *extensionPanelState) DispatchKey(ctx context.Context, svc lifecycle.Service, r rune) error {
	views := s.filteredViews()
	if len(views) == 0 && r != 'R' {
		return nil
	}
	if r == 'R' {
		return svc.Reload(ctx)
	}
	v := views[s.selected]
	if v.Mode == "compiled-in" {
		return nil
	}
	switch r {
	case 's':
		return svc.Start(ctx, v.ID)
	case 'x':
		return svc.Stop(ctx, v.ID)
	case 'r':
		return svc.Restart(ctx, v.ID)
	case 'v':
		return svc.Revoke(ctx, v.ID)
	case 'd':
		return svc.Deny(ctx, v.ID, "denied from TUI")
	}
	return nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
