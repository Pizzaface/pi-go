package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	glamourstyles "github.com/charmbracelet/glamour/styles"

	"charm.land/lipgloss/v2"
)

// renderWelcome builds the startup welcome screen.
func renderWelcome() string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	cmd := lipgloss.NewStyle().Foreground(lipgloss.Color("75"))

	lines := []string{
		accent.Render("Welcome to go-pi") + dim.Render(" — a minimal coding agent harness"),
		"",
		dim.Render("Describe a task, ask a question, or point me at code to change."),
		dim.Render(`  "research this codebase and explain the architecture"`),
		dim.Render(`  "fix the failing test in auth_test.go"`),
		dim.Render(`  "add error handling to the upload endpoint"`),
		dim.Render(`  "explain how the session middleware works"`),
		dim.Render(`  "refactor this function to use channels"`),
		"",
		dim.Render("Commands: ") +
			cmd.Render("/help") + dim.Render(" ") +
			cmd.Render("/resume") + dim.Render(" ") +
			cmd.Render("/fork") + dim.Render(" ") +
			cmd.Render("/settings"),
		dim.Render("Press ") + cmd.Render("Tab") + dim.Render(" to cycle commands, ") +
			cmd.Render("@") + dim.Render(" to mention files"),
	}
	return strings.Join(lines, "\n")
}

// message represents a chat message in the conversation.
type message struct {
	role           string // "user", "assistant", or "tool"
	content        string
	isWarning      bool   // if true, render with warning style
	tool           string // tool name (for role=="tool")
	toolIn         string // tool input args (for role=="tool")
	extensionOwner string // optional extension id for custom render surfaces
	collapsed      bool   // per-message collapse state (used by Agent accordion)
	agentGroupID   int    // >0 means this tool belongs to an Agent invocation group
}

// traceEntry represents a single entry in the debug trace log.
type traceEntry struct {
	time    time.Time
	kind    string // "llm", "tool_call", "tool_result", "error"
	summary string // short one-line summary
	detail  string // full content (args, response, etc.)
}

// agentLineRange maps a range of rendered lines to an Agent tool message index.
// Used by the mouse click handler to toggle Agent accordions.
type agentLineRange struct {
	startLine int // first line of the Agent panel (inclusive)
	endLine   int // last line of the Agent panel (exclusive)
	msgIndex  int // index into ChatModel.Messages
}

// ChatModel manages the conversation message display, scrolling, and markdown rendering.
type ChatModel struct {
	Messages         []message
	Scroll           int // scroll offset from bottom
	Streaming        string
	Thinking         string
	Renderer         *glamour.TermRenderer
	TraceLog         []traceEntry
	Width            int
	ToolDisplay      ToolDisplayModel
	RenderTimeout    time.Duration
	// AgentLineRanges tracks rendered line ranges for Agent tool accordions.
	// Populated by RenderMessages(), consumed by mouse click handler.
	AgentLineRanges []agentLineRange
	// mdCache memoizes glamour output by source text. Cleared on width change
	// (output depends on wrap width) and on Clear(). Kept under mdCacheCap to
	// bound memory during long streaming sessions.
	mdCache map[string]string
	// agentRespCache memoizes the post-markdown ANSI-patched Agent response
	// body (indent + bg shading + padding). Same invalidation rules as mdCache.
	agentRespCache map[agentRespKey]string
}

type agentRespKey struct {
	content    string
	panelWidth int
}

const (
	mdCacheCap        = 512
	agentRespCacheCap = 256
)

// NewChatModel creates a ChatModel with the given markdown renderer.
func NewChatModel(renderer *glamour.TermRenderer) ChatModel {
	return ChatModel{
		Messages:      make([]message, 0),
		Renderer:      renderer,
		RenderTimeout: 250 * time.Millisecond,
	}
}

// Clear removes all messages and resets scroll.
func (c *ChatModel) Clear() {
	c.Messages = c.Messages[:0]
	c.Scroll = 0
	c.mdCache = nil
	c.agentRespCache = nil
}

// AppendWarning adds a warning message styled with yellow text.
func (c *ChatModel) AppendWarning(text string) {
	c.Messages = append(c.Messages, message{
		role:      "assistant",
		content:   text,
		isWarning: true,
	})
	c.Scroll = 0
}

// ResetScroll resets the scroll offset to bottom.
func (c *ChatModel) ResetScroll() {
	c.Scroll = 0
}

// ScrollUp scrolls up by n lines, clamped to max.
func (c *ChatModel) ScrollUp(n, height int) {
	c.Scroll += n
	maxScroll := c.MaxScroll(height)
	if c.Scroll > maxScroll {
		c.Scroll = maxScroll
	}
}

