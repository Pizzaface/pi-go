package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/provider"
	pisession "github.com/dimetron/pi-go/internal/session"
)

func (m *model) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])

	// Log all slash commands.
	if m.cfg.Logger != nil {
		m.cfg.Logger.UserMessage(input)
	}

	switch cmd {
	case "/help":
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: m.formatHelp(),
		})
	case "/clear":
		m.chatModel.Messages = m.chatModel.Messages[:0]
		m.chatModel.Scroll = 0
	case "/model":
		return m.handleModelCommand()
	case "/session":
		m.handleSessionCommand()
	case "/new":
		m.handleNewCommand(parts[1:])
	case "/resume":
		m.handleResumeCommand(parts[1:])
	case "/fork":
		m.handleForkCommand(parts[1:])
	case "/tree":
		m.handleTreeCommand()
	case "/effort":
		return m.handleEffortCommand(parts[1:])
	case "/settings":
		m.handleSettingsCommand()
	case "/context":
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: m.formatContextUsage(),
		})
	case "/branch":
		m.handleBranchCommand(parts[1:])
	case "/compact":
		m.handleCompactCommand()
	case "/history":
		m.handleHistoryCommand(parts[1:])
	case "/login":
		return m.handleLoginCommand(parts[1:])
	case "/skills":
		return m.handleSkillsCommand(parts[1:])
	case "/skill-create":
		return m.handleSkillCreateCommand(parts[1:])
	case "/skill-load":
		return m.handleSkillLoadCommand()
	case "/skill-list":
		return m.handleSkillListCommand()
	case "/theme":
		return m.handleThemeCommand(parts[1:])
	case "/ping":
		return m.handlePingCommand(parts[1:])
	case "/debug":
		m.toggleDebugPanel()
		state := "off"
		if m.debugPanel {
			state = "on"
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: fmt.Sprintf("Debug panel toggled **%s**.", state),
		})
		return m, nil
	case "/restart":
		return m.handleRestartCommand()
	case "/extensions":
		return m.handleExtensionsCommand(parts[1:])
	case "/exit", "/quit":
		m.quitting = true
		return m, tea.Quit
	default:
		name := strings.TrimPrefix(cmd, "/")
		if m.cfg.ExtensionManager != nil {
			if extCmd, ok := m.cfg.ExtensionManager.FindCommand(name); ok {
				return m.submitPrompt(extCmd.Render(parts[1:]), nil)
			}
		} else {
			for _, extCmd := range m.cfg.ExtensionCommands {
				if strings.EqualFold(extCmd.Name, name) {
					return m.submitPrompt(extCmd.Render(parts[1:]), nil)
				}
			}
		}
		// Check if it's a dynamic skill command.
		if skill, ok := extension.FindSkill(m.cfg.Skills, name); ok {
			return m.handleSkillCommand(skill, parts[1:])
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: fmt.Sprintf("Unknown command: `%s`. Type `/help` for available commands.", cmd),
		})
	}

	return m, nil
}

func (m *model) extensionCommands() []extension.SlashCommand {
	if m.cfg.ExtensionManager != nil {
		return m.cfg.ExtensionManager.SlashCommands()
	}
	return m.cfg.ExtensionCommands
}

func (m *model) handleExtensionsCommand(args []string) (tea.Model, tea.Cmd) {
	if m.cfg.ExtensionManager == nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Extensions manager not available.",
		})
		return m, nil
	}

	if len(args) == 0 {
		rows := buildExtensionPanelRows(m.cfg.ExtensionManager.Extensions())
		cursor := 0
		for cursor < len(rows) && rows[cursor].isGroup {
			cursor++
		}
		m.extensionsPanel = &extensionsPanelState{rows: rows, cursor: cursor}
		return m, nil
	}

	sub := strings.ToLower(args[0])
	rest := args[1:]

	switch sub {
	case "reload":
		return m, extensionReloadCmd(m.cfg.ExtensionManager, m.cfg.WorkDir)
	case "approve":
		if len(rest) == 0 {
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role: "assistant", content: "Usage: `/extensions approve <id>`",
			})
			return m, nil
		}
		return m, extensionGrantAndStartCmd(m.cfg.ExtensionManager, m.cfg.WorkDir, rest[0], "", nil)
	case "deny":
		if len(rest) == 0 {
			return m, nil
		}
		return m, extensionDenyCmd(m.cfg.ExtensionManager, rest[0])
	case "stop":
		if len(rest) == 0 {
			return m, nil
		}
		return m, extensionStopCmd(m.cfg.ExtensionManager, rest[0])
	case "restart":
		if len(rest) == 0 {
			return m, nil
		}
		return m, extensionRestartCmd(m.cfg.ExtensionManager, rest[0])
	case "revoke":
		if len(rest) == 0 {
			return m, nil
		}
		return m, extensionRevokeCmd(m.cfg.ExtensionManager, rest[0])
	default:
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: fmt.Sprintf("Unknown extensions subcommand: `%s`. Valid: reload, approve, deny, stop, restart, revoke.", sub),
		})
		return m, nil
	}
}

