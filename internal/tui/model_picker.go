package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dimetron/pi-go/internal/provider"
)

// modelPickerEntry is a single row in the model picker.
// It is either a provider section header or a selectable model.
type modelPickerEntry struct {
	providerHeader string              // non-empty for section headers
	model          *provider.ModelEntry // non-nil for selectable models
}

func (e modelPickerEntry) isHeader() bool { return e.providerHeader != "" }

// modelPickerState manages the interactive model picker popup.
type modelPickerState struct {
	entries    []modelPickerEntry // flat list (headers + models interleaved)
	all        []modelPickerEntry // unfiltered copy
	selected   int                // index into entries (skips headers)
	height     int                // visible rows
	scrollOff  int                // scroll offset
	filter     string             // live type-to-filter text
	loading    bool               // true while fetching
	current    string             // current model name (to highlight)
	err        error              // fetch error, if any
	hidden     map[string]bool    // model IDs the user has hidden
	showHidden bool               // when true, hidden models appear dimmed instead of filtered out
}

// modelsFetchedMsg carries async list-models results back to Update().
type modelsFetchedMsg struct {
	entries []modelPickerEntry
	err     error
}

// buildPickerEntries groups ModelEntry slices by provider into a flat list
// with section headers.
func buildPickerEntries(models []provider.ModelEntry) []modelPickerEntry {
	// Group by provider, preserving order of first occurrence.
	type group struct {
		provider string
		models   []provider.ModelEntry
	}
	seen := map[string]int{}
	var groups []group
	for _, m := range models {
		idx, ok := seen[m.Provider]
		if !ok {
			idx = len(groups)
			seen[m.Provider] = idx
			groups = append(groups, group{provider: m.Provider})
		}
		groups[idx].models = append(groups[idx].models, m)
	}

	var entries []modelPickerEntry
	for _, g := range groups {
		entries = append(entries, modelPickerEntry{providerHeader: g.provider})
		for i := range g.models {
			entries = append(entries, modelPickerEntry{model: &g.models[i]})
		}
	}
	return entries
}

// fetchModels returns a tea.Cmd that fetches models from all configured providers.
func fetchModels(ctx context.Context, reg *provider.Registry) tea.Cmd {
	return func() tea.Msg {
		if reg == nil {
			return modelsFetchedMsg{err: fmt.Errorf("no provider registry configured")}
		}
		var all []provider.ModelEntry
		for _, def := range reg.Providers() {
			models, err := reg.ListModels(ctx, def.Name, nil)
			if err != nil {
				// Skip providers that fail (e.g. no API key).
				continue
			}
			all = append(all, models...)
		}
		if len(all) == 0 {
			return modelsFetchedMsg{err: fmt.Errorf("no models found (check API keys)")}
		}
		return modelsFetchedMsg{entries: buildPickerEntries(all)}
	}
}

// applyFilter rebuilds the visible entries list from the full list,
// respecting both the text filter and the hidden-models set.
func (p *modelPickerState) applyFilter() {
	lower := strings.ToLower(p.filter)
	hasFilter := p.filter != ""
	hasHidden := len(p.hidden) > 0

	// Fast path: no filter and no hidden models (or showing hidden).
	if !hasFilter && (!hasHidden || p.showHidden) {
		p.entries = p.all
		p.selected = p.clampToModel(p.selected)
		p.scrollOff = 0
		return
	}

	var filtered []modelPickerEntry
	var currentHeader *modelPickerEntry
	headerHasMatch := false

	for i := range p.all {
		e := p.all[i]
		if e.isHeader() {
			currentHeader = &p.all[i]
			headerHasMatch = false
			continue
		}
		if e.model == nil {
			continue
		}

		// Skip hidden models unless showHidden is on.
		if !p.showHidden && p.hidden[e.model.ID] {
			continue
		}

		// Apply text filter.
		if hasFilter {
			match := strings.Contains(strings.ToLower(e.model.ID), lower) ||
				strings.Contains(strings.ToLower(e.model.DisplayName), lower)
			if !match {
				continue
			}
		}

		// Include the provider header before the first match in this group.
		if currentHeader != nil && !headerHasMatch {
			filtered = append(filtered, *currentHeader)
			headerHasMatch = true
		}
		filtered = append(filtered, e)
	}
	p.entries = filtered
	p.selected = p.clampToModel(p.selected)
	p.scrollOff = 0
}

