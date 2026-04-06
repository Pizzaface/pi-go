package tui

import (
	"strings"
	"unicode"

	"github.com/dimetron/pi-go/internal/extension"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// InputSubmitMsg is emitted when the user presses Enter with non-empty input.
type InputSubmitMsg struct {
	Text     string
	Mentions []string // file paths referenced via @path
}

// InputModel manages the text input area: cursor, history, and completion.
type InputModel struct {
	Text       string
	CursorPos  int
	History    []HistoryEntry
	HistoryIdx int

	// Ghost autocomplete suggestion.
	Completion string

	// Enhanced completion state.
	CompletionResult *CompleteResult
	CompletionMode   bool
	SelectedIndex    int

	// Command cycling state.
	CyclingIdx int

	// File @mention completion state.
	MentionMode          bool
	MentionStart         int // cursor position of the '@' character
	MentionResult        *CompleteResult
	MentionSelectedIndex int

	// Dependencies (set by root model).
	Skills            []extension.Skill
	SkillDirs         []string
	ExtensionCommands []extension.SlashCommand
	ExtensionManager  *extension.Manager
	WorkDir           string
}

// NewInputModel creates an InputModel with initial state.
func NewInputModel(history []HistoryEntry, skills []extension.Skill, skillDirs []string, workDir string) InputModel {
	return InputModel{
		History:    history,
		HistoryIdx: -1,
		CyclingIdx: -1,
		Skills:     skills,
		SkillDirs:  skillDirs,
		WorkDir:    workDir,
	}
}

// HandleKey processes a key press for the input area.
// Returns a tea.Cmd (InputSubmitMsg on submit, nil otherwise).
func (im *InputModel) HandleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.Key()

	switch {
	case key.Code == tea.KeyEnter:
		// Mention mode: apply file selection, don't submit.
		if im.MentionMode && im.MentionResult != nil && len(im.MentionResult.Candidates) > 0 {
			selected := im.MentionResult.Candidates[im.MentionSelectedIndex].Text
			// Replace @prefix with @selected-path
			before := im.Text[:im.MentionStart]
			after := im.Text[im.CursorPos:]
			im.Text = before + "@" + selected + after
			im.CursorPos = im.MentionStart + 1 + len(selected)
			im.dismissMention()
			return nil
		}
		// Cycling: apply selected command, dismiss menu.
		if im.CyclingIdx >= 0 {
			allCmds := im.AllCommandNames()
			if im.CyclingIdx < len(allCmds) {
				im.Text = allCmds[im.CyclingIdx]
				im.CursorPos = len(im.Text)
			}
			im.CyclingIdx = -1
			return nil
		}
		// Completion: apply selection.
		if im.CompletionMode && im.CompletionResult != nil && len(im.CompletionResult.Candidates) > 0 {
			im.Text = im.CompletionResult.ApplySelection(im.SelectedIndex)
			im.CursorPos = len(im.Text)
			im.CompletionMode = false
			im.CompletionResult = nil
			im.SelectedIndex = 0
			return nil
		}
		// Submit.
		text := strings.TrimSpace(im.Text)
		if text == "" {
			return nil
		}
		mentions := extractMentions(text)
		entry := HistoryEntry{Text: text, Mentions: mentions}
		if len(im.History) == 0 || im.History[len(im.History)-1].Text != text {
			im.History = append(im.History, entry)
			appendHistory(entry)
		}
		im.HistoryIdx = -1
		im.Text = ""
		im.CursorPos = 0
		return func() tea.Msg { return InputSubmitMsg{Text: text, Mentions: mentions} }

	case key.Code == tea.KeyTab && key.Mod == tea.ModShift:
		if im.MentionMode && im.MentionResult != nil && len(im.MentionResult.Candidates) > 0 {
			im.MentionResult.CycleSelection(-1)
			im.MentionSelectedIndex = im.MentionResult.Selected
			return nil
		}
		if im.CompletionMode && im.CompletionResult != nil && len(im.CompletionResult.Candidates) > 0 {
			im.CompletionResult.CycleSelection(-1)
			im.SelectedIndex = im.CompletionResult.Selected
		} else if im.Text == "/" || im.CyclingIdx >= 0 {
			allCmds := im.AllCommandNames()
			if len(allCmds) > 0 {
				if im.CyclingIdx <= 0 {
					im.CyclingIdx = len(allCmds) - 1
				} else {
					im.CyclingIdx--
				}
			}
		}

	case key.Code == tea.KeyTab:
		if im.MentionMode && im.MentionResult != nil && len(im.MentionResult.Candidates) > 0 {
			im.MentionResult.CycleSelection(1)
			im.MentionSelectedIndex = im.MentionResult.Selected
			return nil
		}
		if im.CompletionMode && im.CompletionResult != nil && len(im.CompletionResult.Candidates) > 0 {
			im.CompletionResult.CycleSelection(1)
			im.SelectedIndex = im.CompletionResult.Selected
		} else if im.Text == "/" || im.CyclingIdx >= 0 {
			allCmds := im.AllCommandNames()
			if len(allCmds) > 0 {
				im.CyclingIdx = (im.CyclingIdx + 1) % len(allCmds)
			}
		} else {
			im.CompletionResult = Complete(im.Text, im.Skills, im.WorkDir, im.currentExtensionCommands()...)
			if len(im.CompletionResult.Candidates) == 1 {
				im.Text = im.CompletionResult.Candidates[0].Text
				im.CursorPos = len(im.Text)
				im.CompletionResult = nil
			} else if len(im.CompletionResult.Candidates) > 1 {
				im.CompletionMode = true
				im.SelectedIndex = 0
				im.CompletionResult.Selected = 0
			}
		}

	case key.Code == tea.KeyBackspace:
		if im.CursorPos > 0 {
			im.Text = im.Text[:im.CursorPos-1] + im.Text[im.CursorPos:]
			im.CursorPos--
			if im.Text == "" {
				im.CyclingIdx = -1
			}
			// Update mention mode after backspace.
			if im.MentionMode {
				start, prefix := findMentionAtCursor(im.Text, im.CursorPos)
				if start >= 0 {
					im.MentionStart = start
					im.MentionResult = CompleteMention(prefix, im.WorkDir)
					im.MentionSelectedIndex = 0
				} else {
					im.dismissMention()
				}
			}
		}

	case key.Code == tea.KeyDelete:
		if im.CursorPos < len(im.Text) {
			im.Text = im.Text[:im.CursorPos] + im.Text[im.CursorPos+1:]
		}

	case key.Code == tea.KeyLeft:
		if im.CursorPos > 0 {
			im.CursorPos--
		}

	case key.Code == tea.KeyRight:
		if im.CursorPos < len(im.Text) {
			im.CursorPos++
		}

	case key.Code == tea.KeyHome || (key.Code == 'a' && key.Mod == tea.ModCtrl):
		im.CursorPos = 0

	case key.Code == tea.KeyEnd || (key.Code == 'e' && key.Mod == tea.ModCtrl):
		im.CursorPos = len(im.Text)

	case key.Code == tea.KeyUp:
		if im.CyclingIdx >= 0 {
			allCmds := im.AllCommandNames()
			if len(allCmds) > 0 {
				if im.CyclingIdx <= 0 {
					im.CyclingIdx = len(allCmds) - 1
				} else {
					im.CyclingIdx--
				}
			}
		} else if len(im.History) > 0 {
			if im.HistoryIdx < 0 {
				im.HistoryIdx = len(im.History) - 1
			} else if im.HistoryIdx > 0 {
				im.HistoryIdx--
			}
			im.restoreHistoryEntry(im.HistoryIdx)
		}

	case key.Code == tea.KeyDown:
		if im.CyclingIdx >= 0 {
			allCmds := im.AllCommandNames()
			if len(allCmds) > 0 {
				im.CyclingIdx = (im.CyclingIdx + 1) % len(allCmds)
			}
		} else if im.HistoryIdx >= 0 {
			im.HistoryIdx++
			if im.HistoryIdx >= len(im.History) {
				im.HistoryIdx = -1
				im.Text = ""
				im.CursorPos = 0
				im.dismissMention()
			} else {
				im.restoreHistoryEntry(im.HistoryIdx)
			}
		}

	case key.Code == tea.KeyEscape:
		if im.MentionMode {
			im.dismissMention()
			return nil
		}

	default:
		if key.Text != "" && isUserInput(key.Text) {
			if key.Text == "/" && im.Text == "" {
				im.ReloadSkills()
				im.Text = "/"
				im.CursorPos = 1
				im.CyclingIdx = -1
				return nil
			}
			im.Text = im.Text[:im.CursorPos] + key.Text + im.Text[im.CursorPos:]
			im.CursorPos += len(key.Text)
			im.CyclingIdx = -1

			// Enter mention mode when @ is typed.
			if key.Text == "@" {
				im.MentionMode = true
				im.MentionStart = im.CursorPos - 1
				im.MentionResult = CompleteMention("", im.WorkDir)
				im.MentionSelectedIndex = 0
				return nil
			}

			// Update mention completions while typing after @.
			if im.MentionMode {
				start, prefix := findMentionAtCursor(im.Text, im.CursorPos)
				if start >= 0 {
					im.MentionStart = start
					im.MentionResult = CompleteMention(prefix, im.WorkDir)
					im.MentionSelectedIndex = 0
				} else {
					im.dismissMention()
				}
			}
		}
	}

	// Update ghost autocomplete.
	if im.CursorPos == len(im.Text) {
		result := Complete(im.Text, im.Skills, im.WorkDir, im.currentExtensionCommands()...)
		if result != nil && len(result.Candidates) > 0 && len(result.Candidates) == 1 {
			im.Completion = result.Candidates[0].Text
		} else {
			im.Completion = ""
		}
	} else {
		im.Completion = ""
	}

	// Clear completion mode on non-Tab keys.
	if key.Code != tea.KeyTab {
		im.CompletionMode = false
		im.CompletionResult = nil
		im.SelectedIndex = 0
	}

	return nil
}