// handleBranchCommand handles /branch subcommands: create, switch, list.
func (m *model) handleBranchCommand(args []string) {
	if m.cfg.SessionService == nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Branching not available (no session service configured).",
		})
		return
	}

	if len(args) == 0 {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Usage: `/branch <name>` to create, `/branch switch <name>` to switch, `/branch list` to list.",
		})
		return
	}

	subcmd := strings.ToLower(args[0])

	switch subcmd {
	case "list":
		branches, active, err := m.cfg.SessionService.ListBranches(
			m.cfg.SessionID, agent.AppName, agent.DefaultUserID,
		)
		if err != nil {
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role:    "assistant",
				content: fmt.Sprintf("Error listing branches: %v", err),
			})
			return
		}
		var b strings.Builder
		b.WriteString("**Branches:**\n")
		for _, br := range branches {
			marker := " "
			if br.Name == active {
				marker = "*"
			}
			fmt.Fprintf(&b, "- %s `%s` (head: %d)\n", marker, br.Name, br.Head)
		}
		m.chatModel.Messages = append(m.chatModel.Messages, message{role: "assistant", content: b.String()})

	case "switch":
		if len(args) < 2 {
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role:    "assistant",
				content: "Usage: `/branch switch <name>`",
			})
			return
		}
		branchName := args[1]
		err := m.cfg.SessionService.SwitchBranch(
			m.cfg.SessionID, agent.AppName, agent.DefaultUserID, branchName,
		)
		if err != nil {
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role:    "assistant",
				content: fmt.Sprintf("Error switching branch: %v", err),
			})
			return
		}
		if err := m.loadSessionMessages(m.cfg.SessionID); err != nil {
			m.appendAssistant(fmt.Sprintf("Switched to branch `%s`, but could not reload the transcript: %v", branchName, err))
			return
		}
		m.appendAssistant(fmt.Sprintf("Switched to branch `%s`.", branchName))

	default:
		// Treat as branch name to create.
		branchName := subcmd
		err := m.cfg.SessionService.CreateBranch(
			m.cfg.SessionID, agent.AppName, agent.DefaultUserID, branchName,
		)
		if err != nil {
			m.chatModel.Messages = append(m.chatModel.Messages, message{
				role:    "assistant",
				content: fmt.Sprintf("Error creating branch: %v", err),
			})
			return
		}
		// Auto-switch to the new branch.
		_ = m.cfg.SessionService.SwitchBranch(
			m.cfg.SessionID, agent.AppName, agent.DefaultUserID, branchName,
		)
		if err := m.loadSessionMessages(m.cfg.SessionID); err != nil {
			m.appendAssistant(fmt.Sprintf("Created branch `%s`, but could not reload the transcript: %v", branchName, err))
			return
		}
		m.appendAssistant(fmt.Sprintf("Created and switched to branch `%s`.", branchName))
	}
}

func (m *model) appendAssistant(content string) {
	m.chatModel.Messages = append(m.chatModel.Messages, message{role: "assistant", content: content})
}