// toggleHidden hides or unhides the currently selected model.
// Returns the model ID that was toggled, or "" if nothing was selected.
func (p *modelPickerState) toggleHidden() string {
	sel := p.selectedModel()
	if sel == nil {
		return ""
	}
	if p.hidden == nil {
		p.hidden = make(map[string]bool)
	}
	if p.hidden[sel.ID] {
		delete(p.hidden, sel.ID)
	} else {
		p.hidden[sel.ID] = true
	}
	return sel.ID
}

// hiddenList returns the hidden model IDs as a sorted slice (for persistence).
func (p *modelPickerState) hiddenList() []string {
	if len(p.hidden) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.hidden))
	for id := range p.hidden {
		out = append(out, id)
	}
	// Sort for deterministic output.
	sort.Strings(out)
	return out
}

// clampToModel ensures idx points at a model entry, not a header.
func (p *modelPickerState) clampToModel(idx int) int {
	if len(p.entries) == 0 {
		return 0
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(p.entries) {
		idx = len(p.entries) - 1
	}
	// If on a header, move down to next model.
	if p.entries[idx].isHeader() {
		for i := idx; i < len(p.entries); i++ {
			if !p.entries[i].isHeader() {
				return i
			}
		}
		// No model below; try above.
		for i := idx - 1; i >= 0; i-- {
			if !p.entries[i].isHeader() {
				return i
			}
		}
	}
	return idx
}

// moveUp moves the selection up, skipping headers.
func (p *modelPickerState) moveUp() {
	for i := p.selected - 1; i >= 0; i-- {
		if !p.entries[i].isHeader() {
			p.selected = i
			if p.selected < p.scrollOff {
				p.scrollOff = p.selected
			}
			return
		}
	}
}

// moveDown moves the selection down, skipping headers.
func (p *modelPickerState) moveDown() {
	for i := p.selected + 1; i < len(p.entries); i++ {
		if !p.entries[i].isHeader() {
			p.selected = i
			if p.selected >= p.scrollOff+p.height {
				p.scrollOff = p.selected - p.height + 1
			}
			return
		}
	}
}

// selectedModel returns the currently selected ModelEntry, or nil.
func (p *modelPickerState) selectedModel() *provider.ModelEntry {
	if p.selected >= 0 && p.selected < len(p.entries) {
		return p.entries[p.selected].model
	}
	return nil
}

// selectCurrent positions the cursor on the entry matching currentModel.
func (p *modelPickerState) selectCurrent() {
	for i, e := range p.entries {
		if e.model != nil && e.model.ID == p.current {
			p.selected = i
			// Centre the view around the selected item.
			if p.selected >= p.height {
				p.scrollOff = p.selected - p.height/2
				if p.scrollOff < 0 {
					p.scrollOff = 0
				}
			}
			return
		}
	}
	// Fallback: first model row.
	p.selected = p.clampToModel(0)
}

// renderModelPicker renders the model picker popup.
func (m *model) renderModelPicker() string {
	if m.modelPicker == nil {
		return ""
	}

	pk := m.modelPicker
	bg := lipgloss.Color("236")
	border := lipgloss.Color("240")
	selectedBg := lipgloss.Color("33")
	headerFg := lipgloss.Color("39")
	currentFg := lipgloss.Color("35")
	dimFg := lipgloss.Color("243")

	popupWidth := m.width - 10

	style := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Border(lipgloss.ThickBorder(), true, true, true, true).
		BorderForeground(border).
		Width(popupWidth)

	var b strings.Builder
	b.WriteString("\n")

	// Header.
	title := "Models"
	if pk.filter != "" {
		title = fmt.Sprintf("Models — filter: %s", pk.filter)
	}
	if pk.loading {
		title = "Loading models…"
	}
	if pk.err != nil {
		title = fmt.Sprintf("Error: %v", pk.err)
	}
	hints := "↑↓ navigate · type to filter · Enter select · H hide · S show hidden · Esc close"
	if pk.showHidden {
		hints = "↑↓ navigate · type to filter · Enter select · H unhide · S hide filtered · Esc close"
	}
	header := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Bold(true).
		Width(popupWidth).
		Align(lipgloss.Center).
		Render(title + "\n" + hints)
	b.WriteString(header)
	b.WriteString("\n")

	if pk.loading || pk.err != nil || len(pk.entries) == 0 {
		if len(pk.entries) == 0 && !pk.loading && pk.err == nil {
			empty := lipgloss.NewStyle().Background(bg).Foreground(dimFg).
				Width(popupWidth).Align(lipgloss.Center).
				Render("No matching models")
			b.WriteString(empty)
			b.WriteString("\n")
		}
		return style.Render(b.String())
	}

	// Visible window.
	height := pk.height
	entries := pk.entries
	scrollOff := pk.scrollOff

	endIdx := scrollOff + height
	if endIdx > len(entries) {
		endIdx = len(entries)
	}
	visible := entries[scrollOff:endIdx]

	for i, entry := range visible {
		actualIdx := i + scrollOff
		isSelected := actualIdx == pk.selected

		if entry.isHeader() {
			line := fmt.Sprintf("  ── %s ──", strings.ToUpper(entry.providerHeader))
			lineStyle := lipgloss.NewStyle().Background(bg).Foreground(headerFg).Bold(true)
			b.WriteString(lineStyle.Width(popupWidth).Render(line))
			b.WriteString("\n")
			continue
		}

		if entry.model == nil {
			continue
		}

		isCurrent := entry.model.ID == pk.current
		isHidden := pk.hidden[entry.model.ID]

		// Build the model line: ID + display name + context window.
		line := fmt.Sprintf("    %s", entry.model.ID)
		if entry.model.DisplayName != "" && entry.model.DisplayName != entry.model.ID {
			line = fmt.Sprintf("    %s  (%s)", entry.model.ID, entry.model.DisplayName)
		}

		// Append context window info.
		ctxInfo := formatTokenLimits(entry.model.MaxInputTokens, entry.model.MaxOutputTokens)
		if ctxInfo != "" {
			line += "  " + ctxInfo
		}

		if isHidden {
			line += "  [hidden]"
		}
		if isCurrent {
			line += "  ●"
		}
		if isSelected {
			line = "  > " + line[4:]
		}

		hiddenFg := lipgloss.Color("238") // very dim for hidden models

		var lineStyle lipgloss.Style
		switch {
		case isSelected && isHidden:
			lineStyle = lipgloss.NewStyle().Background(selectedBg).Foreground(lipgloss.Color("245")).Strikethrough(true)
		case isSelected:
			lineStyle = lipgloss.NewStyle().Background(selectedBg).Foreground(lipgloss.Color("15"))
		case isHidden:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(hiddenFg).Strikethrough(true)
		case isCurrent:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(currentFg)
		default:
			lineStyle = lipgloss.NewStyle().Background(bg).Foreground(dimFg)
		}
		b.WriteString(lineStyle.Width(popupWidth).Render(line))
		b.WriteString("\n")
	}

	// Footer: scroll position + hidden count.
	{
		scrollStyle := lipgloss.NewStyle().Background(bg).Foreground(dimFg)
		footer := fmt.Sprintf("  %d/%d", pk.selected+1, len(pk.entries))
		if len(pk.entries) > pk.height {
			footer += "  ↑↓ scroll"
		}
		if hiddenCount := len(pk.hidden); hiddenCount > 0 {
			if pk.showHidden {
				footer += fmt.Sprintf("  (%d hidden shown)", hiddenCount)
			} else {
				footer += fmt.Sprintf("  (%d hidden)", hiddenCount)
			}
		}
		b.WriteString(scrollStyle.Width(popupWidth).Render(footer))
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

// loadHiddenModels reads the "hiddenModels" array from ~/.pi-go/config.json.
func loadHiddenModels() map[string]bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".pi-go", "config.json"))
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	arr, ok := raw["hiddenModels"].([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make(map[string]bool, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out[s] = true
		}
	}
	return out
}

// saveHiddenModels writes the "hiddenModels" array to ~/.pi-go/config.json.
func saveHiddenModels(hidden map[string]bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configDir := filepath.Join(home, ".pi-go")
	configPath := filepath.Join(configDir, "config.json")

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			raw = make(map[string]any)
		}
	}

	// Build sorted list.
	ids := make([]string, 0, len(hidden))
	for id := range hidden {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if len(ids) == 0 {
		delete(raw, "hiddenModels")
	} else {
		raw["hiddenModels"] = ids
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.WriteFile(configPath, out, 0o644)
}

// formatTokenLimits returns a compact human-readable string like "128k in / 8k out".
// Returns "" if both values are zero (unknown).
func formatTokenLimits(maxIn, maxOut int64) string {
	if maxIn == 0 && maxOut == 0 {
		return ""
	}
	format := func(n int64) string {
		if n <= 0 {
			return "—"
		}
		if n >= 1_000_000 {
			v := float64(n) / 1_000_000
			if v == float64(int64(v)) {
				return fmt.Sprintf("%.0fM", v)
			}
			return fmt.Sprintf("%.1fM", v)
		}
		if n >= 1000 {
			v := float64(n) / 1000
			if v == float64(int64(v)) {
				return fmt.Sprintf("%.0fk", v)
			}
			return fmt.Sprintf("%.1fk", v)
		}
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("[%s in / %s out]", format(maxIn), format(maxOut))
}
