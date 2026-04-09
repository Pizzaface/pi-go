package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// StatusModel manages the status bar display at the bottom of the TUI.
type StatusModel struct {
	// GitBranch is the current git branch (detected at startup).
	GitBranch string
	// ExtensionStatus is an optional extension-owned status text.
	ExtensionStatus string
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
	EffortLevel  string // current effort level label (e.g. "medium")

	Messages        []message    // for context estimate
	TokenTracker    TokenTracker // may be nil
	DiffAdded       int
	DiffRemoved     int
	LoadingItems    map[string]bool // item -> done; nil means not loading
	ExtensionStatus string
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

// padRow renders a left + right pair as a single full-width row with bg fill.
// Any remaining space between them is filled with bg-colored spaces.
func padRow(left, right string, width int, bg color.Color) string {
	leftW := ansi.StringWidth(left)
	rightW := ansi.StringWidth(right)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	fill := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap))
	return left + fill + right
}

// Render renders the footer as a two-row table: left-aligned and right-aligned
// content on each row, with a full-width background.
//
//	Row 1:  ⎇ branch (+N -N)                    • (provider) model • effort
//	Row 2:  ctx ████░░░░ 42%  tokens: 1.2k/5M         ⚡ tool (1.2s)
func (s *StatusModel) Render(in StatusRenderInput) string {
	bg := lipgloss.Color("236")
	fg := lipgloss.Color("252")
	dimFg := lipgloss.Color("246")

	bright := lipgloss.NewStyle().Background(bg).Foreground(fg)
	dim := lipgloss.NewStyle().Background(bg).Foreground(dimFg)
	accent := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("39"))
	dot := dim.Render(" • ")

	// --- Loading state (single row) ---
	if in.LoadingItems != nil {
		var items []string
		for _, name := range sortedKeys(in.LoadingItems) {
			if in.LoadingItems[name] {
				okStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("35"))
				items = append(items, okStyle.Render(name+" ✓"))
			} else {
				loadStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214"))
				items = append(items, loadStyle.Render(name+"..."))
			}
		}
		content := dim.Render(" loading: ") + strings.Join(items, dim.Render(" "))
		return padRow(content, "", s.Width, bg)
	}

	// ═══════════════════════════════════════════════════
	// Row 1: branch/diff (left)  ·  provider model effort (right)
	// ═══════════════════════════════════════════════════

	var row1Left []string
	var row1Right []string

	// Left: git branch + diff stats.
	if s.GitBranch != "" {
		branchStr := accent.Render(fmt.Sprintf(" ⎇ %s", s.GitBranch))
		row1Left = append(row1Left, branchStr)
	}
	if in.DiffAdded > 0 || in.DiffRemoved > 0 {
		var diffParts []string
		if in.DiffAdded > 0 {
			addStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("35"))
			diffParts = append(diffParts, addStyle.Render(fmt.Sprintf("+%d", in.DiffAdded)))
		}
		if in.DiffRemoved > 0 {
			delStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("196"))
			diffParts = append(diffParts, delStyle.Render(fmt.Sprintf("-%d", in.DiffRemoved)))
		}
		row1Left = append(row1Left, strings.Join(diffParts, dim.Render(" ")))
	}

	// Right: provider · model · effort.
	if in.ProviderName != "" {
		row1Right = append(row1Right, dim.Render(fmt.Sprintf("(%s)", in.ProviderName)))
	}
	if in.ModelName != "" {
		row1Right = append(row1Right, bright.Render(in.ModelName))
	}
	if in.EffortLevel != "" && in.EffortLevel != "medium" {
		effortStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("183"))
		row1Right = append(row1Right, effortStyle.Render(in.EffortLevel))
	}

	r1Left := strings.Join(row1Left, dim.Render("  "))
	r1Right := strings.Join(row1Right, dot)
	if r1Right != "" {
		r1Right += " "
	}

	// ═══════════════════════════════════════════════════
	// Row 2: context + tokens (left)  ·  activity (right)
	// ═══════════════════════════════════════════════════

	var row2Left []string
	var row2Right []string

	// Left: context bar + token usage.
	if tt := in.TokenTracker; tt != nil && tt.ContextUsed() > 0 {
		ctxUsed := tt.ContextUsed()
		ctxLimit := tt.ContextLimit()
		if ctxLimit > 0 {
			pct := float64(ctxUsed) / float64(ctxLimit) * 100
			row2Left = append(row2Left, " "+renderContextBar(pct, bg))
		} else {
			row2Left = append(row2Left, dim.Render(fmt.Sprintf(" ctx: %s", formatTokenCount(ctxUsed))))
		}
	} else if len(in.Messages) > 0 {
		// Fallback: rough context size estimate (~4 chars per token).
		ctxChars := 0
		for _, msg := range in.Messages {
			ctxChars += len(msg.content) + len(msg.tool) + len(msg.toolIn)
		}
		ctxTokens := ctxChars / 4
		if ctxTokens >= 1000 {
			row2Left = append(row2Left, dim.Render(fmt.Sprintf(" ctx: ~%.1fk", float64(ctxTokens)/1000)))
		} else if ctxTokens > 0 {
			row2Left = append(row2Left, dim.Render(fmt.Sprintf(" ctx: ~%d", ctxTokens)))
		}
	}

	if tt := in.TokenTracker; tt != nil {
		total := tt.TotalUsed()
		limit := tt.Limit()
		if limit > 0 {
			pct := tt.PercentUsed()
			var tokenStyle lipgloss.Style
			switch {
			case pct >= 100:
				tokenStyle = lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("196"))
			case pct >= 80:
				tokenStyle = lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("214"))
			default:
				tokenStyle = dim
			}
			row2Left = append(row2Left, tokenStyle.Render(fmt.Sprintf("%s/%s",
				formatTokenCount(total), formatTokenCount(limit))))
		} else if total > 0 {
			row2Left = append(row2Left, dim.Render(formatTokenCount(total)))
		}
	}

	// Right: active tool / thinking status + extension status.
	if len(s.ActiveTools) > 1 {
		var toolNames []string
		for name := range s.ActiveTools {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		toolStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("35"))
		row2Right = append(row2Right, toolStyle.Render(fmt.Sprintf("⚡ %s", strings.Join(toolNames, ", "))))
	} else if s.ActiveTool != "" {
		elapsed := time.Since(s.ToolStart).Truncate(time.Millisecond)
		toolStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("35"))
		row2Right = append(row2Right, toolStyle.Render(fmt.Sprintf("⚡ %s", s.ActiveTool))+dim.Render(fmt.Sprintf(" %s", elapsed)))
	} else if in.Running {
		row2Right = append(row2Right, dim.Render("thinking..."))
	}
	if ext := strings.TrimSpace(in.ExtensionStatus); ext != "" {
		row2Right = append(row2Right, dim.Render(ext))
	}

	r2Left := strings.Join(row2Left, dot)
	r2Right := strings.Join(row2Right, dot)
	if r2Right != "" {
		r2Right += " "
	}

	return padRow(r1Left, r1Right, s.Width, bg) + "\n" + padRow(r2Left, r2Right, s.Width, bg)
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
