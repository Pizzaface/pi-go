package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// StatusModel manages the status bar display at the bottom of the TUI.
type StatusModel struct {
	// GitBranch is the current git branch (detected at startup).
	GitBranch string
	// ActiveTool is the name of the currently executing tool (single).
	ActiveTool string
	// ActiveTools tracks parallel tool execution: name → start time.
	ActiveTools map[string]time.Time
	// ToolStart is when the current single tool started.
	ToolStart time.Time
	// Width is the terminal width for rendering.
	Width int
}

// StatusRenderInput provides data from other models needed by the status bar.
type StatusRenderInput struct {
	ProviderName string
	ModelName    string
	Running      bool
	Mode         string       // e.g. "chat"
	Eyes         string       // mood eyes e.g. "◕ ◕"
	Messages     []message    // for context estimate
	TokenTracker TokenTracker // may be nil
	DiffAdded    int
	DiffRemoved  int
	LoadingItems map[string]bool // item -> done; nil means not loading
}

// contextBarWidth is the number of characters used for the visual context bar.
const contextBarWidth = 10

// renderContextBar returns a color-coded visual bar like "████░░░░░░ 42%".
// Colors: green < 60%, orange 60-80%, red > 80%.
func renderContextBar(pct float64, bg color.Color) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int(pct / 100 * contextBarWidth)
	if filled > contextBarWidth {
		filled = contextBarWidth
	}
	empty := contextBarWidth - filled

	var fg color.Color
	switch {
	case pct >= 80:
		fg = lipgloss.Color("196") // red
	case pct >= 60:
		fg = lipgloss.Color("214") // orange
	default:
		fg = lipgloss.Color("35") // green
	}

	filledStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	emptyStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("240"))
	pctStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)

	return filledStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty)) +
		pctStyle.Render(fmt.Sprintf(" %.0f%%", pct))
}

// Render renders the status bar string.
func (s *StatusModel) Render(in StatusRenderInput) string {
	bg := lipgloss.Color("236")
	fg := lipgloss.Color("252")
	dimFg := lipgloss.Color("246")

	bright := lipgloss.NewStyle().Background(bg).Foreground(fg)
	dim := lipgloss.NewStyle().Background(bg).Foreground(dimFg)
	bar := lipgloss.NewStyle().Background(bg).Width(s.Width)

	sep := dim.Render("  |  ")

	var parts []string

	// Mode indicator.
	mode := in.Mode
	if mode == "" {
		mode = "chat"
	}
	if mode == "plan" {
		modeStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214")) // orange/warning
		parts = append(parts, modeStyle.Render(fmt.Sprintf(" [%s]", mode)))
	} else {
		parts = append(parts, dim.Render(fmt.Sprintf(" [%s]", mode)))
	}

	// Provider | Model.
	if in.ProviderName != "" {
		parts = append(parts, bright.Render(fmt.Sprintf("%s | %s", in.ProviderName, in.ModelName)))
	} else {
		parts = append(parts, bright.Render(in.ModelName))
	}

	// Loading progress (replaces normal status content during init).
	if in.LoadingItems != nil {
		var items []string
		for _, name := range sortedKeys(in.LoadingItems) {
			if in.LoadingItems[name] {
				okStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("35")) // green
				items = append(items, okStyle.Render(name+" \u2713"))
			} else {
				loadStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214")) // orange
				items = append(items, loadStyle.Render(name+"..."))
			}
		}
		parts = append(parts, dim.Render("loading: ")+strings.Join(items, dim.Render(" ")))
		return bar.Render(strings.Join(parts, sep))
	}

	// Context % bar — prefer actual provider-reported context usage.
	if tt := in.TokenTracker; tt != nil && tt.ContextUsed() > 0 {
		ctxUsed := tt.ContextUsed()
		ctxLimit := tt.ContextLimit()
		if ctxLimit > 0 {
			pct := float64(ctxUsed) / float64(ctxLimit) * 100
			parts = append(parts, renderContextBar(pct, bg))
		} else {
			parts = append(parts, dim.Render(fmt.Sprintf("ctx: %s", formatTokenCount(ctxUsed))))
		}
	} else {
		// Fallback: rough context size estimate (~4 chars per token).
		ctxChars := 0
		for _, msg := range in.Messages {
			ctxChars += len(msg.content) + len(msg.tool) + len(msg.toolIn)
		}
		ctxTokens := ctxChars / 4
		switch {
		case ctxTokens >= 1000:
			parts = append(parts, dim.Render(fmt.Sprintf("ctx: ~%.1fk", float64(ctxTokens)/1000)))
		default:
			parts = append(parts, dim.Render(fmt.Sprintf("ctx: ~%d", ctxTokens)))
		}
	}

	// Token usage (numeric): daily aggregate from provider.
	if tt := in.TokenTracker; tt != nil {
		total := tt.TotalUsed()
		limit := tt.Limit()
		if limit > 0 {
			pct := tt.PercentUsed()
			var tokenStyle lipgloss.Style
			switch {
			case pct >= 100:
				tokenStyle = lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("196")) // red
			case pct >= 80:
				tokenStyle = lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214")) // orange
			default:
				tokenStyle = dim
			}
			parts = append(parts, tokenStyle.Render(fmt.Sprintf("tokens: %s/%s",
				formatTokenCount(total), formatTokenCount(limit))))
		} else if total > 0 {
			parts = append(parts, dim.Render(fmt.Sprintf("tokens: %s", formatTokenCount(total))))
		}
	}

	// Git branch.
	if s.GitBranch != "" {
		parts = append(parts, bright.Render(fmt.Sprintf("\u2387 %s", s.GitBranch)))
	}

	// Active tools or thinking status.
	if len(s.ActiveTools) > 1 {
		// Multiple tools running in parallel.
		var toolNames []string
		for name := range s.ActiveTools {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		parts = append(parts, bright.Render(fmt.Sprintf("tools[%d]: %s", len(toolNames), strings.Join(toolNames, ", "))))
	} else if s.ActiveTool != "" {
		elapsed := time.Since(s.ToolStart).Truncate(time.Millisecond)
		parts = append(parts, bright.Render(fmt.Sprintf("tool: %s (%s)", s.ActiveTool, elapsed)))
	} else if in.Running {
		parts = append(parts, dim.Render("thinking..."))
	}

	return bar.Render(strings.Join(parts, sep))
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