// View renders the input area.
func (im *InputModel) View(running bool) string {
	prefix := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true).
		Render("> ")

	if running {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		return prefix + dim.Render("(waiting for response...)")
	}

	before := im.Text[:im.CursorPos]
	after := im.Text[im.CursorPos:]
	cursor := lipgloss.NewStyle().
		Background(lipgloss.Color("252")).
		Foreground(lipgloss.Color("0")).
		Render(" ")
	if im.CursorPos < len(im.Text) {
		cursor = lipgloss.NewStyle().
			Background(lipgloss.Color("252")).
			Foreground(lipgloss.Color("0")).
			Render(string(im.Text[im.CursorPos]))
		after = im.Text[im.CursorPos+1:]
	}

	// Completion menu.
	if im.CompletionMode && im.CompletionResult != nil && len(im.CompletionResult.Candidates) > 0 {
		inputLine := prefix + before + cursor + after
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		sel := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

		var menu strings.Builder
		for i, c := range im.CompletionResult.Candidates {
			if i == im.SelectedIndex {
				menu.WriteString(sel.Render("  > " + c.Text))
			} else {
				menu.WriteString(dim.Render("    " + c.Text))
			}
			if c.Description != "" {
				menu.WriteString(dim.Render(" — " + c.Description))
			}
			menu.WriteString("\n")
		}
		return inputLine + "\n" + menu.String()
	}

	// Command cycling menu.
	if im.CyclingIdx >= 0 {
		// Ghost the selected command suffix on the input line.
		allCmds := im.AllCommandNames()
		ghostSuffix := ""
		if im.CyclingIdx < len(allCmds) {
			selected := allCmds[im.CyclingIdx]
			if strings.HasPrefix(selected, im.Text) {
				ghostSuffix = selected[len(im.Text):]
			} else {
				ghostSuffix = selected
			}
		}
		ghostStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		inputLine := prefix + before + cursor + after + ghostStyle.Render(ghostSuffix)
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		sel := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		extCommands := im.currentExtensionCommands()
		var menu strings.Builder
		for i, cmd := range allCmds {
			desc := slashCommandDesc(cmd)
			if desc == "" {
				for _, skill := range im.Skills {
					if "/"+skill.Name == cmd {
						desc = skill.Description
						break
					}
				}
			}
			if desc == "" {
				for _, extCmd := range extCommands {
					name := "/" + strings.TrimPrefix(extCmd.Name, "/")
					if name == cmd {
						desc = extCmd.Description
						break
					}
				}
			}
			if i == im.CyclingIdx {
				menu.WriteString(sel.Render("  > " + cmd))
			} else {
				menu.WriteString(dim.Render("    " + cmd))
			}
			if desc != "" {
				menu.WriteString(descStyle.Render(" — " + desc))
			}
			menu.WriteString("\n")
		}
		return inputLine + "\n" + menu.String()
	}

	// File @mention completion menu.
	if im.MentionMode && im.MentionResult != nil && len(im.MentionResult.Candidates) > 0 {
		inputLine := prefix + before + cursor + after
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		sel := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
		fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

		var menu strings.Builder
		for i, c := range im.MentionResult.Candidates {
			if i == im.MentionSelectedIndex {
				menu.WriteString(sel.Render("  > @" + c.Text))
			} else {
				menu.WriteString(dim.Render("    @" + c.Text))
			}
			if c.Description != "" {
				menu.WriteString(fileStyle.Render(" — " + c.Description))
			}
			menu.WriteString("\n")
		}
		return inputLine + "\n" + menu.String()
	}

	// Ghost autocomplete.
	ghost := ""
	if im.Completion != "" && im.CursorPos == len(im.Text) {
		suffix := im.Completion[len(im.Text):]
		ghost = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(suffix + " [tab]")
	}

	return prefix + before + cursor + after + ghost
}

