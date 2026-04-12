package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type slashCommandOverlayRowKind int

const (
	slashCommandOverlayRowHeader slashCommandOverlayRowKind = iota
	slashCommandOverlayRowCommand
)

type slashCommandOverlayRow struct {
	Kind        slashCommandOverlayRowKind
	Header      string
	Name        string
	Description string
}

func (r slashCommandOverlayRow) Selectable() bool {
	return r.Kind == slashCommandOverlayRowCommand
}

type slashCommandOverlayState struct {
	Rows          []slashCommandOverlayRow
	SelectedIndex int
	ScrollOffset  int
	Height        int
}

func newSlashCommandOverlayState(rows []slashCommandOverlayRow) slashCommandOverlayState {
	state := slashCommandOverlayState{Rows: rows, SelectedIndex: -1, ScrollOffset: 0, Height: 0}
	for i, row := range rows {
		if row.Selectable() {
			state.SelectedIndex = i
			break
		}
	}
	return state
}

func (s slashCommandOverlayState) SelectedRow() (slashCommandOverlayRow, bool) {
	if s.SelectedIndex < 0 || s.SelectedIndex >= len(s.Rows) {
		return slashCommandOverlayRow{}, false
	}
	row := s.Rows[s.SelectedIndex]
	if !row.Selectable() {
		return slashCommandOverlayRow{}, false
	}
	return row, true
}