func (m *model) formatSessionInfo() string {
	if m.cfg.SessionID == "" {
		return "No active session. Use `/new` to start one."
	}
	if m.cfg.SessionService == nil {
		return fmt.Sprintf("Session: `%s`", m.cfg.SessionID)
	}
	meta, err := m.cfg.SessionService.ListMeta(agent.AppName, agent.DefaultUserID, 0)
	if err != nil {
		return fmt.Sprintf("Session: `%s`", m.cfg.SessionID)
	}
	var current *pisession.Meta
	for i := range meta {
		if meta[i].ID == m.cfg.SessionID {
			current = &meta[i]
			break
		}
	}
	if current == nil {
		return fmt.Sprintf("Session: `%s`", m.cfg.SessionID)
	}
	branch := "main"
	if m.cfg.SessionService != nil {
		branch = m.cfg.SessionService.ActiveBranch(m.cfg.SessionID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Session:** %s\n\n", formatSessionLabel(*current))
	fmt.Fprintf(&b, "- **ID**: `%s`\n", current.ID)
	fmt.Fprintf(&b, "- **Branch**: `%s`\n", branch)
	if current.WorkDir != "" {
		fmt.Fprintf(&b, "- **Worktree**: `%s`\n", current.WorkDir)
	}
	fmt.Fprintf(&b, "- **Updated**: %s\n", current.UpdatedAt.Format(time.RFC3339))
	b.WriteString("\nUse `/resume` to switch sessions, `/fork <name>` to branch this one, `/tree` to inspect branches, or `/new` for a clean session.")
	return b.String()
}

func formatSessionLabel(meta pisession.Meta) string {
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = meta.ID
	}
	return fmt.Sprintf("**%s** (`%s`)", title, meta.ID)
}

func (m *model) handleSessionCommand() {
	m.appendAssistant(m.formatSessionInfo())
}

func (m *model) handleNewCommand(args []string) {
	if m.cfg.Agent == nil || m.cfg.SessionService == nil {
		m.appendAssistant("Session switching is not available yet.")
		return
	}
	sessionID, err := m.cfg.Agent.CreateSession(m.ctx)
	if err != nil {
		m.appendAssistant(fmt.Sprintf("Error creating session: %v", err))
		return
	}
	m.cfg.SessionID = sessionID
	m.chatModel.Messages = nil
	m.chatModel.Scroll = 0
	title := "New session"
	if metas, err := m.cfg.SessionService.ListMeta(agent.AppName, agent.DefaultUserID, 0); err == nil {
		for _, meta := range metas {
			if meta.ID == sessionID && strings.TrimSpace(meta.Title) != "" {
				title = meta.Title
				break
			}
		}
	}
	m.appendAssistant(fmt.Sprintf("Started %s. Ask a question or use `/resume` to jump back to another thread.", formatSessionLabel(pisession.Meta{ID: sessionID, Title: title})))
}

func (m *model) handleResumeCommand(args []string) {
	if m.cfg.SessionService == nil {
		m.appendAssistant("Resume is not available (no session service configured).")
		return
	}
	metas, err := m.cfg.SessionService.ListMeta(agent.AppName, agent.DefaultUserID, 12)
	if err != nil {
		m.appendAssistant(fmt.Sprintf("Error listing sessions: %v", err))
		return
	}
	if len(args) == 0 {
		if len(metas) == 0 {
			m.appendAssistant("No saved sessions found. Use `/new` to start one.")
			return
		}
		var b strings.Builder
		b.WriteString("**Recent sessions:**\n")
		for _, meta := range metas {
			marker := " "
			if meta.ID == m.cfg.SessionID {
				marker = "*"
			}
			fmt.Fprintf(&b, "- %s %s — updated %s\n", marker, formatSessionLabel(meta), meta.UpdatedAt.Format(time.RFC3339))
		}
		b.WriteString("\nUse `/resume <session-id>` to switch.")
		m.appendAssistant(b.String())
		return
	}
	query := strings.TrimSpace(args[0])
	if strings.EqualFold(query, "latest") && len(metas) > 0 {
		query = metas[0].ID
	}
	matches := make([]pisession.Meta, 0, 1)
	for _, meta := range metas {
		if meta.ID == query || strings.HasPrefix(meta.ID, query) {
			matches = append(matches, meta)
		}
	}
	if len(matches) == 0 {
		m.appendAssistant(fmt.Sprintf("No session matched `%s`. Use `/resume` to list recent sessions.", query))
		return
	}
	if len(matches) > 1 {
		m.appendAssistant(fmt.Sprintf("`%s` matched multiple sessions. Use a longer prefix.", query))
		return
	}
	if err := m.loadSessionMessages(matches[0].ID); err != nil {
		m.appendAssistant(fmt.Sprintf("Error loading session `%s`: %v", matches[0].ID, err))
		return
	}
	m.appendAssistant(fmt.Sprintf("Resumed %s.", formatSessionLabel(matches[0])))
}

func (m *model) handleForkCommand(args []string) {
	if len(args) == 0 {
		m.appendAssistant("Usage: `/fork <name>`")
		return
	}
	m.handleBranchCommand(args)
}

func (m *model) handleTreeCommand() {
	if m.cfg.SessionService == nil {
		m.appendAssistant("Branch tree is not available (no session service configured).")
		return
	}
	branches, active, err := m.cfg.SessionService.ListBranches(m.cfg.SessionID, agent.AppName, agent.DefaultUserID)
	if err != nil {
		m.appendAssistant(fmt.Sprintf("Error listing branches: %v", err))
		return
	}
	children := make(map[string][]pisession.BranchInfo)
	var roots []pisession.BranchInfo
	for _, br := range branches {
		if br.Parent == nil {
			roots = append(roots, br)
			continue
		}
		children[*br.Parent] = append(children[*br.Parent], br)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for name := range children {
		sort.Slice(children[name], func(i, j int) bool { return children[name][i].Name < children[name][j].Name })
	}
	var b strings.Builder
	b.WriteString("**Session tree:**\n")
	var walk func(pisession.BranchInfo, string)
	walk = func(br pisession.BranchInfo, indent string) {
		marker := " "
		if br.Name == active {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s%s `%s`\n", indent, marker, br.Name)
		for _, child := range children[br.Name] {
			walk(child, indent+"  ")
		}
	}
	for _, root := range roots {
		walk(root, "")
	}
	m.appendAssistant(b.String())
}

func (m *model) handleSettingsCommand() {
	m.appendAssistant(m.formatSettingsInfo())
}

func (m *model) formatSettingsInfo() string {
	home, _ := os.UserHomeDir()
	globalConfig := filepath.Join(home, ".pi-go", "config.json")
	projectConfig := filepath.Join(m.cfg.WorkDir, ".pi-go", "config.json")
	keys := config.APIKeys()
	var b strings.Builder
	b.WriteString("**Settings**\n\n")
	fmt.Fprintf(&b, "- **Global config**: `%s`\n", globalConfig)
	fmt.Fprintf(&b, "- **Project config**: `%s`\n", projectConfig)
	themeName := "default"
	if m.themeManager != nil {
		themeName = m.themeManager.CurrentName()
	}
	fmt.Fprintf(&b, "- **Theme**: `%s`\n", themeName)
	fmt.Fprintf(&b, "- **Role**: `%s`\n", defaultString(m.cfg.ActiveRole, "default"))
	fmt.Fprintf(&b, "- **Provider / model**: `%s / %s`\n", m.cfg.ProviderName, m.cfg.ModelName)
	if len(keys) == 0 {
		b.WriteString("- **API keys**: none detected via environment or `~/.pi-go/.env`\n")
	} else {
		providers := make([]string, 0, len(keys))
		for name := range keys {
			providers = append(providers, name)
		}
		sort.Strings(providers)
		fmt.Fprintf(&b, "- **API keys**: %s\n", strings.Join(providers, ", "))
	}
	if reg := m.cfg.ProviderRegistry; reg != nil {
		providers := reg.Providers()
		models := reg.Models()
		if len(providers) > 0 {
			b.WriteString("\n**Loaded provider families / aliases:**\n")
			for _, def := range providers {
				fmt.Fprintf(&b, "- `%s` → %s\n", def.Name, def.Family)
			}
		}
		if len(models) > 0 {
			b.WriteString("\n**Loaded model aliases:**\n")
			for _, mdl := range models {
				fmt.Fprintf(&b, "- `%s` → %s/%s\n", mdl.Name, mdl.Provider, mdl.Target)
			}
		}
		b.WriteString("\nCompatible provider resources extend existing families (`anthropic`, `openai`, `gemini`, `ollama`). If a backend needs a brand-new SDK or auth flow, wire it in intentionally instead of forcing it through `models/*.json`.\n")
	}
	b.WriteString("\nUse `/theme`, `/model`, `/login`, and `pi package list` for day-to-day customization. Docs: `docs/settings.md`, `docs/providers.md`, `docs/extensions.md`, `docs/packages.md`. ")
	return b.String()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (m *model) loadSessionMessages(sessionID string) error {
	resp, err := m.cfg.SessionService.Get(m.ctx, &session.GetRequest{
		AppName:   agent.AppName,
		UserID:    agent.DefaultUserID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("loading session: %w", err)
	}
	messages := make([]message, 0)
	for i := 0; i < resp.Session.Events().Len(); i++ {
		ev := resp.Session.Events().At(i)
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			switch {
			case part.Text != "" && ev.Content.Role == genai.RoleUser:
				messages = append(messages, message{role: "user", content: part.Text})
			case part.Text != "" && ev.Content.Role == "thinking":
				messages = append(messages, message{role: "thinking", content: part.Text})
			case part.Text != "":
				messages = append(messages, message{role: "assistant", content: part.Text})
			case part.FunctionCall != nil:
				messages = append(messages, message{role: "tool", tool: part.FunctionCall.Name, toolIn: toolCallSummary(part.FunctionCall.Name, part.FunctionCall.Args)})
			}
		}
	}
	m.cfg.SessionID = sessionID
	m.chatModel.Messages = messages
	m.chatModel.Streaming = ""
	m.chatModel.Thinking = ""
	m.chatModel.Scroll = 0
	if m.cfg.SessionService != nil {
		m.statusModel.GitBranch = detectBranch(m.cfg.WorkDir)
	}
	return nil
}

// handleCompactCommand triggers session compaction.
func (m *model) handleCompactCommand() {
	if m.cfg.SessionService == nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Compaction not available (no session service configured).",
		})
		return
	}

	err := m.cfg.SessionService.Compact(
		m.cfg.SessionID, agent.AppName, agent.DefaultUserID,
		pisession.SimpleSummarizer, pisession.CompactConfig{},
	)
	if err != nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: fmt.Sprintf("Compaction error: %v", err),
		})
		return
	}
	m.chatModel.Messages = append(m.chatModel.Messages, message{
		role:    "assistant",
		content: "Session context compacted.",
	})
}