// InsertText inserts pasted or programmatic text at cursor position.
func (im *InputModel) InsertText(text string) {
	im.Text = im.Text[:im.CursorPos] + text + im.Text[im.CursorPos:]
	im.CursorPos += len(text)
	im.Completion = ""
	im.CompletionMode = false
	im.CompletionResult = nil
	im.SelectedIndex = 0
	im.CyclingIdx = -1
}

// Clear resets the input text and cursor.
func (im *InputModel) Clear() {
	im.Text = ""
	im.CursorPos = 0
}

// InCompletionMode returns true if the input is showing a completion, cycling, or mention menu.
func (im *InputModel) InCompletionMode() bool {
	return im.CompletionMode || im.CyclingIdx >= 0 || im.MentionMode
}

// DismissCompletion clears completion/cycling/mention state and input.
func (im *InputModel) DismissCompletion() {
	im.CompletionMode = false
	im.CompletionResult = nil
	im.SelectedIndex = 0
	im.CyclingIdx = -1
	im.dismissMention()
	im.Text = ""
	im.CursorPos = 0
}

// restoreHistoryEntry restores full input state from a history entry.
func (im *InputModel) restoreHistoryEntry(idx int) {
	entry := im.History[idx]
	im.Text = entry.Text
	im.CursorPos = len(im.Text)
}