// ScrollDown scrolls down by n lines, clamped to 0.
func (c *ChatModel) ScrollDown(n int) {
	c.Scroll -= n
	if c.Scroll < 0 {
		c.Scroll = 0
	}
}

// MaxScroll returns the maximum scroll offset for the given terminal height.
func (c *ChatModel) MaxScroll(height int) int {
	if len(c.Messages) == 0 {
		return 0
	}
	messagesView := c.RenderMessages(false)
	if c.Width > 0 {
		messagesView = lipgloss.NewStyle().Width(c.Width).Render(messagesView)
	}
	totalLines := lipgloss.Height(messagesView)

	availableHeight := height - 3
	if availableHeight < 1 {
		return 0
	}
	maxLines := totalLines - availableHeight
	if maxLines < 0 {
		return 0
	}
	return maxLines
}

// UpdateRenderer recreates the glamour renderer for the given terminal width.
func (c *ChatModel) UpdateRenderer(width int) {
	c.Width = width
	contentWidth := width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}
	// Start from the dark style and override inline code colors —
	// the default uses color 203 (salmon/red) which clashes.
	style := glamourstyles.DarkStyleConfig
	codeColor := "252"
	codeBg := "238"
	style.Code.StylePrimitive.Color = &codeColor
	style.Code.StylePrimitive.BackgroundColor = &codeBg
	c.Renderer, _ = glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(contentWidth),
		glamour.WithEmoji(),
	)
	c.mdCache = nil
	c.agentRespCache = nil
	c.ToolDisplay.Width = width
}

// RenderMarkdown renders text as markdown using the glamour renderer.
func (c *ChatModel) RenderMarkdown(text string) string {
	if text == "" {
		return ""
	}
	if cached, ok := c.mdCache[text]; ok {
		return cached
	}
	if c.Renderer == nil {
		return text
	}
	rendered, err := c.Renderer.Render(text)
	if err != nil {
		return text
	}
	rendered = strings.Trim(rendered, "\n")
	rendered = trimRenderedMarkdownIndent(rendered)
	if c.mdCache == nil || len(c.mdCache) >= mdCacheCap {
		c.mdCache = make(map[string]string, mdCacheCap)
	}
	c.mdCache[text] = rendered
	return rendered
}

func trimRenderedMarkdownIndent(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = trimMarkdownLineIndent(line)
	}
	return strings.Join(lines, "\n")
}

func trimMarkdownLineIndent(line string) string {
	if line == "" {
		return line
	}

	i := 0
	for i < len(line) && line[i] == '\x1b' {
		j := i + 1
		if j >= len(line) || line[j] != '[' {
			break
		}
		j++
		for j < len(line) && ((line[j] >= '0' && line[j] <= '9') || line[j] == ';') {
			j++
		}
		if j >= len(line) || line[j] != 'm' {
			break
		}
		i = j + 1
	}

	trimmed := 0
	for trimmed < 2 && i < len(line) && line[i] == ' ' {
		i++
		trimmed++
	}
	return line[:i-trimmed] + line[i:]
}