// handleModelCommand opens the interactive model picker popup.
// It starts an async fetch of available models from all configured providers.
func (m *model) handleModelCommand() (tea.Model, tea.Cmd) {
	if m.cfg.ProviderRegistry == nil {
		m.appendAssistant("No provider registry available.")
		return m, nil
	}

	m.modelPicker = &modelPickerState{
		loading: true,
		current: m.cfg.ModelName,
		height:  12,
		hidden:  loadHiddenModels(),
	}

	return m, fetchModels(m.ctx, m.cfg.ProviderRegistry)
}

// handleModelSelect applies the user's model selection from the picker popup.
func (m *model) handleModelSelect() (tea.Model, tea.Cmd) {
	if m.modelPicker == nil {
		return m, nil
	}
	selected := m.modelPicker.selectedModel()
	if selected == nil {
		m.modelPicker = nil
		return m, nil
	}

	// Resolve provider info for the selected model.
	reg := m.cfg.ProviderRegistry
	info, err := reg.Resolve(selected.ID, selected.Provider)
	if err != nil {
		m.appendAssistant(fmt.Sprintf("Failed to resolve model `%s`: %v", selected.ID, err))
		m.modelPicker = nil
		return m, nil
	}

	apiKey := reg.APIKey(info.Provider)
	baseURL := reg.BaseURL(info.Provider)

	llmOpts := &provider.LLMOptions{
		ExtraHeaders: reg.DefaultHeaders(info.Provider),
	}

	newLLM, err := provider.NewLLM(m.ctx, info, apiKey, baseURL, m.effortLevel, llmOpts)
	if err != nil {
		m.appendAssistant(fmt.Sprintf("Failed to create LLM for `%s`: %v", selected.ID, err))
		m.modelPicker = nil
		return m, nil
	}

	// Wrap the new LLM with the token/usage tracker if available.
	if m.cfg.WrapLLM != nil {
		newLLM = m.cfg.WrapLLM(newLLM)
	}

	// Hot-swap the agent's model.
	if m.cfg.Agent != nil {
		if err := m.cfg.Agent.RebuildWithModel(newLLM); err != nil {
			m.appendAssistant(fmt.Sprintf("Failed to swap model: %v", err))
			m.modelPicker = nil
			return m, nil
		}
	}

	// Update TUI config.
	m.cfg.LLM = newLLM
	m.cfg.ModelName = selected.ID
	m.cfg.ProviderName = info.Provider

	// Update context window limit from the selected model's metadata.
	if selected.MaxInputTokens > 0 {
		if setter, ok := m.cfg.TokenTracker.(interface{ SetContextLimit(int64) }); ok {
			setter.SetContextLimit(selected.MaxInputTokens)
		}
	} else if ctxLimit := provider.KnownContextWindow(selected.ID); ctxLimit > 0 {
		if setter, ok := m.cfg.TokenTracker.(interface{ SetContextLimit(int64) }); ok {
			setter.SetContextLimit(ctxLimit)
		}
	}

	// Persist last-selected model for next TUI launch.
	saveLastSelectedModel(selected.ID, info.Provider)

	m.appendAssistant(fmt.Sprintf("Switched to model **%s** (%s)", selected.ID, info.Provider))
	m.modelPicker = nil
	return m, nil
}

