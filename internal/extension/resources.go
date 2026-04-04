package extension

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResourceDirs captures the discoverable resource directories used by startup.
type ResourceDirs struct {
	ExtensionDirs []string
	SkillDirs     []string
	PromptDirs    []string
	ThemeDirs     []string
}

// PromptTemplate is a first-class prompt resource that can be exposed in the TUI
// through the narrow SlashCommand seam without requiring custom widgets.
type PromptTemplate struct {
	Name        string
	Description string
	Prompt      string
	Source      string
}

// Render expands the minimal {{args}} placeholder used by slash commands.
func (t PromptTemplate) Render(args []string) string {
	prompt := t.Prompt
	joined := strings.TrimSpace(strings.Join(args, " "))
	prompt = strings.ReplaceAll(prompt, "{{args}}", joined)
	return strings.TrimSpace(prompt)
}

// SlashCommand converts a prompt template into the narrow TUI command seam.
func (t PromptTemplate) SlashCommand() SlashCommand {
	return SlashCommand{
		Name:        t.Name,
		Description: t.Description,
		Prompt:      t.Prompt,
	}
}

// DiscoverResourceDirs returns the ordered resource directories used by runtime
// discovery. Later directories override earlier ones by resource name.
func DiscoverResourceDirs(workDir string) ResourceDirs {
	var out ResourceDirs

	if home, err := os.UserHomeDir(); err == nil {
		globalRoot := filepath.Join(home, ".pi-go")
		out.ExtensionDirs = append(out.ExtensionDirs,
			packageResourceDirs(globalRoot, "extensions")...,
		)
		out.ExtensionDirs = append(out.ExtensionDirs, filepath.Join(globalRoot, "extensions"))

		out.SkillDirs = append(out.SkillDirs,
			packageResourceDirs(globalRoot, "skills")...,
		)
		out.SkillDirs = append(out.SkillDirs, filepath.Join(globalRoot, "skills"))

		out.PromptDirs = append(out.PromptDirs,
			packageResourceDirs(globalRoot, "prompts")...,
		)
		out.PromptDirs = append(out.PromptDirs, filepath.Join(globalRoot, "prompts"))

		out.ThemeDirs = append(out.ThemeDirs,
			packageResourceDirs(globalRoot, "themes")...,
		)
		out.ThemeDirs = append(out.ThemeDirs, filepath.Join(globalRoot, "themes"))
	}

	if workDir != "" {
		projectRoot := filepath.Join(workDir, ".pi-go")
		out.ExtensionDirs = append(out.ExtensionDirs, packageResourceDirs(projectRoot, "extensions")...)
		out.ExtensionDirs = append(out.ExtensionDirs, filepath.Join(projectRoot, "extensions"))

		out.SkillDirs = append(out.SkillDirs, packageResourceDirs(projectRoot, "skills")...)
		out.SkillDirs = append(out.SkillDirs,
			filepath.Join(projectRoot, "skills"),
			filepath.Join(workDir, ".claude", "skills"),
			filepath.Join(workDir, ".cursor", "skills"),
		)

		out.PromptDirs = append(out.PromptDirs, packageResourceDirs(projectRoot, "prompts")...)
		out.PromptDirs = append(out.PromptDirs, filepath.Join(projectRoot, "prompts"))

		out.ThemeDirs = append(out.ThemeDirs, packageResourceDirs(projectRoot, "themes")...)
		out.ThemeDirs = append(out.ThemeDirs, filepath.Join(projectRoot, "themes"))
	}

	out.ExtensionDirs = dedupeStrings(out.ExtensionDirs)
	out.SkillDirs = dedupeStrings(out.SkillDirs)
	out.PromptDirs = dedupeStrings(out.PromptDirs)
	out.ThemeDirs = dedupeStrings(out.ThemeDirs)
	return out
}

func packageResourceDirs(root, resource string) []string {
	packagesRoot := filepath.Join(root, "packages")
	entries, err := os.ReadDir(packagesRoot)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs = append(dirs, filepath.Join(packagesRoot, entry.Name(), resource))
	}
	sort.Strings(dirs)
	return dirs
}

// LoadPromptTemplates discovers markdown prompt templates from the provided
// directories. Later directories override earlier ones by prompt name.
func LoadPromptTemplates(dirs ...string) ([]PromptTemplate, error) {
	seen := map[string]int{}
	var templates []PromptTemplate

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading prompts dir %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			template, err := parsePromptTemplateFile(path)
			if err != nil {
				return nil, fmt.Errorf("parsing prompt template %s: %w", path, err)
			}
			if template.Name == "" {
				template.Name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			}
			template.Source = path
			if idx, ok := seen[template.Name]; ok {
				templates[idx] = template
			} else {
				seen[template.Name] = len(templates)
				templates = append(templates, template)
			}
		}
	}

	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Name < templates[j].Name
	})
	return templates, nil
}

func parsePromptTemplateFile(path string) (PromptTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PromptTemplate{}, err
	}

	tpl := PromptTemplate{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inFrontmatter := false
	frontmatterDone := false
	var body strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" && !frontmatterDone {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			inFrontmatter = false
			frontmatterDone = true
			continue
		}

		if inFrontmatter {
			key, value, ok := parseFrontmatterLine(line)
			if !ok {
				continue
			}
			switch key {
			case "name":
				tpl.Name = value
			case "description":
				tpl.Description = value
			}
			continue
		}

		body.WriteString(line)
		body.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return PromptTemplate{}, err
	}

	tpl.Prompt = strings.TrimSpace(body.String())
	return tpl, nil
}
