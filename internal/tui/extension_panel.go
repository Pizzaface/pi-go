package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	extapi "github.com/pizzaface/go-pi/internal/extension/api"
	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/lifecycle"
)

// extensionPanelState holds the /extensions overlay. Populated from
// lifecycle.Service.List() on open and on lifecycle events.
type extensionPanelState struct {
	open     bool
	views    []lifecycle.View
	selected int
	filter   string
	height   int

	// Tools sub-view state: populated when SetToolRegistry is called.
	registry   *extapi.HostedToolRegistry
	collidedMu sync.Mutex
	collided   []extapi.Change
}

// SetToolRegistry wires the panel to a HostedToolRegistry so the detail
// pane can render the tools owned by the selected extension and track
// collisions surfaced via OnChange. Safe to call at most once per panel.
func (s *extensionPanelState) SetToolRegistry(reg *extapi.HostedToolRegistry) {
	s.registry = reg
	if reg == nil {
		return
	}
	reg.OnChange(func(c extapi.Change) {
		if c.Kind != extapi.ChangeCollisionRejected {
			return
		}
		s.collidedMu.Lock()
		s.collided = append(s.collided, c)
		if len(s.collided) > 32 {
			s.collided = s.collided[len(s.collided)-32:]
		}
		s.collidedMu.Unlock()
	})
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
	if s.registry != nil {
		if tools := s.toolsOf(v.ID); len(tools) > 0 {
			b.WriteString("\nTools: " + strings.Join(tools, ", "))
		}
		if rejected := s.collisionsFor(v.ID); len(rejected) > 0 {
			b.WriteString("\nRejected: " + strings.Join(rejected, ", "))
		}
	}
	return b.String()
}

// toolsOf returns the names of tools owned by extID, sorted by the
// registry snapshot order.
func (s *extensionPanelState) toolsOf(extID string) []string {
	var out []string
	for _, e := range s.registry.Snapshot() {
		if e.ExtID == extID {
			out = append(out, e.Desc.Name)
		}
	}
	return out
}

// collisionsFor returns "<tool> (↯ <winner>)" entries recorded for
// extID's rejected registrations.
func (s *extensionPanelState) collisionsFor(extID string) []string {
	s.collidedMu.Lock()
	defer s.collidedMu.Unlock()
	var out []string
	for _, c := range s.collided {
		if c.ExtID != extID {
			continue
		}
		out = append(out, fmt.Sprintf("%s (↯ %s)", c.ToolName, c.ConflictWith))
	}
	return out
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