// handleEffortCommand shows or changes the reasoning effort level.
// Usage: /effort — shows current level; /effort <level> — sets the level.
func (m *model) handleEffortCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		levels := provider.AllEffortLevels()
		var b strings.Builder
		b.WriteString("**Reasoning effort level:** `")
		b.WriteString(m.effortLevel.String())
		b.WriteString("`\n\nAvailable levels:\n")
		for _, lvl := range levels {
			marker := "  "
			if lvl == m.effortLevel {
				marker = "▸ "
			}
			desc := effortDescription(lvl)
			fmt.Fprintf(&b, "%s`%s` — %s\n", marker, lvl.String(), desc)
		}
		b.WriteString("\nUsage: `/effort <level>` to change")
		m.appendAssistant(b.String())
		return m, nil
	}

	newLevel := provider.ParseEffortLevel(args[0])
	if newLevel == m.effortLevel {
		m.appendAssistant(fmt.Sprintf("Effort level is already `%s`.", newLevel.String()))
		return m, nil
	}

	oldLevel := m.effortLevel
	m.effortLevel = newLevel

	// Rebuild LLM with new effort level.
	reg := m.cfg.ProviderRegistry
	if reg != nil {
		info, err := reg.Resolve(m.cfg.ModelName, m.cfg.ProviderName)
		if err == nil {
			apiKey := reg.APIKey(info.Provider)
			baseURL := reg.BaseURL(info.Provider)
			llmOpts := &provider.LLMOptions{
				ExtraHeaders: reg.DefaultHeaders(info.Provider),
			}
			newLLM, err := provider.NewLLM(m.ctx, info, apiKey, baseURL, newLevel, llmOpts)
			if err == nil {
				if m.cfg.WrapLLM != nil {
					newLLM = m.cfg.WrapLLM(newLLM)
				}
				if m.cfg.Agent != nil {
					if rebuildErr := m.cfg.Agent.RebuildWithModel(newLLM); rebuildErr != nil {
						m.effortLevel = oldLevel
						m.appendAssistant(fmt.Sprintf("Failed to rebuild model with new effort: %v", rebuildErr))
						return m, nil
					}
				}
				m.cfg.LLM = newLLM
			}
		}
	}

	// Persist effort level.
	saveEffortLevel(newLevel)

	m.appendAssistant(fmt.Sprintf("Effort level changed: `%s` → `%s`", oldLevel.String(), newLevel.String()))
	return m, nil
}