// RenderMessages renders all messages into a string for display.
// It also populates AgentLineRanges for click-to-toggle accordion support.
func (c *ChatModel) RenderMessages(running bool) string {
	if len(c.Messages) == 0 {
		return renderWelcome()
	}
	c.ToolDisplay.RenderMarkdown = c.RenderMarkdown
	if c.ToolDisplay.RenderTimeout <= 0 {
		c.ToolDisplay.RenderTimeout = c.RenderTimeout
	}

	const assistantPrefix = "● "
	const userPrefix = "> "

	c.AgentLineRanges = c.AgentLineRanges[:0]
	lineCount := 0

	// Build a set of collapsed agent group IDs so child tools can be skipped.
	collapsedGroups := make(map[int]bool)
	for _, msg := range c.Messages {
		if isAgentTool(msg.tool) && msg.collapsed && msg.agentGroupID > 0 {
			collapsedGroups[msg.agentGroupID] = true
		}
	}

	// Count child tools per Agent group for header display.
	groupChildCounts := make(map[int]int)
	for _, msg := range c.Messages {
		if msg.role == "tool" && msg.agentGroupID > 0 && !isAgentTool(msg.tool) {
			groupChildCounts[msg.agentGroupID]++
		}
	}

	// write appends to the builder and tracks cumulative line count.
	var b strings.Builder
	write := func(s string) {
		b.WriteString(s)
		lineCount += strings.Count(s, "\n")
	}

	// Agent child border prefix for visual containment.
	agentBorderPrefix := " │ "

	// Deferred Agent response — rendered as an indented panel after children.
	type pendingAgentResp struct {
		groupID int
		content string
	}
	var pendingResp *pendingAgentResp

	// renderAgentResp writes an Agent's response as an indented, bg-styled
	// panel that visually belongs to the accordion group.
	renderAgentResp := func(content string) {
		if content == "" {
			return
		}
		panelWidth := c.ToolDisplay.Width
		if panelWidth < 24 {
			panelWidth = 24
		}
		key := agentRespKey{content: content, panelWidth: panelWidth}
		if cached, ok := c.agentRespCache[key]; ok {
			write(cached)
			write("\n")
			return
		}
		// Extract text from content-part wrappers (JSON or Go %v format).
		raw := content
		if extracted := extractContentPartsText(raw); extracted != "" {
			raw = extracted
		}
		rendered := c.RenderMarkdown(raw)
		if rendered == "" {
			rendered = raw
		}
		// Apply bg per-line, re-applying after ANSI resets from markdown
		// rendering so the background stretches to the full panel width.
		bgOn := "\x1b[48;5;238m"
		reset := "\x1b[0m"
		lines := strings.Split(rendered, "\n")
		for i, line := range lines {
			prefixed := agentBorderPrefix + line
			patched := strings.ReplaceAll(prefixed, reset, reset+bgOn)
			vis := lipgloss.Width(patched)
			if vis < panelWidth {
				patched += strings.Repeat(" ", panelWidth-vis)
			}
			lines[i] = bgOn + patched + reset
		}
		out := strings.Join(lines, "\n")
		if c.agentRespCache == nil || len(c.agentRespCache) >= agentRespCacheCap {
			c.agentRespCache = make(map[agentRespKey]string, agentRespCacheCap)
		}
		c.agentRespCache[key] = out
		write(out)
		write("\n")
	}

	prevRole := ""
	for i, msg := range c.Messages {
		// Flush deferred Agent response when leaving the group.
		if pendingResp != nil {
			isChild := msg.role == "tool" && msg.agentGroupID == pendingResp.groupID && !isAgentTool(msg.tool)
			if !isChild {
				renderAgentResp(pendingResp.content)
				prevRole = "tool" // response is part of the tool group
				pendingResp = nil
			}
		}

		// Skip child tools whose parent Agent accordion is collapsed.
		if msg.role == "tool" && msg.agentGroupID > 0 && !isAgentTool(msg.tool) && collapsedGroups[msg.agentGroupID] {
			continue
		}

		switch msg.role {
		case "user":
			if i > 0 {
				write("\n")
			}
			write(renderWrappedPrefixBlock(msg.content, userPrefix, c.Width))
			write("\n")

		case "tool":
			// Add spacing before the first tool in a sequence, not between tools.
			if prevRole != "tool" {
				write("\n")
			}
			// Set child count for Agent header rendering.
			if isAgentTool(msg.tool) {
				c.ToolDisplay.AgentChildCount = groupChildCounts[msg.agentGroupID]
			}
			isChild := msg.agentGroupID > 0 && !isAgentTool(msg.tool)
			if isChild {
				c.ToolDisplay.Width -= agentChildIndent
			}
			startLine := lineCount
			rendered := c.ToolDisplay.RenderToolMessage(msg)
			if isChild {
				c.ToolDisplay.Width += agentChildIndent
				rendered = indentBlock(rendered, agentBorderPrefix)
			}
			write(rendered)
			if isAgentTool(msg.tool) {
				c.AgentLineRanges = append(c.AgentLineRanges, agentLineRange{
					startLine: startLine,
					endLine:   lineCount,
					msgIndex:  i,
				})
				// Render the Agent's response below the accordion, indented.
				if msg.content != "" {
					if msg.collapsed {
						// Collapsed: render response immediately after header.
						renderAgentResp(msg.content)
						prevRole = "tool" // response is part of the tool group
						continue          // skip prevRole = msg.role below
					}
					if msg.agentGroupID > 0 {
						// Expanded: defer until after last child.
						pendingResp = &pendingAgentResp{
							groupID: msg.agentGroupID,
							content: msg.content,
						}
					}
				}
			}

		case "thinking":
			if msg.content != "" {
				thinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)
				thinkBullet := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("💭 ")
				write("\n")
				write(thinkBullet)
				// Show last few lines of thinking to keep it compact.
				lines := strings.Split(msg.content, "\n")
				maxLines := 6
				if len(lines) > maxLines {
					lines = lines[len(lines)-maxLines:]
				}
				for j, line := range lines {
					if j > 0 {
						write("   ")
					}
					write(thinkStyle.Render(line))
					if j < len(lines)-1 {
						write("\n")
					}
				}
				write("\n")
			}

		case "assistant":
			content := msg.content
			if content == "" && running && i == len(c.Messages)-1 {
				content = "..."
			}
			if content != "" {
				// Add extra spacing after a tool sequence before the assistant response.
				if prevRole == "tool" {
					write("\n")
				}
				write("\n")
				if msg.isWarning {
					warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
					warnContent := prefixBlockLines(warnStyle.Render(content), "⚠ ")
					write(warnContent)
				} else {
					rendered, ok := c.renderCustomAssistantMessage(msg, content)
					if !ok {
						rendered = c.RenderMarkdown(content)
					}
					write(prefixBlockLines(rendered, assistantPrefix))
				}
				write("\n")
			}
		}
		prevRole = msg.role
	}

	// Flush any remaining deferred Agent response at the end.
	if pendingResp != nil {
		renderAgentResp(pendingResp.content)
	}

	return b.String()
}