// dismissMention exits mention completion mode.
func (im *InputModel) dismissMention() {
	im.MentionMode = false
	im.MentionResult = nil
	im.MentionSelectedIndex = 0
	im.MentionStart = 0
}

// ReloadSkills re-scans skill directories from disk and updates the cached list.
func (im *InputModel) ReloadSkills() {
	if len(im.SkillDirs) > 0 {
		if fresh, err := extension.LoadSkills(im.SkillDirs...); err == nil {
			im.Skills = fresh
		}
	}
}

func (im *InputModel) currentExtensionCommands() []extension.SlashCommand {
	if im.ExtensionManager != nil {
		return im.ExtensionManager.SlashCommands()
	}
	return im.ExtensionCommands
}

type slashCommandInventoryItem struct {
	Name        string
	Description string
}

type slashCommandInventory struct {
	BuiltIns   []slashCommandInventoryItem
	Extensions []slashCommandInventoryItem
	Skills     []slashCommandInventoryItem
}

func builtInSlashCommandInventory() []slashCommandInventoryItem {
	items := make([]slashCommandInventoryItem, 0, len(slashCommands))
	for _, cmd := range slashCommands {
		items = append(items, slashCommandInventoryItem{
			Name:        cmd,
			Description: slashCommandDesc(cmd),
		})
	}
	return items
}

func extensionSlashCommandInventory(commands []extension.SlashCommand, seen map[string]bool) []slashCommandInventoryItem {
	items := make([]slashCommandInventoryItem, 0, len(commands))
	for _, cmd := range commands {
		name := "/" + strings.TrimPrefix(strings.TrimSpace(cmd.Name), "/")
		if name == "/" || seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, slashCommandInventoryItem{
			Name:        name,
			Description: strings.TrimSpace(cmd.Description),
		})
	}
	return items
}

func skillSlashCommandInventory(skills []extension.Skill, seen map[string]bool) []slashCommandInventoryItem {
	items := make([]slashCommandInventoryItem, 0, len(skills))
	for _, skill := range skills {
		name := "/" + strings.TrimPrefix(strings.TrimSpace(skill.Name), "/")
		if name == "/" || seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, slashCommandInventoryItem{
			Name:        name,
			Description: strings.TrimSpace(skill.Description),
		})
	}
	return items
}