// effortDescription returns a human-readable description for an effort level.
func effortDescription(level provider.EffortLevel) string {
	switch level {
	case provider.EffortNone:
		return "No extended thinking/reasoning"
	case provider.EffortLow:
		return "Minimal thinking budget"
	case provider.EffortMedium:
		return "Balanced thinking (default)"
	case provider.EffortHigh:
		return "Maximum thinking budget"
	default:
		return ""
	}
}

// saveEffortLevel persists the effort level to ~/.pi-go/config.json.
func saveEffortLevel(level provider.EffortLevel) {
	dir := piGoDir()
	if dir == "" {
		return
	}
	configPath := filepath.Join(dir, "config.json")

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			raw = make(map[string]any)
		}
	}

	raw["thinkingLevel"] = level.String()

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(configPath, out, 0o644)
}

// formatModelInfo returns a formatted string showing the current model and all configured roles.
func (m *model) formatModelInfo() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Current model: **%s**", m.cfg.ModelName)
	if m.cfg.ActiveRole != "" && m.cfg.ActiveRole != "default" {
		fmt.Fprintf(&b, " (role: %s)", m.cfg.ActiveRole)
	}

	if len(m.cfg.Roles) > 0 {
		b.WriteString("\n\n**Configured roles:**\n")
		// Sort role names for stable output.
		names := make([]string, 0, len(m.cfg.Roles))
		for name := range m.cfg.Roles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			rc := m.cfg.Roles[name]
			marker := " "
			if name == m.cfg.ActiveRole || (m.cfg.ActiveRole == "" && name == "default") {
				marker = "*"
			}
			provInfo := ""
			if rc.Provider != "" {
				provInfo = fmt.Sprintf(" [%s]", rc.Provider)
			}
			fmt.Fprintf(&b, "- %s **%s**: `%s`%s\n", marker, name, rc.Model, provInfo)
		}
	}
	if reg := m.cfg.ProviderRegistry; reg != nil && reg.HasCustomizations() {
		providers := reg.Providers()
		models := reg.Models()
		if len(providers) > 0 {
			b.WriteString("\n**Loaded provider aliases:**\n")
			for _, def := range providers {
				fmt.Fprintf(&b, "- `%s` → %s\n", def.Name, def.Family)
			}
		}
		if len(models) > 0 {
			b.WriteString("\n**Loaded model aliases:**\n")
			for _, mdl := range models {
				fmt.Fprintf(&b, "- `%s` → %s/%s\n", mdl.Name, mdl.Provider, mdl.Target)
			}
		}
	}
	return b.String()
}

