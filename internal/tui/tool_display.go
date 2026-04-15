package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"charm.land/lipgloss/v2"
)

// spinnerFrames are the braille-dot frames for the Agent active spinner.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ToolDisplayModel manages the formatting and rendering of tool call/result
// messages in the chat view. It owns per-tool formatters, syntax highlighting,
// and summary generation.
type ToolDisplayModel struct {
	// Width is the terminal width for rendering.
	Width int
	// CompactTools when true shows one-line summaries instead of full output.
	CompactTools bool
	// CollapsedTools when true keeps tool headers visible but hides result bodies.
	CollapsedTools bool
	// RenderTimeout bounds extension renderer calls.
	RenderTimeout time.Duration
	// RenderMarkdown renders markdown payloads when extensions return markdown.
	RenderMarkdown func(string) string
	// AgentChildCount is the number of child tool calls for the current Agent group.
	AgentChildCount int
	// SpinnerFrame is the current animation frame for Agent active spinners.
	SpinnerFrame int
}

// isAgentTool returns true if the tool name represents a sub-agent invocation.
func isAgentTool(name string) bool {
	return strings.EqualFold(name, "agent")
}

// RenderToolMessage renders a tool message (role=="tool") into a styled string.
// When CompactTools is true, renders a one-line summary instead of full output.
// Agent tools render as individually collapsible accordions.
func (t *ToolDisplayModel) RenderToolMessage(msg message) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	if t.CompactTools {
		return t.renderCompactTool(msg, dim)
	}
	if isAgentTool(msg.tool) {
		return t.renderAgentTool(msg, dim)
	}
	if t.CollapsedTools {
		return t.renderCollapsedTool(msg, dim)
	}
	return t.renderRegularTool(msg, dim)
}

// renderCompactTool renders a one-line tally for a tool message.
func (t *ToolDisplayModel) renderCompactTool(msg message, dim lipgloss.Style) string {
	if rendered, ok := t.renderWithExtension(msg, "tool_call_row", map[string]any{
		"compact": true,
		"tool":    msg.tool,
		"tool_in": msg.toolIn,
		"content": msg.content,
	}); ok {
		return ensureTrailingNewline(rendered)
	}

	toolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35")).Bold(true)
	checkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	toolBullet := lipgloss.NewStyle().Foreground(lipgloss.Color("35")).Bold(true).Render("● ")

	var b strings.Builder
	b.WriteString(toolBullet)
	b.WriteString(toolStyle.Render(msg.tool))

	if msg.toolIn != "" {
		args := msg.toolIn
		if len(args) > 60 {
			args = args[:57] + "..."
		}
		b.WriteString(dim.Render("("))
		b.WriteString(dim.Render(args))
		b.WriteString(dim.Render(")"))
	}

	if msg.content != "" {
		summary := toolResultSummary(msg.content)
		if len(summary) > 60 {
			summary = summary[:57] + "..."
		}
		// Show only the first line of the summary.
		if idx := strings.IndexByte(summary, '\n'); idx >= 0 {
			summary = summary[:idx]
		}
		b.WriteString(" ")
		b.WriteString(checkStyle.Render("✓ "))
		b.WriteString(dim.Render(summary))
	}

	b.WriteString("\n")
	return b.String()
}

func (t *ToolDisplayModel) renderCollapsedTool(msg message, dim lipgloss.Style) string {
	return t.renderTool(msg, dim, true)
}

// agentChildIndent is the number of columns child tools are indented under
// their parent Agent accordion.
const agentChildIndent = 3

