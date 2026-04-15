package loader

import "strings"

// SlashCommand is the minimal TUI seam a prompt template or extension
// exposes. Deferred behavior lives in spec #2.
type SlashCommand struct {
	Name        string
	Description string
	Prompt      string
}

// Skill is a discovered skill resource. Kept minimal for spec #1.
type Skill struct {
	Name        string
	Description string
	Path        string
}

// dedupeStrings returns xs with duplicate entries removed, preserving order.
func dedupeStrings(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// parseFrontmatterLine parses "key: value" from a YAML frontmatter line.
// Returns ok=false for comments, blank lines, or malformed entries.
func parseFrontmatterLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	value = strings.Trim(value, `"'`)
	return key, value, key != ""
}