// formatContextUsage builds a context usage display similar to Claude Code's /context.
// When a TokenTracker is available and has received at least one provider
// response, it uses the actual provider-reported token counts. Otherwise it
// falls back to a rough character-based estimate (~4 chars per token).
func (m *model) formatContextUsage() string {
	var b strings.Builder

	// Count chars per role (rough token estimate: ~4 chars per token).
	// Used as fallback when no provider response has been received yet.
	userChars, assistantChars, toolChars := 0, 0, 0
	for _, msg := range m.chatModel.Messages {
		size := len(msg.content) + len(msg.tool) + len(msg.toolIn)
		switch msg.role {
		case "user":
			userChars += size
		case "assistant":
			assistantChars += size
		default: // tool
			toolChars += size
		}
	}
	totalChars := userChars + assistantChars + toolChars
	estTokens := int64(totalChars / 4)
	userTokens := int64(userChars / 4)
	assistantTokens := int64(assistantChars / 4)
	toolTokens := int64(toolChars / 4)

	// Determine whether we have actual provider-reported context usage.
	var contextUsed int64  // current context window size from provider
	var contextLimit int64 // max context window (0 = unknown)
	hasProviderContext := false
	if tt := m.cfg.TokenTracker; tt != nil {
		contextUsed = tt.ContextUsed()
		contextLimit = tt.ContextLimit()
		if contextUsed > 0 {
			hasProviderContext = true
		}
	}

	// Choose the token count to display: actual or estimated.
	displayTokens := estTokens
	if hasProviderContext {
		displayTokens = contextUsed
	}

	// Header.
	b.WriteString("**Context Usage**\n\n")

	// Progress bar (20 blocks).
	const barLen = 20
	var usedBlocks int
	if contextLimit > 0 && displayTokens > 0 {
		pct := float64(displayTokens) / float64(contextLimit)
		usedBlocks = int(pct * barLen)
		if usedBlocks > barLen {
			usedBlocks = barLen
		}
	} else {
		// No context limit known — show rough proportion.
		if displayTokens > 0 {
			usedBlocks = 1
			if displayTokens > 10000 {
				usedBlocks = int(float64(displayTokens) / 100000 * barLen)
				if usedBlocks < 1 {
					usedBlocks = 1
				}
				if usedBlocks > barLen {
					usedBlocks = barLen
				}
			}
		}
	}
	bar := strings.Repeat("█", usedBlocks) + strings.Repeat("░", barLen-usedBlocks)

	// Model line with bar.
	modelLabel := m.cfg.ModelName
	if m.cfg.ProviderName != "" {
		modelLabel = m.cfg.ProviderName + " | " + modelLabel
	}
	if contextLimit > 0 {
		pct := float64(displayTokens) / float64(contextLimit) * 100
		fmt.Fprintf(&b, "`%s`  %s · %s/%s tokens (%.0f%%)\n\n",
			bar, modelLabel,
			formatTokenCount(displayTokens), formatTokenCount(contextLimit), pct)
	} else if hasProviderContext {
		fmt.Fprintf(&b, "`%s`  %s · ctx %s tokens\n\n",
			bar, modelLabel, formatTokenCount(displayTokens))
	} else {
		fmt.Fprintf(&b, "`%s`  %s · ctx ~%s tokens\n\n",
			bar, modelLabel, formatTokenCount(estTokens))
	}

	// Category breakdown (always estimated from message content).
	label := "*Estimated usage by category*\n"
	if hasProviderContext {
		label = "*Usage by category (estimated breakdown)*\n"
	}
	b.WriteString(label)
	fmt.Fprintf(&b, "- **User messages**: ~%s tokens (%d msgs)\n",
		formatTokenCount(userTokens), countByRole(m.chatModel.Messages, "user"))
	fmt.Fprintf(&b, "- **Assistant messages**: ~%s tokens (%d msgs)\n",
		formatTokenCount(assistantTokens), countByRole(m.chatModel.Messages, "assistant"))
	fmt.Fprintf(&b, "- **Tool calls**: ~%s tokens (%d calls)\n",
		formatTokenCount(toolTokens), countByRole(m.chatModel.Messages, "tool"))
	if hasProviderContext {
		fmt.Fprintf(&b, "- **Total context**: %s tokens (%d messages)\n",
			formatTokenCount(contextUsed), len(m.chatModel.Messages))
	} else {
		fmt.Fprintf(&b, "- **Total context**: ~%s tokens (%d messages)\n",
			formatTokenCount(estTokens), len(m.chatModel.Messages))
	}

	// Daily token usage (actual, not estimated).
	if tt := m.cfg.TokenTracker; tt != nil {
		total := tt.TotalUsed()
		if total > 0 {
			b.WriteString("\n*Daily token usage*\n")
			fmt.Fprintf(&b, "- **Consumed today**: %s tokens\n", formatTokenCount(total))
			if tt.Limit() > 0 {
				fmt.Fprintf(&b, "- **Remaining**: %s tokens\n", formatTokenCount(tt.Remaining()))
			}
		}
	}

	// Compaction stats.
	if cm := m.cfg.CompactMetrics; cm != nil {
		stats := cm.FormatStats()
		if stats != "" {
			b.WriteString("\n*Output compaction*\n")
			b.WriteString(stats)
		}
	}

	return b.String()
}

// showCommandList displays available slash commands as an assistant message.
func (m *model) showCommandList() {
	var b strings.Builder
	b.WriteString("**Commands:**\n")
	for _, cmd := range slashCommands {
		desc := slashCommandDesc(cmd)
		b.WriteString("  `" + cmd + "`")
		if desc != "" {
			b.WriteString(" — " + desc)
		}
		b.WriteString("\n")
	}
	extCommands := m.extensionCommands()
	if len(extCommands) > 0 {
		b.WriteString("\n**Extension commands:**\n")
		for _, cmd := range extCommands {
			fmt.Fprintf(&b, "  `/%s`", strings.TrimPrefix(cmd.Name, "/"))
			if cmd.Description != "" {
				b.WriteString(" — " + cmd.Description)
			}
			b.WriteString("\n")
		}
	}
	if len(m.cfg.Skills) > 0 {
		b.WriteString("\n**Skills:**\n")
		for _, skill := range m.cfg.Skills {
			fmt.Fprintf(&b, "  `/%s`", skill.Name)
			if skill.Description != "" {
				b.WriteString(" — " + skill.Description)
			}
			b.WriteString("\n")
		}
	}
	m.chatModel.Messages = append(m.chatModel.Messages, message{
		role:    "assistant",
		content: b.String(),
	})
}