// renderAgentTool renders an Agent tool call as a collapsible accordion header.
// Uses a chevron (▶/▼) and purple accent to distinguish from regular tools.
// Shows an animated spinner when the agent is still running (no result yet).
// The Agent's response content is rendered separately by RenderMessages as a
// conversation message after the child tools, not inside the accordion.
func (t *ToolDisplayModel) renderAgentTool(msg message, _ lipgloss.Style) string {
	panelWidth := t.Width
	if panelWidth < 24 {
		panelWidth = 24
	}
	innerWidth := panelWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	agentColor := lipgloss.Color("228") // light yellow
	panelStyle := lipgloss.NewStyle().Background(lipgloss.Color("236")).Padding(0, 1)
	headerStyle := lipgloss.NewStyle().Foreground(agentColor).Bold(true)

	chevron := "▶"
	if !msg.collapsed {
		chevron = "▼"
	}

	headerText := chevron + " " + msg.tool
	if t.AgentChildCount > 0 {
		label := "tool calls"
		if t.AgentChildCount == 1 {
			label = "tool call"
		}
		headerText += fmt.Sprintf(" (%d %s)", t.AgentChildCount, label)
	}
	if msg.toolIn != "" {
		headerText += " — " + msg.toolIn
	}
	header := headerStyle.Render(wrapPlainText(headerText, innerWidth))

	// Animated spinner when the agent has no result yet.
	if msg.content == "" {
		frame := spinnerFrames[t.SpinnerFrame%len(spinnerFrames)]
		activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
		header += " " + activeStyle.Render(frame)
	}

	return panelStyle.Width(panelWidth).Render(header) + "\n"
}

// renderRegularTool renders a standard tool message with name, args, and
// syntax-highlighted output.
func (t *ToolDisplayModel) renderRegularTool(msg message, dim lipgloss.Style) string {
	return t.renderTool(msg, dim, false)
}

func (t *ToolDisplayModel) renderTool(msg message, dim lipgloss.Style, collapsed bool) string {
	customHeader, customHeaderOK := t.renderWithExtension(msg, "tool_call_row", map[string]any{
		"compact": false,
		"tool":    msg.tool,
		"tool_in": msg.toolIn,
	})
	customResult, customResultOK := t.renderWithExtension(msg, "tool_result", map[string]any{
		"tool":    msg.tool,
		"tool_in": msg.toolIn,
		"content": msg.content,
	})

	panelWidth := t.Width
	if panelWidth < 24 {
		panelWidth = 24
	}
	innerWidth := panelWidth - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

	panelStyle := lipgloss.NewStyle().Background(lipgloss.Color("236")).Padding(0, 1)
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("35")).Bold(true)
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	header := strings.TrimRight(customHeader, "\n")
	if !customHeaderOK {
		header = "● " + msg.tool
		if msg.toolIn != "" {
			header += " (" + msg.toolIn + ")"
		}
		header = headerStyle.Render(wrapPlainText(header, innerWidth))
	}
	if customHeaderOK {
		header = wrapPlainText(header, innerWidth)
	}

	sections := []string{header}
	body := ""
	if !collapsed && msg.content != "" {
		body = t.renderToolBody(msg, dim, customResult, customResultOK, innerWidth)
	}
	if body != "" {
		sections = append(sections, dividerStyle.Render(strings.Repeat("─", innerWidth)), body)
	}

	return panelStyle.Width(panelWidth).Render(strings.Join(sections, "\n")) + "\n"
}

func (t *ToolDisplayModel) renderToolBody(msg message, dim lipgloss.Style, customResult string, customResultOK bool, width int) string {
	if customResultOK {
		var lines []string
		for _, line := range strings.Split(customResult, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, prefixBlockLines(line, "│ "))
		}
		return strings.Join(lines, "\n")
	}

	content := msg.content
	lines := strings.Split(content, "\n")
	maxLines := 15
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... (%d more lines)", len(lines)-maxLines))
	}
	var styled []string
	switch {
	case msg.tool == "read" && msg.toolIn != "":
		styled = highlightReadOutput(lines, msg.toolIn)
	case msg.tool == "grep":
		styled = highlightGrepOutput(lines)
	case msg.tool == "find":
		styled = highlightFindOutput(lines)
	}
	if styled != nil {
		for i, line := range styled {
			styled[i] = prefixBlockLines(line, "│ ")
		}
		return strings.Join(styled, "\n")
	}

	bodyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped := renderWrappedPrefixBlock(line, "│ ", width)
		bodyLines = append(bodyLines, dim.Render(wrapped))
	}
	return strings.Join(bodyLines, "\n")
}

