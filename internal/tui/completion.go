package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pizzaface/go-pi/internal/extension"
)

// CompletionType identifies what kind of completion to perform.
type CompletionType int

const (
	CompletionTypeNone CompletionType = iota
	CompletionTypeCommand
	CompletionTypeSkill
	CompletionTypeFile
)

// CompletionCandidate represents a single completion option.
type CompletionCandidate struct {
	Text        string
	Description string
	Type        CompletionType
}

// CompleteResult holds all completion results.
type CompleteResult struct {
	Candidates []CompletionCandidate
	Selected   int
	Type       CompletionType
}

// Complete returns completion candidates for the given input.
// It analyzes the input and returns all matching options for commands, skills, and specs.
func Complete(input string, skills []extension.Skill, workDir string, extensionCommands ...extension.SlashCommand) *CompleteResult {
	if input == "" {
		return &CompleteResult{}
	}

	// "/" alone returns no completion candidates (handled by the exact-slash overlay path)
	if input == "/" {
		return &CompleteResult{}
	}

	// Determine completion type and get candidates
	var candidates []CompletionCandidate

	completionType := detectCompletionType(input)

	switch completionType {
	case CompletionTypeCommand:
		// For command completion, include both built-in commands and skills
		candidates = append(candidates, matchingCommands(input, extensionCommands)...)
		candidates = append(candidates, matchingSkills(input, skills)...)
	case CompletionTypeSkill:
		candidates = matchingSkills(input, skills)
	}

	// Filter out exact matches for single candidates (no ghost for exact match)
	// But keep them if there are multiple candidates
	if len(candidates) > 1 {
		filtered := make([]CompletionCandidate, 0)
		for _, c := range candidates {
			if c.Text != input {
				filtered = append(filtered, c)
			}
		}
		candidates = filtered
	}

	// Sort candidates alphabetically by text
	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].Text) < strings.ToLower(candidates[j].Text)
	})

	return &CompleteResult{
		Candidates: candidates,
		Selected:   0,
		Type:       completionType,
	}
}

// detectCompletionType determines what kind of completion to perform.
func detectCompletionType(input string) CompletionType {
	// Check for command completion (just /)
	if input == "/" {
		return CompletionTypeCommand
	}

	// Check for partial command or skill (starts with /, no space)
	if strings.HasPrefix(input, "/") && !strings.Contains(input, " ") {
		// Could be command or skill - we'll match both in Complete()
		return CompletionTypeCommand
	}

	return CompletionTypeNone
}

// matchingCommands returns all command candidates matching the prefix.
func matchingCommands(prefix string, extensionCommands []extension.SlashCommand) []CompletionCandidate {
	prefix = strings.ToLower(prefix)

	var candidates []CompletionCandidate
	seen := map[string]bool{}

	// Check against slash commands
	for _, cmd := range slashCommands {
		if strings.HasPrefix(strings.ToLower(cmd), prefix) {
			seen[cmd] = true
			desc := slashCommandDesc(cmd)
			candidates = append(candidates, CompletionCandidate{
				Text:        cmd,
				Description: desc,
				Type:        CompletionTypeCommand,
			})
		}
	}
	for _, cmd := range extensionCommands {
		name := "/" + strings.TrimPrefix(strings.TrimSpace(cmd.Name), "/")
		lowerName := strings.ToLower(name)
		if seen[name] || !strings.HasPrefix(lowerName, prefix) {
			continue
		}
		seen[name] = true
		candidates = append(candidates, CompletionCandidate{
			Text:        name,
			Description: strings.TrimSpace(cmd.Description),
			Type:        CompletionTypeCommand,
		})
	}

	return candidates
}

