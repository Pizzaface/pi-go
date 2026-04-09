package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m *model) hasBlockingPopup() bool {
	return m.modelPicker != nil || m.loginPicker != nil
}

func (m *model) reservedOverlayLines() int {
	reserved := 0
	if m.branchPopup != nil {
		reserved += m.branchPopup.height + 6
	}
	if m.modelPicker != nil {
		reserved += m.modelPicker.height + 6
	}
	if m.loginPicker != nil {
		reserved += m.loginPicker.height + 6
	}
	return reserved
}

func (m *model) View() tea.View {
	if m.quitting {
		os.Exit(0)
	}

	if m.width == 0 {
		return tea.NewView("Loading...")
	}

	debugTraceWidth := m.debugTraceWidth()
	mainWidth := m.layoutMainWidth()

	messagesView := m.chatModel.RenderMessages(m.running)
	statusBar := padBlockWidth(m.statusModel.Render(m.statusRenderInput()), mainWidth)
	inputArea := padBlockWidth(m.inputModel.View(m.running || m.loading), mainWidth)
	widgetAbove := padBlockWidth(m.renderExtensionWidget(m.extensionWidgetAbove), mainWidth)
	widgetBelow := padBlockWidth(m.renderExtensionWidget(m.extensionWidgetBelow), mainWidth)

	statusLines := strings.Count(statusBar, "\n") + 1
	inputLines := strings.Count(inputArea, "\n") + 1
	widgetAboveLines := 0
	if widgetAbove != "" {
		widgetAboveLines = strings.Count(widgetAbove, "\n") + 1
	}
	widgetBelowLines := 0
	if widgetBelow != "" {
		widgetBelowLines = strings.Count(widgetBelow, "\n") + 1
	}

	queueIndicatorLines := 0
	if m.running && m.renderQueueIndicator() != "" {
		queueIndicatorLines = 1
	}

	availableHeight := m.height - statusLines - inputLines - 2
	availableHeight -= widgetAboveLines + widgetBelowLines + queueIndicatorLines
	if m.chatModel.Scroll > 0 {
		availableHeight--
	}
	availableHeight -= m.reservedOverlayLines()
	if availableHeight < 1 {
		availableHeight = 1
	}

	msgLines := strings.Split(messagesView, "\n")
	totalLines := len(msgLines)
	startLine := totalLines - availableHeight - m.chatModel.Scroll
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + availableHeight
	if endLine > totalLines {
		endLine = totalLines
	}

	visibleMessages := strings.Join(msgLines[startLine:endLine], "\n")
	visibleLineCount := strings.Count(visibleMessages, "\n") + 1
	for visibleLineCount < availableHeight {
		visibleMessages += "\n"
		visibleLineCount++
	}

	chatStyle := lipgloss.NewStyle().Width(mainWidth)
	visibleMessages = chatStyle.Render(visibleMessages)
	if m.slashOverlay != nil {
		maxOverlayHeight := availableHeight - 1
		if maxOverlayHeight < 1 {
			m.slashOverlay = nil
		} else {
			if m.slashOverlay.Height <= 0 || m.slashOverlay.Height > maxOverlayHeight {
				m.slashOverlay.Height = minInt(8, maxOverlayHeight)
			}
			m.slashOverlay.EnsureSelectionVisible()
			if !m.slashOverlay.HasVisibleSelectableRow() {
				m.slashOverlay = nil
			} else {
				visibleMessages = overlaySlashCommandBlock(visibleMessages, m.slashOverlay.render(mainWidth))
			}
		}
	}
	if m.extensionDialog != nil {
		visibleMessages = overlaySlashCommandBlock(visibleMessages, m.renderExtensionDialog(mainWidth))
	}

	var b strings.Builder
	b.WriteString(visibleMessages)
	b.WriteString("\n")
	if m.branchPopup != nil {
		b.WriteString(m.renderBranchPopup())
		b.WriteString("\n")
	}
	if m.modelPicker != nil {
		b.WriteString(m.renderModelPicker())
		b.WriteString("\n")
	}
	if m.loginPicker != nil {
		b.WriteString(m.renderLoginPicker())
		b.WriteString("\n")
	}
	if m.chatModel.Scroll > 0 {
		scrollDim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		scrollIndicator := scrollDim.Render(fmt.Sprintf(" ↑ scrolled %d lines (PgDn to return)", m.chatModel.Scroll))
		b.WriteString(scrollIndicator)
		b.WriteString("\n")
	}

	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	b.WriteString(sepStyle.Render(strings.Repeat("─", mainWidth)))
	b.WriteString("\n")
	if widgetAbove != "" {
		b.WriteString(widgetAbove)
		b.WriteString("\n")
	}
	b.WriteString(inputArea)
	if widgetBelow != "" {
		b.WriteString("\n")
		b.WriteString(widgetBelow)
	}
	b.WriteString("\n")

	// Queue indicator for steering/follow-up messages.
	if m.running {
		queueLine := m.renderQueueIndicator()
		if queueLine != "" {
			b.WriteString(queueLine)
			b.WriteString("\n")
		}
	}
	b.WriteString(statusBar)

	leftPanel := b.String()
	if m.cfg.Screen != nil {
		m.cfg.Screen.update(visibleMessages)
	}

	var final string
	if m.debugPanel && debugTraceWidth > 0 {
		traceContent := m.chatModel.RenderTracePanel(debugTraceWidth-4, m.height)
		traceLines := strings.Split(traceContent, "\n")
		if len(traceLines) > m.height {
			traceLines = traceLines[len(traceLines)-m.height:]
		}
		for len(traceLines) < m.height {
			traceLines = append(traceLines, "")
		}
		traceContent = strings.Join(traceLines, "\n")

		borderFg := lipgloss.Color("245")
		traceBox := lipgloss.NewStyle().
			Width(debugTraceWidth).
			BorderStyle(lipgloss.Border{Left: "│"}).
			BorderLeft(true).
			BorderForeground(borderFg)

		final = lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, traceBox.Render(traceContent))
	} else {
		final = leftPanel
	}

	if m.setupAlert {
		final = overlaySetupAlert(final, m.width, m.height)
	}

	v := tea.NewView(final)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) eyes() string {
	if m.face != nil {
		return m.face.Eyes()
	}
	return MoodIdle.Eyes()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderQueueIndicator renders a status line showing queued steering/follow-up counts.
func (m *model) renderQueueIndicator() string {
	sc := m.messageQueue.SteeringCount()
	fc := m.messageQueue.FollowUpCount()
	if sc == 0 && fc == 0 {
		return ""
	}

	steerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	followStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("183"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var parts []string
	if sc > 0 {
		parts = append(parts, steerStyle.Render(fmt.Sprintf("⚡%d steering", sc)))
	}
	if fc > 0 {
		parts = append(parts, followStyle.Render(fmt.Sprintf("📋%d follow-up", fc)))
	}

	return dimStyle.Render("  queued: ") + strings.Join(parts, dimStyle.Render(" · "))
}

func (m *model) debugTraceWidth() int {
	if !m.debugPanel {
		return 0
	}
	width := m.width / 2
	if width < 40 {
		width = 40
	}
	if width > m.width-30 {
		width = m.width - 30
	}
	if width < 0 {
		width = 0
	}
	return width
}

func (m *model) layoutMainWidth() int {
	mainWidth := m.width
	if m.debugPanel {
		mainWidth -= m.debugTraceWidth()
	}
	if mainWidth < 20 {
		mainWidth = 20
	}
	return mainWidth
}

func (m *model) statusRenderInput() StatusRenderInput {
	return StatusRenderInput{
		ProviderName:    m.cfg.ProviderName,
		ModelName:       m.cfg.ModelName,
		Running:         m.running,
		EffortLevel:     m.effortLevel.String(),
		Messages:        m.chatModel.Messages,
		TokenTracker:    m.cfg.TokenTracker,
		DiffAdded:       m.diffAdded,
		DiffRemoved:     m.diffRemoved,
		LoadingItems:    m.loadingItems,
		ExtensionStatus: m.statusModel.ExtensionStatus,
	}
}