// renderWithExtension is a spec #5 stub — no extension renderers exist in
// spec #1. Returns ("", false) so callers fall back to the default TUI
// rendering path.
func (t *ToolDisplayModel) renderWithExtension(
	msg message,
	surface string,
	payload map[string]any,
) (string, bool) {
	_ = msg
	_ = surface
	_ = payload
	return "", false
}

func ensureTrailingNewline(content string) string {
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

// toolCallSummary returns a short one-line summary of tool arguments.
func toolCallSummary(name string, args map[string]any) string {
	switch name {
	case "read":
		if fp, ok := args["file_path"].(string); ok {
			return fp
		}
	case "write":
		if fp, ok := args["file_path"].(string); ok {
			return fp
		}
	case "edit":
		if fp, ok := args["file_path"].(string); ok {
			return fp
		}
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			if len(cmd) > 80 {
				cmd = cmd[:77] + "..."
			}
			return cmd
		}
	case "grep":
		if p, ok := args["pattern"].(string); ok {
			return p
		}
	case "find":
		if p, ok := args["pattern"].(string); ok {
			return p
		}
	case "ls":
		if p, ok := args["path"].(string); ok {
			return p
		}
		return "."
	case "tree":
		p, _ := args["path"].(string)
		if p == "" {
			p = "."
		}
		if d, ok := args["depth"].(float64); ok && d > 0 {
			return fmt.Sprintf("%s (depth %d)", p, int(d))
		}
		return p
	case "Agent", "agent":
		if desc, ok := args["description"].(string); ok {
			return desc
		}
		if subType, ok := args["subagent_type"].(string); ok {
			return subType
		}
		if prompt, ok := args["prompt"].(string); ok {
			if len(prompt) > 80 {
				prompt = prompt[:77] + "..."
			}
			return prompt
		}
	}
	return ""
}

// toolResultSummary returns a short one-line summary of a tool result.
func toolResultSummary(content string) string {
	// Try to parse as JSON object and extract a friendly summary.
	var data map[string]any
	if json.Unmarshal([]byte(content), &data) == nil {
		return formatToolResult(data)
	}
	// Try to parse as JSON array of content parts (e.g., Agent/LLM results).
	if text := extractContentPartsText(content); text != "" {
		return text
	}
	// Collapse to single line.
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 120 {
		return content[:117] + "..."
	}
	return content
}