func (im *InputModel) slashCommandInventory() slashCommandInventory {
	inventory := slashCommandInventory{
		BuiltIns: builtInSlashCommandInventory(),
	}
	seen := make(map[string]bool, len(inventory.BuiltIns))
	for _, cmd := range inventory.BuiltIns {
		seen[cmd.Name] = true
	}
	inventory.Extensions = extensionSlashCommandInventory(im.currentExtensionCommands(), seen)
	inventory.Skills = skillSlashCommandInventory(im.Skills, seen)
	return inventory
}

// AllCommandNames returns all command names in overlay order: built-in, extension, then skill.
func (im *InputModel) AllCommandNames() []string {
	inventory := im.slashCommandInventory()
	cmds := make([]string, 0, len(inventory.BuiltIns)+len(inventory.Extensions)+len(inventory.Skills))
	for _, section := range [][]slashCommandInventoryItem{inventory.BuiltIns, inventory.Extensions, inventory.Skills} {
		for _, cmd := range section {
			cmds = append(cmds, cmd.Name)
		}
	}
	return cmds
}

// slashCommands is the list of built-in slash commands for autocomplete.
var slashCommands = extension.DefaultBuiltinSlashCommands()

// slashCommandDesc returns the description for a slash command.
func slashCommandDesc(cmd string) string {
	switch cmd {
	case "/help":
		return "Show help"
	case "/clear":
		return "Clear conversation"
	case "/model":
		return "Show current model"
	case "/session":
		return "Show active session details"
	case "/new":
		return "Start a fresh session"
	case "/resume":
		return "List or switch saved sessions"
	case "/fork":
		return "Fork the current session into a branch"
	case "/tree":
		return "Show the current session branch tree"
	case "/settings":
		return "Show config and customization paths"
	case "/context":
		return "Show context usage"
	case "/branch":
		return "Manage branches"
	case "/compact":
		return "Compact context"
	case "/history":
		return "Command history"
	case "/login":
		return "Configure API keys (codex, openai, anthropic, gemini)"
	case "/theme":
		return "Switch theme or list themes"
	case "/skills":
		return "List skills (create, load)"
	case "/skill-create":
		return "Create a new skill"
	case "/skill-load":
		return "Reload skills from disk"
	case "/skill-list":
		return "List available skills"
	case "/ping":
		return "Test LLM connectivity"
	case "/debug":
		return "Toggle debug trace panel"
	case "/restart":
		return "Restart pi process"
	case "/exit", "/quit":
		return "Exit"
	default:
		return ""
	}
}

// completeSlashCommand returns the best matching slash command for the current input.
func completeSlashCommand(input string, extensionCommands ...extension.SlashCommand) string {
	if !strings.HasPrefix(input, "/") || len(input) < 2 {
		return ""
	}
	prefix := strings.ToLower(input)
	for _, cmd := range matchingSlashCommands(input, extensionCommands...) {
		if strings.HasPrefix(cmd, prefix) && cmd != prefix {
			return cmd
		}
	}
	return ""
}

// matchingSlashCommands returns all slash commands matching the given prefix.
func matchingSlashCommands(input string, extensionCommands ...extension.SlashCommand) []string {
	prefix := strings.ToLower(input)
	var matches []string
	seen := map[string]bool{}
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd, prefix) {
			seen[cmd] = true
			matches = append(matches, cmd)
		}
	}
	for _, extCmd := range extensionCommands {
		name := "/" + strings.TrimPrefix(strings.ToLower(extCmd.Name), "/")
		if seen[name] {
			continue
		}
		if strings.HasPrefix(name, prefix) {
			seen[name] = true
			matches = append(matches, name)
		}
	}
	return matches
}

// isUserInput returns true if the string represents genuine user keyboard input.
func isUserInput(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	if len(s) > 2 && s[0] == ']' && s[1] >= '0' && s[1] <= '9' {
		return false
	}
	if len(s) > 2 && s[0] == '[' && (s[len(s)-1] >= 'A' && s[len(s)-1] <= 'Z') {
		return false
	}
	if strings.Contains(s, ";rgb:") || strings.Contains(s, "rgb:") {
		return false
	}
	return true
}
