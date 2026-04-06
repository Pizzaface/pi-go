package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// overlaySetupAlert renders a centered modal box over the screen content.
func overlaySetupAlert(screen string, width, height int) string {
	title := "  No Models Configured  "
	body1 := "  Use /login <provider> to set up an API key.  "
	body2 := "                                                "
	body3 := "  Providers: anthropic, openai, gemini,         "
	body4 := "             groq, mistral, xai, openrouter     "
	footer := "          Press Enter to dismiss                "

	lines := []string{title, body2, body1, body2, body3, body4, body2, footer}
	maxW := 0
	for _, l := range lines {
		if len(l) > maxW {
			maxW = len(l)
		}
	}
	for i, l := range lines {
		if len(l) < maxW {
			lines[i] = l + strings.Repeat(" ", maxW-len(l))
		}
	}

	boxBg := lipgloss.Color("235")
	boxFg := lipgloss.Color("255")
	borderFg := lipgloss.Color("33")
	titleFg := lipgloss.Color("214")
	dimFg := lipgloss.Color("243")

	boxStyle := lipgloss.NewStyle().Background(boxBg).Foreground(boxFg).Padding(0, 1)
	titleStyle := lipgloss.NewStyle().Background(boxBg).Foreground(titleFg).Bold(true).Padding(0, 1)
	footerStyle := lipgloss.NewStyle().Background(boxBg).Foreground(dimFg).Padding(0, 1)

	topBorder := lipgloss.NewStyle().Foreground(borderFg).Render("╭" + strings.Repeat("─", maxW+2) + "╮")
	botBorder := lipgloss.NewStyle().Foreground(borderFg).Render("╰" + strings.Repeat("─", maxW+2) + "╯")
	sideBorderL := lipgloss.NewStyle().Foreground(borderFg).Render("│")
	sideBorderR := lipgloss.NewStyle().Foreground(borderFg).Render("│")

	var boxLines []string
	boxLines = append(boxLines, topBorder)
	for i, l := range lines {
		var styled string
		if i == 0 {
			styled = titleStyle.Render(l)
		} else if i == len(lines)-1 {
			styled = footerStyle.Render(l)
		} else {
			styled = boxStyle.Render(l)
		}
		boxLines = append(boxLines, sideBorderL+styled+sideBorderR)
	}
	boxLines = append(boxLines, botBorder)

	screenLines := strings.Split(screen, "\n")
	boxHeight := len(boxLines)
	startY := (height - boxHeight) / 2
	if startY < 0 {
		startY = 0
	}
	startX := (width - maxW - 4) / 2
	if startX < 0 {
		startX = 0
	}

	for i, boxLine := range boxLines {
		row := startY + i
		if row >= 0 && row < len(screenLines) {
			screenLines[row] = overlayLine(screenLines[row], boxLine, startX)
		}
	}

	return strings.Join(screenLines, "\n")
}

func overlayLine(orig, overlay string, startX int) string {
	padding := ""
	if startX > 0 {
		runes := []rune(orig)
		if startX < len(runes) {
			padding = string(runes[:startX])
		} else {
			padding = orig + strings.Repeat(" ", startX-len(runes))
		}
	}
	return padding + overlay
}

func overlaySlashCommandBlock(base, overlay string) string {
	if overlay == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	if len(overlayLines) > len(baseLines) {
		overlayLines = overlayLines[len(overlayLines)-len(baseLines):]
	}
	start := len(baseLines) - len(overlayLines)
	if start < 0 {
		start = 0
	}
	for i, line := range overlayLines {
		idx := start + i
		if idx >= 0 && idx < len(baseLines) && strings.TrimSpace(line) != "" {
			baseLines[idx] = line
		}
	}
	return strings.Join(baseLines, "\n")
}