// extractContentPartsText extracts concatenated text from content-part
// formats returned by Agent/LLM tools. Handles both:
//   - JSON array:  [{"text":"...","type":"text"}]
//   - Go %v format: [map[text:... type:text]]
//
// Returns "" if the content is not in a recognized format.
func extractContentPartsText(content string) string {
	// Try JSON array first.
	var parts []map[string]any
	if json.Unmarshal([]byte(content), &parts) == nil {
		var texts []string
		for _, part := range parts {
			if t, ok := part["text"].(string); ok && t != "" {
				texts = append(texts, t)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}

	// Try Go %v format: [map[text:... type:text]]
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "[map[") {
		return extractGoMapTexts(trimmed)
	}

	return ""
}

// extractGoMapTexts parses Go's fmt %v representation of []map[string]any
// containing content parts with "text" and "type" keys.
func extractGoMapTexts(content string) string {
	var texts []string
	remaining := content

	for {
		start := strings.Index(remaining, "map[text:")
		if start < 0 {
			break
		}
		// Move past "map[text:"
		inner := remaining[start+len("map[text:"):]

		// Find the closing boundary: " type:" followed by the value and "]"
		// Use the LAST " type:" before the next "]" to handle text that
		// might contain those characters.
		closeBracket := strings.Index(inner, "]")
		if closeBracket < 0 {
			break
		}
		mapContent := inner[:closeBracket]
		typeIdx := strings.LastIndex(mapContent, " type:")
		if typeIdx >= 0 {
			texts = append(texts, mapContent[:typeIdx])
		} else {
			texts = append(texts, mapContent)
		}
		remaining = inner[closeBracket+1:]
	}

	if len(texts) == 0 {
		return ""
	}
	return strings.Join(texts, "\n")
}

// formatToolResult extracts a readable summary from a parsed tool result.
func formatToolResult(data map[string]any) string {
	// ls tool: show file/dir names
	if entries, ok := data["entries"].([]any); ok {
		var names []string
		for _, e := range entries {
			if m, ok := e.(map[string]any); ok {
				name, _ := m["name"].(string)
				if isDir, ok := m["is_dir"].(bool); ok && isDir {
					name += "/"
				}
				names = append(names, name)
			}
		}
		result := strings.Join(names, "  ")
		if len(result) > 120 {
			return result[:117] + "..."
		}
		return result
	}
	// tree tool: show dirs/files count
	if _, ok := data["tree"].(string); ok {
		d, _ := data["dirs"].(float64)
		f, _ := data["files"].(float64)
		return fmt.Sprintf("%d dirs, %d files", int(d), int(f))
	}
	// grep tool: show matches with file:line: content
	if matchList, ok := data["matches"].([]any); ok {
		total, _ := data["total_matches"].(float64)
		trunc, _ := data["truncated"].(bool)
		var sb strings.Builder
		for _, m := range matchList {
			if entry, ok := m.(map[string]any); ok {
				file, _ := entry["file"].(string)
				line, _ := entry["line"].(float64)
				content, _ := entry["content"].(string)
				fmt.Fprintf(&sb, "%s:%d: %s\n", file, int(line), content)
			}
		}
		if trunc {
			fmt.Fprintf(&sb, "... (%d total matches, truncated)", int(total))
		}
		return strings.TrimRight(sb.String(), "\n")
	}
	if matches, ok := data["total_matches"].(float64); ok {
		return fmt.Sprintf("%d matches", int(matches))
	}
	// find tool: show file list
	if fileList, ok := data["files"].([]any); ok {
		total, _ := data["total_files"].(float64)
		trunc, _ := data["truncated"].(bool)
		var sb strings.Builder
		for _, f := range fileList {
			if name, ok := f.(string); ok {
				sb.WriteString(name)
				sb.WriteByte('\n')
			}
		}
		if trunc {
			fmt.Fprintf(&sb, "... (%d total files, truncated)", int(total))
		}
		return strings.TrimRight(sb.String(), "\n")
	}
	if total, ok := data["total_files"].(float64); ok {
		return fmt.Sprintf("%d files", int(total))
	}
	// read tool: show actual content with line numbers
	if content, ok := data["content"].(string); ok {
		total, _ := data["total_lines"].(float64)
		trunc, _ := data["truncated"].(bool)
		if trunc {
			content += fmt.Sprintf("\n... (%d total lines, truncated)", int(total))
		}
		return content
	}
	if total, ok := data["total_lines"].(float64); ok {
		trunc := ""
		if t, ok := data["truncated"].(bool); ok && t {
			trunc = " (truncated)"
		}
		return fmt.Sprintf("%d lines%s", int(total), trunc)
	}
	// write tool: show bytes written
	if bw, ok := data["bytes_written"].(float64); ok {
		if p, ok := data["path"].(string); ok {
			return fmt.Sprintf("%s (%d bytes)", p, int(bw))
		}
	}
	// edit tool: show replacements
	if r, ok := data["replacements"].(float64); ok {
		return fmt.Sprintf("%d replacements", int(r))
	}
	// bash tool: show exit code + truncated stdout
	if code, ok := data["exit_code"].(float64); ok {
		stdout, _ := data["stdout"].(string)
		stdout = strings.ReplaceAll(stdout, "\n", " ")
		if len(stdout) > 80 {
			stdout = stdout[:77] + "..."
		}
		if int(code) != 0 {
			return fmt.Sprintf("exit %d: %s", int(code), stdout)
		}
		if stdout == "" {
			return "(No output)"
		}
		return stdout
	}
	// Fallback: compact JSON
	b, _ := json.Marshal(data)
	s := string(b)
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

// highlightReadOutput applies syntax highlighting to read tool output lines.
// Each line has format "     1\tcontent" — line numbers are styled separately.
func highlightReadOutput(lines []string, filename string) []string {
	numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Separate line numbers from code
	var codeLines []string
	var lineNums []string
	for _, line := range lines {
		if parts := strings.SplitN(line, "\t", 2); len(parts) == 2 {
			lineNums = append(lineNums, parts[0])
			codeLines = append(codeLines, parts[1])
		} else {
			lineNums = append(lineNums, "")
			codeLines = append(codeLines, line)
		}
	}

	// Highlight all code at once for proper multi-line token handling
	code := strings.Join(codeLines, "\n")
	highlighted := highlightCode(code, filename)
	highlightedLines := strings.Split(highlighted, "\n")

	// Recombine with styled line numbers
	result := make([]string, 0, len(lines))
	for i := range lines {
		if i < len(highlightedLines) {
			if i < len(lineNums) && lineNums[i] != "" {
				result = append(result, numStyle.Render(lineNums[i])+" "+highlightedLines[i])
			} else {
				result = append(result, highlightedLines[i])
			}
		}
	}
	return result
}

// highlightKey is the cache key for highlightCode output.
type highlightKey struct {
	filename string
	code     string
}

// highlightCache memoizes chroma output. Bubble Tea drives View from a single
// goroutine so this package-level cache needs no mutex. Bounded by
// highlightCacheCap to prevent unbounded growth.
var highlightCache = map[highlightKey]string{}

const highlightCacheCap = 256

// highlightCode applies chroma syntax highlighting based on filename extension.
func highlightCode(code, filename string) string {
	key := highlightKey{filename: filename, code: code}
	if cached, ok := highlightCache[key]; ok {
		return cached
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(code) //nolint:misspell // chroma API uses British spelling
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return code
	}
	result := strings.TrimRight(buf.String(), "\n")
	if len(highlightCache) >= highlightCacheCap {
		highlightCache = make(map[highlightKey]string, highlightCacheCap)
	}
	highlightCache[key] = result
	return result
}

// highlightGrepOutput styles grep result lines of the form "file:line: content".
func highlightGrepOutput(lines []string) []string {
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))     // blue
	lineNumStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // gray
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	result := make([]string, 0, len(lines))
	for _, line := range lines {
		// Try to parse "file:line: content"
		first := strings.IndexByte(line, ':')
		if first < 0 {
			// Not a match line (e.g. truncation note) — dim it.
			result = append(result, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(line))
			continue
		}
		second := strings.IndexByte(line[first+1:], ':')
		if second < 0 {
			result = append(result, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(line))
			continue
		}
		second += first + 1 // absolute index of second colon

		filePart := line[:first]
		linePart := line[first+1 : second]
		contentPart := ""
		if second+1 < len(line) {
			contentPart = strings.TrimPrefix(line[second+1:], " ")
		}

		// Highlight the content portion using the file extension.
		highlighted := highlightCode(contentPart, filePart)

		var sb strings.Builder
		sb.WriteString(fileStyle.Render(filePart))
		sb.WriteString(sepStyle.Render(":"))
		sb.WriteString(lineNumStyle.Render(linePart))
		sb.WriteString(sepStyle.Render(": "))
		sb.WriteString(highlighted)
		result = append(result, sb.String())
	}
	return result
}

// highlightFindOutput styles find/glob result lines as file paths.
func highlightFindOutput(lines []string) []string {
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // blue
	dirStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "...") {
			// Truncation note.
			result = append(result, dimStyle.Render(line))
		} else if strings.HasSuffix(line, "/") {
			result = append(result, dirStyle.Render(line))
		} else {
			result = append(result, fileStyle.Render(line))
		}
	}
	return result
}