// matchingSkills returns all skill candidates matching the prefix.
func matchingSkills(prefix string, skills []extension.Skill) []CompletionCandidate {
	prefix = strings.ToLower(strings.TrimPrefix(prefix, "/"))

	var candidates []CompletionCandidate

	for _, skill := range skills {
		if strings.HasPrefix(strings.ToLower(skill.Name), prefix) {
			candidates = append(candidates, CompletionCandidate{
				Text:        "/" + skill.Name,
				Description: skill.Description,
				Type:        CompletionTypeSkill,
			})
		}
	}

	return candidates
}

// CompleteMention returns file completion candidates for the given prefix.
func CompleteMention(prefix string, workDir string) *CompleteResult {
	candidates := matchingFiles(prefix, workDir)
	return &CompleteResult{
		Candidates: candidates,
		Selected:   0,
		Type:       CompletionTypeFile,
	}
}

// matchingFiles returns files in workDir whose relative path starts with the prefix.
// Skips hidden directories, node_modules, vendor, and binary artifacts.
// Returns at most 20 candidates.
func matchingFiles(prefix string, workDir string) []CompletionCandidate {
	if workDir == "" {
		return nil
	}

	normalizedPrefix := strings.ToLower(filepath.ToSlash(prefix))
	var candidates []CompletionCandidate

	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		if rel == "." {
			return nil
		}

		base := d.Name()
		if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		normalizedRel := filepath.ToSlash(rel)
		lowerRel := strings.ToLower(normalizedRel)
		if strings.HasPrefix(lowerRel, normalizedPrefix) || (normalizedPrefix != "" && fuzzyMatchPath(lowerRel, normalizedPrefix)) {
			candidates = append(candidates, CompletionCandidate{
				Text:        normalizedRel,
				Description: "file",
				Type:        CompletionTypeFile,
			})
		}

		if len(candidates) >= 20 {
			return filepath.SkipAll
		}
		return nil
	})

	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].Text) < strings.ToLower(candidates[j].Text)
	})

	return candidates
}

// fuzzyMatchPath checks if all parts of the query appear in order in the path.
func fuzzyMatchPath(path, query string) bool {
	pi := 0
	for qi := 0; qi < len(query) && pi < len(path); qi++ {
		idx := strings.IndexByte(path[pi:], query[qi])
		if idx < 0 {
			return false
		}
		pi += idx + 1
	}
	return pi <= len(path)
}

// findMentionAtCursor finds the @mention prefix at the cursor position.
// Returns the start index of '@' and the text after it, or -1 if no mention found.
func findMentionAtCursor(text string, cursorPos int) (start int, prefix string) {
	for i := cursorPos - 1; i >= 0; i-- {
		if text[i] == '@' {
			return i, text[i+1 : cursorPos]
		}
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\n' {
			break
		}
	}
	return -1, ""
}

// extractMentions finds all @path mentions in text and returns their paths.
func extractMentions(text string) []string {
	var mentions []string
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		// Extract the path after @
		j := i + 1
		for j < len(text) && text[j] != ' ' && text[j] != '\t' && text[j] != '\n' && text[j] != '@' {
			j++
		}
		if j > i+1 {
			mentions = append(mentions, text[i+1:j])
		}
		i = j - 1
	}
	return mentions
}

// CycleSelection moves the selection index in the given direction.
// dir should be 1 for next, -1 for previous.
func (r *CompleteResult) CycleSelection(dir int) {
	if len(r.Candidates) == 0 {
		return
	}
	r.Selected = (r.Selected + dir + len(r.Candidates)) % len(r.Candidates)
}

// ApplySelection returns the text of the candidate at the given index.
func (r *CompleteResult) ApplySelection(index int) string {
	if index < 0 || index >= len(r.Candidates) {
		return ""
	}
	return r.Candidates[index].Text
}

// SelectedCandidate returns the currently selected candidate.
func (r *CompleteResult) SelectedCandidate() *CompletionCandidate {
	if r.Selected < 0 || r.Selected >= len(r.Candidates) {
		return nil
	}
	return &r.Candidates[r.Selected]
}