// renderCustomAssistantMessage is a spec #5 stub — extension-driven
// message rendering lands later. Always returns ("", false) so the
// default rendering path runs.
func (c *ChatModel) renderCustomAssistantMessage(msg message, content string) (string, bool) {
	_ = msg
	_ = content
	return "", false
}

// RenderTracePanel renders the debug trace log as a color-coded panel.
// Each entry shows a timestamp, kind icon, summary, and (truncated) detail.
func (c *ChatModel) RenderTracePanel(width, height int) string {
	if len(c.TraceLog) == 0 {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		return dim.Render("  No trace events yet. Send a message to start.")
	}

	// Style definitions.
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	llmStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	toolCallStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	toolResultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	requestStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	httpReqStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	httpRespStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	httpErrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("183")).Bold(true)

	maxDetail := width - 14 // timestamp (8) + icon (2) + padding (4)
	if maxDetail < 20 {
		maxDetail = 20
	}

	var lines []string

	// Header.
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).
		Bold(true).
		Render("  ─── Debug Trace (F12 to close) ───")
	lines = append(lines, header, "")

	for _, entry := range c.TraceLog {
		ts := timeStyle.Render(entry.time.Format("15:04:05"))

		var icon, summary string
		switch entry.kind {
		case "user_prompt":
			icon = userStyle.Render("📤")
			summary = userStyle.Render(entry.summary)
		case "request_sent":
			icon = requestStyle.Render("🚀")
			summary = requestStyle.Render(entry.summary)
		case "request_done":
			icon = requestStyle.Render("🏁")
			summary = requestStyle.Render(entry.summary)
		case "llm":
			icon = llmStyle.Render("🔵")
			summary = llmStyle.Render(entry.summary)
		case "http_request":
			icon = httpReqStyle.Render("🌐")
			summary = httpReqStyle.Render(entry.summary)
		case "http_response":
			icon = httpRespStyle.Render("📥")
			summary = httpRespStyle.Render(entry.summary)
		case "http_error":
			icon = httpErrStyle.Render("🛑")
			summary = httpErrStyle.Render(entry.summary)
		case "tool_call":
			icon = toolCallStyle.Render("🔧")
			summary = toolCallStyle.Render(entry.summary)
		case "tool_result":
			icon = toolResultStyle.Render("✅")
			summary = toolResultStyle.Render(entry.summary)
		case "error":
			icon = errorStyle.Render("❌")
			summary = errorStyle.Render(entry.summary)
		default:
			icon = dimStyle.Render("·")
			summary = dimStyle.Render(entry.summary)
		}

		line := fmt.Sprintf("  %s %s %s", ts, icon, summary)
		lines = append(lines, line)

		// Show truncated detail if present.
		if entry.detail != "" {
			detail := entry.detail
			// Collapse to first line for compact view.
			if idx := strings.IndexByte(detail, '\n'); idx >= 0 {
				detail = detail[:idx] + "…"
			}
			if len(detail) > maxDetail {
				detail = detail[:maxDetail-1] + "…"
			}
			lines = append(lines, "       "+dimStyle.Render(detail))
		}
	}

	// Join and return (caller handles height clipping).
	return strings.Join(lines, "\n")
}

// countByRole counts messages with the given role.
func countByRole(msgs []message, role string) int {
	n := 0
	for _, msg := range msgs {
		if msg.role == role {
			n++
		}
	}
	return n
}

// formatTokenCount formats a token count with K/M suffixes.
func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
