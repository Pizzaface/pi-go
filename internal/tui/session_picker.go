package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	pisession "github.com/dimetron/pi-go/internal/session"
)

type sessionPickerState struct {
	entries   []pisession.Meta
	all       []pisession.Meta
	selected  int
	height    int
	scrollOff int
	filter    string
	current   string // current session ID (to highlight)
}

func (p *sessionPickerState) applyFilter() {
	if p.filter == "" {
		p.entries = p.all
	} else {
		lower := strings.ToLower(p.filter)
		var filtered []pisession.Meta
		for _, meta := range p.all {
			title := strings.ToLower(meta.Title)
			id := strings.ToLower(meta.ID)
			if strings.Contains(title, lower) || strings.Contains(id, lower) {
				filtered = append(filtered, meta)
			}
		}
		p.entries = filtered
	}
	if len(p.entries) == 0 {
		p.selected = 0
	} else if p.selected >= len(p.entries) {
		p.selected = len(p.entries) - 1
	}
	p.scrollOff = 0
	p.ensureSelectionVisible()
}

func (p *sessionPickerState) moveUp() {
	if p.selected > 0 {
		p.selected--
		if p.selected < p.scrollOff {
			p.scrollOff = p.selected
		}
	}
}

func (p *sessionPickerState) moveDown() {
	if p.selected < len(p.entries)-1 {
		p.selected++
		if p.selected >= p.scrollOff+p.height {
			p.scrollOff = p.selected - p.height + 1
		}
	}
}

func (p *sessionPickerState) ensureSelectionVisible() {
	if len(p.entries) == 0 {
		p.selected = 0
		p.scrollOff = 0
		return
	}
	if p.selected < 0 {
		p.selected = 0
	}
	if p.selected >= len(p.entries) {
		p.selected = len(p.entries) - 1
	}
	maxScroll := len(p.entries) - p.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollOff > maxScroll {
		p.scrollOff = maxScroll
	}
	if p.scrollOff < 0 {
		p.scrollOff = 0
	}
	if p.selected < p.scrollOff {
		p.scrollOff = p.selected
	}
	if p.selected >= p.scrollOff+p.height {
		p.scrollOff = p.selected - p.height + 1
	}
}

func (p *sessionPickerState) selectedMeta() *pisession.Meta {
	if p.selected >= 0 && p.selected < len(p.entries) {
		return &p.entries[p.selected]
	}
	return nil
}

func (m *model) renderSessionPicker() string {
	if m.sessionPicker == nil {
		return ""
	}

	pk := m.sessionPicker
	bg := lipgloss.Color("236")
	border := lipgloss.Color("240")
	selectedBg := lipgloss.Color("33")
	currentFg := lipgloss.Color("35")
	dimFg := lipgloss.Color("243")

	popupWidth := m.width - 10
	if popupWidth < 40 {
		popupWidth = m.width
	}

	style := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Border(lipgloss.ThickBorder(), true, true, true, true).
		BorderForeground(border).
		Width(popupWidth)

	var b strings.Builder
	b.WriteString("\n")

	title := "Sessions"
	if pk.filter != "" {
		title = fmt.Sprintf("Sessions — filter: %s", pk.filter)
	}
	hints := "↑↓ navigate · type to filter · Enter select · Esc close"
	header := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Bold(true).
		Width(popupWidth).
		Align(lipgloss.Center).
		Render(title + "\n" + hints)
	b.WriteString(header)
	b.WriteString("\n")

	if len(pk.entries) == 0 {
		empty := lipgloss.NewStyle().Background(bg).Foreground(dimFg).
			Width(popupWidth).Align(lipgloss.Center).
			Render("No matching sessions")
		b.WriteString(empty)
		b.WriteString("\n")
		return style.Render(b.String())
	}

	endIdx := pk.scrollOff + pk.height
	if endIdx > len(pk.entries) {
		endIdx = len(pk.entries)
	}
	visible := pk.entries[pk.scrollOff:endIdx]

	for i, meta := range visible {
		actualIdx := i + pk.scrollOff
		isSelected := actualIdx == pk.selected
		isCurrent := meta.ID == pk.current

		sessionTitle := strings.TrimSpace(meta.Title)
		if sessionTitle == "" {
			sessionTitle = meta.ID[:minInt(8, len(meta.ID))]
		}
		age := formatTimeAgo(meta.UpdatedAt)
		line := fmt.Sprintf("    %s  (%s)  %s", sessionTitle, meta.ID[:minInt(8, len(meta.ID))], age)
		if isCurrent {
			line += "  ●"
		}
		if isSelected {
			line = "  > " + line[4:]
		}

		var lineStyle lipgloss.Style
		switch {
		case isSelected:
			lineStyle = lipgloss.NewStyle().Background(selectedBg).Foreground(lipgloss.Color("15"))
		case isCurrent:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(currentFg)
		default:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(dimFg)
		}
		b.WriteString(lineStyle.Width(popupWidth).Render(line))
		b.WriteString("\n")
	}

	scrollStyle := lipgloss.NewStyle().Background(bg).Foreground(dimFg)
	footer := fmt.Sprintf("  %d/%d", pk.selected+1, len(pk.entries))
	if len(pk.entries) > pk.height {
		footer += "  ↑↓ scroll"
	}
	b.WriteString(scrollStyle.Width(popupWidth).Render(footer))
	b.WriteString("\n")

	return style.Render(b.String())
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
