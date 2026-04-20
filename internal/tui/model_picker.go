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

	"github.com/pizzaface/go-pi/internal/provider"
)

// modelPickerEntry is a single row in the model picker.
// It is either a provider section header or a selectable model.
type modelPickerEntry struct {
	providerHeader string               // non-empty for section headers
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
	prevSelectedID := p.selectedModelID()
	prevSelected := p.selected

	lower := strings.ToLower(p.filter)
	hasFilter := p.filter != ""
	hasHidden := len(p.hidden) > 0

	// Fast path: no filter and no hidden models (or showing hidden).
	if !hasFilter && (!hasHidden || p.showHidden) {
		p.entries = p.all
		if prevSelectedID != "" && p.selectModelByID(prevSelectedID) {
			p.ensureSelectionVisible()
			return
		}
		p.selected = p.clampToModel(prevSelected)
		p.ensureSelectionVisible()
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
	if prevSelectedID != "" && p.selectModelByID(prevSelectedID) {
		p.ensureSelectionVisible()
		return
	}
	p.selected = p.clampToModel(prevSelected)
	p.ensureSelectionVisible()
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

func (p *modelPickerState) selectedModelID() string {
	if sel := p.selectedModel(); sel != nil {
		return sel.ID
	}
	return ""
}

func (p *modelPickerState) selectModelByID(id string) bool {
	if id == "" {
		return false
	}
	for i, e := range p.entries {
		if e.model != nil && e.model.ID == id {
			p.selected = i
			return true
		}
	}
	return false
}

func (p *modelPickerState) ensureSelectionVisible() {
	if len(p.entries) == 0 {
		p.selected = 0
		p.scrollOff = 0
		return
	}
	p.selected = p.clampToModel(p.selected)
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
	if p.scrollOff > maxScroll {
		p.scrollOff = maxScroll
	}
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
			p.ensureSelectionVisible()
			return
		}
	}
	// Fallback: first model row.
	p.selected = p.clampToModel(0)
	p.ensureSelectionVisible()
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
	hints := "↑↓ navigate · type to filter · Enter select · Esc close"
	shortcuts := "Shift+H hide · Shift+S show hidden"
	if pk.showHidden {
		shortcuts = "Shift+H unhide · Shift+S hide hidden"
	}
	header := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Bold(true).
		Width(popupWidth).
		Align(lipgloss.Center).
		Render(title + "\n" + hints + "\n" + shortcuts)
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

// piGoDir returns the ~/.go-pi directory path, or "" on error.
func piGoDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-pi")
}

// loadHiddenModels reads the hidden model IDs from ~/.go-pi/hidden_models.json.
// On first run, migrates from the legacy "hiddenModels" key in config.json.
func loadHiddenModels() map[string]bool {
	dir := piGoDir()
	if dir == "" {
		return nil
	}

	hiddenPath := filepath.Join(dir, "hidden_models.json")

	// Try the new dedicated file first.
	if data, err := os.ReadFile(hiddenPath); err == nil {
		return parseHiddenModelsJSON(data)
	}

	// Migrate from legacy config.json "hiddenModels" key.
	configPath := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(configPath)
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

	// Write to the new file and remove from config.json.
	saveHiddenModels(out)
	delete(raw, "hiddenModels")
	if updated, err := json.MarshalIndent(raw, "", "  "); err == nil {
		_ = os.WriteFile(configPath, updated, 0o644)
	}

	return out
}

// parseHiddenModelsJSON parses a JSON array of strings into a bool map.
func parseHiddenModelsJSON(data []byte) map[string]bool {
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil || len(ids) == 0 {
		return nil
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			out[id] = true
		}
	}
	return out
}

// saveHiddenModels writes hidden model IDs to ~/.go-pi/hidden_models.json.
func saveHiddenModels(hidden map[string]bool) {
	dir := piGoDir()
	if dir == "" {
		return
	}

	hiddenPath := filepath.Join(dir, "hidden_models.json")

	if len(hidden) == 0 {
		_ = os.Remove(hiddenPath)
		return
	}

	// Build sorted list for deterministic output.
	ids := make([]string, 0, len(hidden))
	for id := range hidden {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(hiddenPath, out, 0o644)
}

// LastModelSelection holds the persisted last-selected model.
type LastModelSelection struct {
	Model    string `json:"lastModel"`
	Provider string `json:"lastProvider"`
}

// LoadLastModel reads the last-selected model from ~/.go-pi/config.json.
func LoadLastModel() (modelID, providerName string) {
	dir := piGoDir()
	if dir == "" {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return "", ""
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", ""
	}
	m, _ := raw["lastModel"].(string)
	p, _ := raw["lastProvider"].(string)
	return m, p
}

// saveLastSelectedModel persists the last-selected model to ~/.go-pi/config.json.
func saveLastSelectedModel(modelID, providerName string) {
	dir := piGoDir()
	if dir == "" {
		return
	}
	configPath := filepath.Join(dir, "config.json")

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			raw = make(map[string]any)
		}
	}

	raw["lastModel"] = modelID
	raw["lastProvider"] = providerName

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(configPath, out, 0o644)
}

func loadCollapsedTools() bool {
	dir := piGoDir()
	if dir == "" {
		return true
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return true
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return true
	}
	collapsed, ok := raw["collapsedTools"].(bool)
	if !ok {
		return true
	}
	return collapsed
}

func saveCollapsedTools(collapsed bool) {
	dir := piGoDir()
	if dir == "" {
		return
	}
	configPath := filepath.Join(dir, "config.json")

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			raw = make(map[string]any)
		}
	}

	raw["collapsedTools"] = collapsed

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
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
