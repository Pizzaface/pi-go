package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func wrapPlainText(text string, width int) string {
	if width <= 1 || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			wrapped = append(wrapped, "")
			continue
		}
		wrapped = append(wrapped, wrapPlainLine(line, width)...)
	}
	return strings.Join(wrapped, "\n")
}

func wrapPlainLine(line string, width int) []string {
	if width <= 1 || line == "" {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}

	var out []string
	current := ""
	flush := func() {
		if current != "" {
			out = append(out, current)
			current = ""
		}
	}

	for _, word := range words {
		for len([]rune(word)) > width {
			flush()
			runes := []rune(word)
			out = append(out, string(runes[:width]))
			word = string(runes[width:])
		}
		if current == "" {
			current = word
			continue
		}
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
			continue
		}
		flush()
		current = word
	}
	flush()
	return out
}

func prefixBlockLines(content, prefix string) string {
	if content == "" {
		return prefix
	}
	lines := strings.Split(content, "\n")
	continuation := strings.Repeat(" ", len([]rune(prefix)))
	for i, line := range lines {
		if i == 0 {
			lines[i] = prefix + line
			continue
		}
		lines[i] = continuation + line
	}
	return strings.Join(lines, "\n")
}

func renderWrappedPrefixBlock(content, prefix string, width int) string {
	innerWidth := width - len([]rune(prefix))
	if innerWidth < 1 {
		innerWidth = 1
	}
	return prefixBlockLines(wrapPlainText(content, innerWidth), prefix)
}

// indentBlock prefixes every line of content with the given indent string.
func indentBlock(content, indent string) string {
	if content == "" || indent == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func padBlockWidth(content string, width int) string {
	if width <= 0 || content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	style := lipgloss.NewStyle().Width(width)
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}