func (s *slashCommandOverlayState) clampToSelectable(idx int) int {
	if len(s.Rows) == 0 {
		return -1
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s.Rows) {
		idx = len(s.Rows) - 1
	}
	if s.Rows[idx].Selectable() {
		return idx
	}
	for i := idx; i < len(s.Rows); i++ {
		if s.Rows[i].Selectable() {
			return i
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if s.Rows[i].Selectable() {
			return i
		}
	}
	return -1
}

func (s *slashCommandOverlayState) maxScrollOffset() int {
	if s.Height <= 0 {
		return 0
	}
	maxScroll := len(s.Rows) - s.Height
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (s *slashCommandOverlayState) EnsureSelectionVisible() {
	if len(s.Rows) == 0 {
		s.SelectedIndex = -1
		s.ScrollOffset = 0
		return
	}
	s.SelectedIndex = s.clampToSelectable(s.SelectedIndex)
	if s.SelectedIndex < 0 {
		s.ScrollOffset = 0
		return
	}
	if s.ScrollOffset < 0 {
		s.ScrollOffset = 0
	}
	maxScroll := s.maxScrollOffset()
	if s.ScrollOffset > maxScroll {
		s.ScrollOffset = maxScroll
	}
	if s.Height <= 0 {
		return
	}
	if s.SelectedIndex < s.ScrollOffset {
		s.ScrollOffset = s.SelectedIndex
	}
	if s.SelectedIndex >= s.ScrollOffset+s.Height {
		s.ScrollOffset = s.SelectedIndex - s.Height + 1
	}
	if s.ScrollOffset > maxScroll {
		s.ScrollOffset = maxScroll
	}
	if s.ScrollOffset < 0 {
		s.ScrollOffset = 0
	}
}

func (s *slashCommandOverlayState) Move(delta int) {
	if delta == 0 || len(s.Rows) == 0 {
		return
	}
	if s.SelectedIndex < 0 {
		s.SelectedIndex = s.clampToSelectable(0)
		s.EnsureSelectionVisible()
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for moved := 0; moved != delta; moved += step {
		next := s.SelectedIndex + step
		for next >= 0 && next < len(s.Rows) && !s.Rows[next].Selectable() {
			next += step
		}
		if next < 0 || next >= len(s.Rows) {
			break
		}
		s.SelectedIndex = next
	}
	s.EnsureSelectionVisible()
}

func (s *slashCommandOverlayState) VisibleRows() []slashCommandOverlayRow {
	if len(s.Rows) == 0 || s.Height <= 0 {
		return nil
	}
	s.EnsureSelectionVisible()
	start := s.ScrollOffset
	if start < 0 {
		start = 0
	}
	if start > len(s.Rows) {
		start = len(s.Rows)
	}
	end := start + s.Height
	if end > len(s.Rows) {
		end = len(s.Rows)
	}
	return s.Rows[start:end]
}

func (s *slashCommandOverlayState) HasVisibleSelectableRow() bool {
	for _, row := range s.VisibleRows() {
		if row.Selectable() {
			return true
		}
	}
	return false
}

func (s *slashCommandOverlayState) selectableCount() int {
	count := 0
	for _, row := range s.Rows {
		if row.Selectable() {
			count++
		}
	}
	return count
}

func (s *slashCommandOverlayState) selectedOrdinal() int {
	if s.SelectedIndex < 0 || s.SelectedIndex >= len(s.Rows) {
		return 0
	}
	ordinal := 0
	for i := 0; i <= s.SelectedIndex; i++ {
		if s.Rows[i].Selectable() {
			ordinal++
		}
	}
	return ordinal
}

func (s *slashCommandOverlayState) render(width int) string {
	if len(s.Rows) == 0 {
		return ""
	}
	s.EnsureSelectionVisible()
	visible := s.VisibleRows()
	if len(visible) == 0 {
		return ""
	}
	if width < 20 {
		width = 20
	}
	popupWidth := width - 4
	if popupWidth < 20 {
		popupWidth = width
	}
	innerWidth := popupWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("33")).Bold(true)
	commandStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Foreground(lipgloss.Color("252")).
		Padding(0, 1).
		Width(popupWidth)

	var body strings.Builder
	body.WriteString(titleStyle.Render(truncateOverlayText(fmt.Sprintf("Slash Commands  %d/%d", s.selectedOrdinal(), s.selectableCount()), innerWidth)))
	body.WriteByte('\n')
	body.WriteString(helpStyle.Render(truncateOverlayText("↑↓ move  •  Enter insert  •  Esc close", innerWidth)))
	body.WriteByte('\n')
	for i, row := range visible {
		absoluteIndex := s.ScrollOffset + i
		switch {
		case row.Kind == slashCommandOverlayRowHeader:
			body.WriteString(headerStyle.Render(truncateOverlayText("• "+row.Header, innerWidth)))
		case row.Kind == slashCommandOverlayRowCommand:
			line := formatSlashCommandOverlayLine(row, innerWidth)
			if absoluteIndex == s.SelectedIndex {
				body.WriteString(selectedStyle.Width(innerWidth).Render(truncateOverlayText("› "+line, innerWidth)))
			} else {
				body.WriteString(commandStyle.Render("  " + truncateOverlayText(line, maxInt(innerWidth-2, 1))))
			}
		default:
			body.WriteString(truncateOverlayText(row.Name, innerWidth))
		}
		body.WriteByte('\n')
	}
	rendered := borderStyle.Render(strings.TrimRight(body.String(), "\n"))
	boxLines := strings.Split(rendered, "\n")
	for i, line := range boxLines {
		boxLines[i] = lipgloss.PlaceHorizontal(width, lipgloss.Left, line)
	}
	return strings.Join(boxLines, "\n")
}

func formatSlashCommandOverlayLine(row slashCommandOverlayRow, innerWidth int) string {
	if innerWidth < 40 || row.Description == "" {
		return truncateOverlayText(row.Name, innerWidth)
	}
	separator := " — "
	nameWidth := lipgloss.Width(row.Name)
	remaining := innerWidth - nameWidth - lipgloss.Width(separator)
	if remaining <= 0 {
		return truncateOverlayText(row.Name, innerWidth)
	}
	return row.Name + separator + truncateOverlayText(row.Description, remaining)
}

func truncateOverlayText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildSlashCommandOverlayRows(inventory slashCommandInventory) []slashCommandOverlayRow {
	rows := make([]slashCommandOverlayRow, 0, len(inventory.BuiltIns)+len(inventory.Extensions)+len(inventory.Skills)+3)
	appendSection := func(header string, commands []slashCommandInventoryItem) {
		if len(commands) == 0 {
			return
		}
		rows = append(rows, slashCommandOverlayRow{Kind: slashCommandOverlayRowHeader, Header: header})
		for _, cmd := range commands {
			rows = append(rows, slashCommandOverlayRow{
				Kind:        slashCommandOverlayRowCommand,
				Name:        cmd.Name,
				Description: cmd.Description,
			})
		}
	}

	appendSection("Built-in Commands", inventory.BuiltIns)
	appendSection("Extension Commands", inventory.Extensions)
	appendSection("Skill Commands", inventory.Skills)
	return rows
}
