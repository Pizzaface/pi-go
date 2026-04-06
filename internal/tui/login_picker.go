package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dimetron/pi-go/internal/auth"
	"github.com/dimetron/pi-go/internal/config"
)

// loginPickerEntry is a single row in the /login popup.
type loginPickerEntry struct {
	sectionHeader string
	provider      *auth.Provider
	configured    bool
	statusText    string
	detailText    string
}

func (e loginPickerEntry) isHeader() bool { return e.sectionHeader != "" }

// loginPickerState manages the interactive provider selector shown by /login.
type loginPickerState struct {
	entries   []loginPickerEntry
	selected  int
	height    int
	scrollOff int
}

func buildLoginPickerEntries() []loginPickerEntry {
	keys := config.APIKeys()
	stored, _ := auth.LoadAuth()
	providers := auth.Providers()

	var oauthProviders []loginPickerEntry
	var apiKeyProviders []loginPickerEntry
	for i := range providers {
		prov := providers[i]
		switch {
		case isOAuthLoginProvider(prov):
			configured := false
			if saved, ok := stored[prov.Name]; ok && saved.Type == "oauth" && saved.Access != "" {
				configured = true
			}
			oauthProviders = append(oauthProviders, loginPickerEntry{
				provider:   &prov,
				configured: configured,
				statusText: boolStatus(configured),
				detailText: "browser OAuth",
			})
		case isManualAPIKeyProvider(prov):
			configured := keys[prov.Name] != ""
			apiKeyProviders = append(apiKeyProviders, loginPickerEntry{
				provider:   &prov,
				configured: configured,
				statusText: boolStatus(configured),
				detailText: prov.EnvVar,
			})
		}
	}

	var entries []loginPickerEntry
	if len(oauthProviders) > 0 {
		entries = append(entries, loginPickerEntry{sectionHeader: "OAuth providers"})
		entries = append(entries, oauthProviders...)
	}
	if len(apiKeyProviders) > 0 {
		entries = append(entries, loginPickerEntry{sectionHeader: "API key providers"})
		entries = append(entries, apiKeyProviders...)
	}
	return entries
}

func isOAuthLoginProvider(prov auth.Provider) bool {
	// For now, only Codex should be promoted in the OAuth selector.
	return prov.Name == "codex"
}

func isManualAPIKeyProvider(prov auth.Provider) bool {
	return prov.AuthURL == "" && prov.TokenURL == "" && prov.DeviceURL == ""
}

func boolStatus(ok bool) string {
	if ok {
		return "configured"
	}
	return "not configured"
}

func (p *loginPickerState) clampToProvider(idx int) int {
	if len(p.entries) == 0 {
		return 0
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(p.entries) {
		idx = len(p.entries) - 1
	}
	if p.entries[idx].provider != nil {
		return idx
	}
	for i := idx + 1; i < len(p.entries); i++ {
		if p.entries[i].provider != nil {
			return i
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if p.entries[i].provider != nil {
			return i
		}
	}
	return idx
}

func (p *loginPickerState) moveUp() {
	for i := p.selected - 1; i >= 0; i-- {
		if p.entries[i].provider != nil {
			p.selected = i
			p.ensureSelectionVisible()
			return
		}
	}
}

func (p *loginPickerState) moveDown() {
	for i := p.selected + 1; i < len(p.entries); i++ {
		if p.entries[i].provider != nil {
			p.selected = i
			p.ensureSelectionVisible()
			return
		}
	}
}

func (p *loginPickerState) selectedProvider() *auth.Provider {
	if p.selected < 0 || p.selected >= len(p.entries) {
		return nil
	}
	return p.entries[p.selected].provider
}

func (p *loginPickerState) ensureSelectionVisible() {
	if p.height <= 0 {
		return
	}
	if p.selected < p.scrollOff {
		p.scrollOff = p.selected
	}
	if p.selected >= p.scrollOff+p.height {
		p.scrollOff = p.selected - p.height + 1
	}
	if p.scrollOff < 0 {
		p.scrollOff = 0
	}
}

// renderLoginPicker renders the /login provider selector popup.
func (m *model) renderLoginPicker() string {
	if m.loginPicker == nil {
		return ""
	}

	pk := m.loginPicker
	bg := lipgloss.Color("236")
	border := lipgloss.Color("240")
	selectedBg := lipgloss.Color("33")
	headerFg := lipgloss.Color("39")
	configuredFg := lipgloss.Color("42")
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
	header := lipgloss.NewStyle().
		Background(bg).
		Foreground(lipgloss.Color("252")).
		Bold(true).
		Width(popupWidth).
		Align(lipgloss.Center).
		Render("Login providers\n↑↓ navigate · Enter select · Esc close\n/oauth: Codex · /login <provider> still works")
	b.WriteString(header)
	b.WriteString("\n")

	if len(pk.entries) == 0 {
		empty := lipgloss.NewStyle().Background(bg).Foreground(dimFg).
			Width(popupWidth).Align(lipgloss.Center).
			Render("No login providers available")
		b.WriteString(empty)
		b.WriteString("\n")
		return style.Render(b.String())
	}

	endIdx := pk.scrollOff + pk.height
	if endIdx > len(pk.entries) {
		endIdx = len(pk.entries)
	}
	visible := pk.entries[pk.scrollOff:endIdx]
	for i, entry := range visible {
		actualIdx := pk.scrollOff + i
		isSelected := actualIdx == pk.selected
		if entry.isHeader() {
			line := fmt.Sprintf("  ── %s ──", strings.ToUpper(entry.sectionHeader))
			lineStyle := lipgloss.NewStyle().Background(bg).Foreground(headerFg).Bold(true)
			b.WriteString(lineStyle.Width(popupWidth).Render(line))
			b.WriteString("\n")
			continue
		}
		if entry.provider == nil {
			continue
		}

		status := entry.statusText
		line := fmt.Sprintf("    %s  (%s)", entry.provider.Name, entry.detailText)
		line += "  " + status
		if isSelected {
			line = "  > " + line[4:]
		}

		lineStyle := lipgloss.NewStyle().Background(bg).Foreground(dimFg)
		if entry.configured {
			lineStyle = lineStyle.Foreground(configuredFg)
		}
		if isSelected {
			lineStyle = lipgloss.NewStyle().Background(selectedBg).Foreground(lipgloss.Color("15"))
		}
		b.WriteString(lineStyle.Width(popupWidth).Render(line))
		b.WriteString("\n")
	}

	footer := lipgloss.NewStyle().Background(bg).Foreground(dimFg).Width(popupWidth).
		Render(fmt.Sprintf("  %d/%d", pk.selected+1, len(pk.entries)))
	b.WriteString(footer)
	b.WriteString("\n")

	return style.Render(b.String())
}