// formatHelp builds the grouped help text for /help.
func (m *model) formatHelp() string {
	var b strings.Builder

	b.WriteString("**Commands:**\n")
	b.WriteString("  `/help`                — Show this help\n")
	b.WriteString("  `/clear`               — Clear conversation\n")
	b.WriteString("  `/model`               — Open interactive model picker\n")
	b.WriteString("  `/effort [level]`      — Show or set reasoning effort (none/low/medium/high)\n")
	b.WriteString("  `/session`             — Show active session details\n")
	b.WriteString("  `/new`                 — Start a fresh session\n")
	b.WriteString("  `/resume [id]`         — List or switch saved sessions\n")
	b.WriteString("  `/fork <name>`         — Fork the current session\n")
	b.WriteString("  `/tree`                — Show the current session branch tree\n")
	b.WriteString("  `/settings`            — Show config and customization paths\n")
	b.WriteString("  `/context`             — Show context usage\n")
	b.WriteString("  `/compact`             — Compact session context\n")
	b.WriteString("  `/history [query]`     — Command history\n")
	b.WriteString("  `/exit`, `/quit`       — Exit\n")

	b.WriteString("\n**Branches:**\n")
	b.WriteString("  `/branch <name>`       — Create/switch/list branches\n")

	b.WriteString("\n**Display:**\n")
	b.WriteString("  `/theme [name]`        — List or switch themes\n")

	b.WriteString("\n**System:**\n")

	b.WriteString("  `/login [provider]`   — Open login picker or configure a provider\n")
	b.WriteString("  `/debug`               — Toggle debug trace panel\n")
	b.WriteString("  `/restart`             — Restart the go-pi process\n")

	b.WriteString("\n**Skills:**\n")
	b.WriteString("  `/skills`              — List available skills\n")
	b.WriteString("  `/skills create <n>`   — Create a new skill\n")
	b.WriteString("  `/skills load`         — Reload skills from disk\n")

	extCommands := m.extensionCommands()
	if len(extCommands) > 0 {
		b.WriteString("\n**Extension commands:**\n")
		for _, cmd := range extCommands {
			fmt.Fprintf(&b, "  `/%s`", strings.TrimPrefix(cmd.Name, "/"))
			if cmd.Description != "" {
				b.WriteString(" — " + cmd.Description)
			}
			b.WriteString("\n")
		}
	}

	if len(m.cfg.Skills) > 0 {
		b.WriteString("\n**Available skills:**\n")
		for _, s := range m.cfg.Skills {
			fmt.Fprintf(&b, "  `/%s`", s.Name)
			if s.Description != "" {
				b.WriteString(" — " + s.Description)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n**Keyboard shortcuts:**\n")
	b.WriteString("  `Enter` — Submit  `Ctrl+C`/`Esc` — Cancel  `Ctrl+O` — Hide/show tool results  `Up/Down` — History  `PgUp/PgDn` — Scroll\n")

	return b.String()
}

// handleSkillsCommand handles /skills and its subcommands: create, load, list (default).
func (m *model) handleSkillsCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		return m.handleSkillListCommand()
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "create":
		return m.handleSkillCreateCommand(args[1:])
	case "load", "reload":
		return m.handleSkillLoadCommand()
	case "list":
		return m.handleSkillListCommand()
	default:
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Usage: `/skills` — list  |  `/skills create <name>` — create  |  `/skills load` — reload",
		})
		return m, nil
	}
}

// formatThemeList builds a display string listing all themes with the current one marked.
func formatThemeList(themes []Theme, currentName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Current theme:** `%s`\n\n", currentName)
	b.WriteString("**Available themes:**\n")
	for _, t := range themes {
		marker := " "
		if t.Name == currentName {
			marker = "*"
		}
		icon := "🌙"
		if t.ThemeType == "light" {
			icon = "☀️"
		}
		fmt.Fprintf(&b, "%s %s `%s` — %s\n", marker, icon, t.Name, t.DisplayName)
	}
	return b.String()
}

// formatThemeError builds an error message with optional close-match suggestions.
func formatThemeError(name string, matches []string) string {
	msg := fmt.Sprintf("Unknown theme `%s`.", name)
	if len(matches) > 0 {
		msg += " Did you mean: " + strings.Join(matches, ", ") + "?"
	}
	return msg
}

// handleThemeCommand handles /theme: list themes, switch theme, or show current.
func (m *model) handleThemeCommand(args []string) (tea.Model, tea.Cmd) {
	if m.themeManager == nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: "Theme system not available.",
		})
		return m, nil
	}

	if len(args) == 0 {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: formatThemeList(m.themeManager.List(), m.themeManager.CurrentName()),
		})
		return m, nil
	}

	name := strings.ToLower(args[0])
	if err := m.themeManager.SetTheme(name); err != nil {
		m.chatModel.Messages = append(m.chatModel.Messages, message{
			role:    "assistant",
			content: formatThemeError(name, m.themeManager.ClosestMatches(name, 5)),
		})
		return m, nil
	}

	saveThemeToConfig(name)

	cur := m.themeManager.Current()
	m.chatModel.Messages = append(m.chatModel.Messages, message{
		role:    "assistant",
		content: fmt.Sprintf("Theme switched to `%s` (%s). Colors will apply to new output.", cur.Name, cur.DisplayName),
	})
	return m, nil
}
