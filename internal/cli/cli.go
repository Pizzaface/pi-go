package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/session"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/jsonrpc"
	"github.com/dimetron/pi-go/internal/logger"
	"github.com/dimetron/pi-go/internal/provider"
	pisession "github.com/dimetron/pi-go/internal/session"
	"github.com/dimetron/pi-go/internal/tools"

	"github.com/spf13/cobra"
)

var (
	flagModel   string
	flagMode    string
	flagSession string
	flagSocket  string
	flagURL     string
	flagHeaders []string

	flagContinue bool
	flagInsecure bool
	flagSmol     bool
	flagSlow     bool
	flagPlan     bool
	flagSystem   string
)

// Version is set at build time via -ldflags.
var Version = "dev"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pi [prompt]",
		Short:   "go-pi coding agent",
		Long:    "A minimal Go coding agent harness with multi-provider LLM support, tool calling, and an interactive TUI.",
		Version: Version,
		Args:    cobra.ArbitraryArgs,
		RunE:    runRoot,
	}

	cmd.Flags().StringVar(&flagModel, "model", "", "LLM model to use (e.g. claude-sonnet-4-6, gpt-4o, gemini-2.5-pro)")
	cmd.Flags().StringVar(&flagMode, "mode", "", "Output mode: interactive, print, json, rpc")
	cmd.Flags().StringVar(&flagSocket, "socket", "/tmp/go-pi.sock", "Unix socket path for RPC mode")
	cmd.Flags().StringVar(&flagSession, "session", "", "Session ID to resume")
	cmd.Flags().StringVar(&flagURL, "url", "", "Alternative base URL for the LLM API endpoint")
	cmd.Flags().BoolVar(&flagContinue, "continue", false, "Continue last session")
	cmd.Flags().BoolVar(&flagSmol, "smol", false, "Use the smol role (fast/cheap model)")
	cmd.Flags().BoolVar(&flagSlow, "slow", false, "Use the slow role (powerful model)")
	cmd.Flags().BoolVar(&flagPlan, "plan", false, "Use the plan role (planning model)")
	cmd.Flags().StringVar(&flagSystem, "system", "", "System instruction (overrides default)")
	cmd.Flags().StringArrayVar(&flagHeaders, "header", nil, "Extra HTTP header for LLM requests (key=value, repeatable)")
	cmd.Flags().BoolVar(&flagInsecure, "insecure", false, "Skip TLS certificate verification for LLM API calls")

	cmd.AddCommand(newPingCmd())
	cmd.AddCommand(newPackageCmd())

	return cmd
}

func runRoot(cmd *cobra.Command, args []string) error {
	// Load API keys from ~/.pi-go/.env (set by /login command).
	loadDotEnv()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	reg, err := extension.BuildProviderRegistry(cwd, cfg)
	if err != nil {
		return fmt.Errorf("building provider registry: %w", err)
	}

	// Determine active role from flags.
	activeRole := "default"
	switch {
	case flagSmol:
		activeRole = "smol"
	case flagSlow:
		activeRole = "slow"
	case flagPlan:
		activeRole = "plan"
	}

	// Resolve model: CLI flag overrides the selected active role.
	applyModelOverride(&cfg, activeRole, flagModel)

	info, err := cfg.ResolveRoleInfoWithRegistry(activeRole, reg)
	if err != nil {
		return fmt.Errorf("resolving model role: %w", err)
	}

	mode := flagMode
	if mode == "" {
		mode = detectMode()
	}

	var debugTracer *provider.DebugTracer
	if mode == "interactive" {
		debugTracer = provider.NewDebugTracer()
	}

	apiKey := reg.APIKey(info.Provider)
	if apiKey == "" && reg.RequiresAPIKey(info.Provider) {
		envVar := reg.ProviderEnvVar(info.Provider)
		return fmt.Errorf("no API key found for provider %q (set %s)", info.Provider, envVar)
	}

	// Resolve base URL: --url flag takes precedence over registry env/defaults.
	baseURL := flagURL
	if baseURL == "" {
		baseURL = reg.BaseURL(info.Provider)
	}

	// Check Ollama is online before proceeding.
	if info.Ollama {
		if err := provider.CheckOllama(baseURL); err != nil {
			return fmt.Errorf("ollama health check: %w", err)
		}
	}

	// Build LLM options: registry headers + config headers + CLI headers + insecure TLS.
	llmOpts := &provider.LLMOptions{
		ExtraHeaders:    mergeExtraHeaders(mergeExtraHeaders(reg.DefaultHeaders(info.Provider), nil), append(headerMapToPairs(cfg.ExtraHeaders), flagHeaders...)),
		InsecureSkipTLS: cfg.InsecureSkipTLS || flagInsecure,
		DebugTracer:     debugTracer,
	}

	// Create the LLM provider.
	llm, err := provider.NewLLM(cmd.Context(), info, apiKey, baseURL, cfg.ThinkingLevel, llmOpts)
	if err != nil {
		return fmt.Errorf("creating LLM provider: %w", err)
	}

	prompt := strings.Join(args, " ")

	sandboxRoot := os.Getenv("PI_SANDBOX_ROOT")
	if sandboxRoot == "" {
		sandboxRoot = cwd
	}

	// Resolve --continue early (before TUI) so errors surface immediately.
	if flagContinue {
		homeDir, hErr := os.UserHomeDir()
		if hErr != nil {
			return fmt.Errorf("getting home dir: %w", hErr)
		}
		sessionsDir := filepath.Join(homeDir, ".pi-go", "sessions")
		sessionSvc, sErr := pisession.NewFileService(sessionsDir)
		if sErr != nil {
			return fmt.Errorf("creating session service: %w", sErr)
		}
		lastID := sessionSvc.LastSessionID(agent.AppName, agent.DefaultUserID)
		if lastID == "" {
			return fmt.Errorf("no previous session found to continue")
		}
		flagSession = lastID
	}

	// Interactive mode: show TUI immediately, initialize in background.
	if mode == "interactive" {
		return runInteractive(cmd.Context(), cfg, llm, info, reg, activeRole, cwd, sandboxRoot, debugTracer)
	}

	// Non-interactive modes: synchronous initialization.
	return runNonInteractive(cmd.Context(), cmd, cfg, llm, info, cwd, sandboxRoot, mode, prompt)
}

// runNonInteractive performs synchronous initialization and runs print/json/rpc modes.
func runNonInteractive(
	parentCtx context.Context,
	cmd *cobra.Command,
	cfg config.Config,
	llm adkmodel.LLM,
	info provider.Info,
	cwd, sandboxRoot, mode, prompt string,
) error {
	sandbox, err := tools.NewSandbox(sandboxRoot)
	if err != nil {
		return fmt.Errorf("creating sandbox: %w", err)
	}
	defer func() { _ = sandbox.Close() }()

	var instruction string
	if flagSystem != "" {
		instruction = flagSystem
	} else {
		instruction = agent.LoadInstruction(agent.SystemInstruction)
	}

	compactorCfg := tools.DefaultCompactorConfig()
	if cfg.Compactor != nil {
		if cfg.Compactor.Enabled != nil {
			compactorCfg.Enabled = *cfg.Compactor.Enabled
		}
		if cfg.Compactor.SourceCodeFiltering != "" {
			compactorCfg.SourceCodeFiltering = cfg.Compactor.SourceCodeFiltering
		}
		if cfg.Compactor.MaxChars > 0 {
			compactorCfg.MaxChars = cfg.Compactor.MaxChars
		}
		if cfg.Compactor.MaxLines > 0 {
			compactorCfg.MaxLines = cfg.Compactor.MaxLines
		}
	}
	compactorMetrics := tools.NewCompactMetrics()
	compactorCB := tools.BuildCompactorCallback(compactorCfg, compactorMetrics)

	runtime, err := extension.BuildRuntime(parentCtx, extension.RuntimeConfig{
		Config:          cfg,
		WorkDir:         cwd,
		Sandbox:         sandbox,
		BaseInstruction: instruction,
	})
	if err != nil {
		return fmt.Errorf("building extension runtime: %w", err)
	}
	afterCBs := append(runtime.AfterToolCallbacks, compactorCB)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}
	sessionsDir := filepath.Join(homeDir, ".pi-go", "sessions")
	sessionSvc, err := pisession.NewFileService(sessionsDir)
	if err != nil {
		return fmt.Errorf("creating session service: %w", err)
	}

	ag, err := agent.New(agent.Config{
		Model:               llm,
		Tools:               runtime.Tools,
		Toolsets:            runtime.Toolsets,
		Instruction:         runtime.Instruction,
		SessionService:      sessionSvc,
		BeforeToolCallbacks: runtime.BeforeToolCallbacks,
		AfterToolCallbacks:  afterCBs,
	})
	if err != nil {
		return fmt.Errorf("creating agent: %w", err)
	}

	ctx, stop := signal.NotifyContext(parentCtx, os.Interrupt)
	defer stop()

	sessionID := flagSession
	if flagContinue {
		lastID := sessionSvc.LastSessionID(agent.AppName, agent.DefaultUserID)
		if lastID == "" {
			return fmt.Errorf("no previous session found to continue")
		}
		sessionID = lastID
		fmt.Fprintf(os.Stderr, "go-pi: continuing session %s\n", sessionID)
	}
	if sessionID == "" {
		sessionID, err = ag.CreateSession(ctx)
		if err != nil {
			return fmt.Errorf("creating session: %w", err)
		}
	}
	if err := runtime.RunLifecycleHooks(ctx, extension.LifecycleEventSessionStart, map[string]any{"session_id": sessionID, "mode": mode}); err != nil {
		return fmt.Errorf("running extension session_start hooks: %w", err)
	}

	sessionLog, err := logger.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-pi: warning: could not create session log: %v\n", err)
	}
	defer func() { _ = sessionLog.Close() }()
	sessionLog.SessionStart(sessionID, llm.Name(), mode)

	switch mode {
	case "rpc":
		srv := jsonrpc.NewServer(jsonrpc.Config{
			Agent:      ag,
			SocketPath: flagSocket,
		})
		return srv.Run(ctx)
	case "json":
		if prompt == "" {
			fmt.Fprintf(os.Stderr, "go-pi: no prompt provided (model: %s, mode: %s)\n", llm.Name(), mode)
			return nil
		}
		return runJSON(ctx, ag, sessionID, prompt, sessionLog)
	default:
		if prompt == "" {
			fmt.Fprintf(os.Stderr, "go-pi: no prompt provided (model: %s, mode: %s)\n", llm.Name(), mode)
			return nil
		}
		return runPrint(ctx, ag, sessionID, prompt, sessionLog)
	}
}

func ensureRolesMap(cfg *config.Config) {
	if cfg != nil && cfg.Roles == nil {
		cfg.Roles = map[string]config.RoleConfig{}
	}
}

func applyModelOverride(cfg *config.Config, activeRole, model string) {
	if cfg == nil || model == "" {
		return
	}
	ensureRolesMap(cfg)
	cfg.Roles[activeRole] = config.RoleConfig{Model: model}
}

// detectMode returns the default output mode based on terminal state.
// If stdin is not a terminal, defaults to "print" for piped input.
func detectMode() string {
	if fi, err := os.Stdin.Stat(); err == nil {
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			return "print"
		}
	}
	return "interactive"
}

// runPrint runs the agent and prints text responses to stdout.
// Tool calls are shown as status lines on stderr.
func runPrint(ctx context.Context, ag *agent.Agent, sessionID, prompt string, log *logger.Logger) error {
	log.UserMessage(prompt)
	retryCfg := agent.DefaultRetryConfig()
	sawStreamedText := false
	for ev, err := range agent.WithRetry(retryCfg, func() iter.Seq2[*session.Event, error] {
		return ag.RunStreaming(ctx, sessionID, prompt)
	}) {
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "\ninterrupted")
				return nil
			}
			log.Error(err.Error())
			return fmt.Errorf("agent run: %w", err)
		}
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, part := range ev.Content.Parts {
			if part.Text != "" && ev.Content.Role == "thinking" {
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m", part.Text)
				continue
			}
			if part.Text != "" {
				if ev.Partial {
					sawStreamedText = true
					fmt.Print(part.Text)
					log.LLMText(ev.Author, part.Text)
				} else if !sawStreamedText {
					fmt.Print(part.Text)
					log.LLMText(ev.Author, part.Text)
				}
			}
			if part.FunctionCall != nil {
				sawStreamedText = false
				fmt.Fprintf(os.Stderr, "⚙ tool: %s\n", part.FunctionCall.Name)
				log.ToolCall(ev.Author, part.FunctionCall.Name, part.FunctionCall.Args)
			}
			if part.FunctionResponse != nil {
				sawStreamedText = false
				fmt.Fprintf(os.Stderr, "✓ tool: %s done\n", part.FunctionResponse.Name)
				log.ToolResult(ev.Author, part.FunctionResponse.Name, fmt.Sprintf("%v", part.FunctionResponse.Response))
			}
		}
	}
	fmt.Println()
	return nil
}

// jsonEvent represents a JSONL event for JSON output mode.
// Event types: message_start, thinking_delta, text_delta, tool_call, tool_result, message_end.
type jsonEvent struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Role      string `json:"role,omitempty"`
	Delta     string `json:"delta,omitempty"`
	Content   string `json:"content,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput any    `json:"tool_input,omitempty"`
}

// runJSON runs the agent and emits JSONL events to stdout.
// Events: message_start (once), text_delta (per text chunk), tool_call, tool_result, message_end (once).
func runJSON(ctx context.Context, ag *agent.Agent, sessionID, prompt string, log *logger.Logger) error {
	log.UserMessage(prompt)
	enc := json.NewEncoder(os.Stdout)
	started := false
	sawStreamedText := false

	retryCfg := agent.DefaultRetryConfig()
	for ev, err := range agent.WithRetry(retryCfg, func() iter.Seq2[*session.Event, error] {
		return ag.RunStreaming(ctx, sessionID, prompt)
	}) {
		if err != nil {
			if ctx.Err() != nil {
				_ = enc.Encode(jsonEvent{Type: "message_end"})
				return nil
			}
			log.Error(err.Error())
			return fmt.Errorf("agent run: %w", err)
		}
		if ev == nil || ev.Content == nil {
			continue
		}

		// Emit message_start on the first event from the assistant.
		if !started {
			_ = enc.Encode(jsonEvent{
				Type:  "message_start",
				Agent: ev.Author,
				Role:  ev.Content.Role,
			})
			started = true
		}

		for _, part := range ev.Content.Parts {
			if part.Text != "" && ev.Content.Role == "thinking" {
				_ = enc.Encode(jsonEvent{
					Type:  "thinking_delta",
					Agent: ev.Author,
					Delta: part.Text,
				})
				continue
			}
			if part.Text != "" {
				if ev.Partial {
					sawStreamedText = true
					_ = enc.Encode(jsonEvent{
						Type:  "text_delta",
						Agent: ev.Author,
						Delta: part.Text,
					})
					log.LLMText(ev.Author, part.Text)
				} else if !sawStreamedText {
					_ = enc.Encode(jsonEvent{
						Type:  "text_delta",
						Agent: ev.Author,
						Delta: part.Text,
					})
					log.LLMText(ev.Author, part.Text)
				}
			}
			if part.FunctionCall != nil {
				sawStreamedText = false
				_ = enc.Encode(jsonEvent{
					Type:      "tool_call",
					Agent:     ev.Author,
					ToolName:  part.FunctionCall.Name,
					ToolInput: part.FunctionCall.Args,
				})
				log.ToolCall(ev.Author, part.FunctionCall.Name, part.FunctionCall.Args)
			}
			if part.FunctionResponse != nil {
				sawStreamedText = false
				_ = enc.Encode(jsonEvent{
					Type:     "tool_result",
					Agent:    ev.Author,
					ToolName: part.FunctionResponse.Name,
					Content:  fmt.Sprintf("%v", part.FunctionResponse.Response),
				})
				log.ToolResult(ev.Author, part.FunctionResponse.Name, fmt.Sprintf("%v", part.FunctionResponse.Response))
			}
		}
	}
	_ = enc.Encode(jsonEvent{Type: "message_end"})
	return nil
}

// mergeExtraHeaders merges config extraHeaders with CLI --header flags.
// CLI flags override config values on key conflict.
func mergeExtraHeaders(cfgHeaders map[string]string, cliHeaders []string) map[string]string {
	if len(cfgHeaders) == 0 && len(cliHeaders) == 0 {
		return nil
	}
	merged := make(map[string]string)
	for k, v := range cfgHeaders {
		merged[k] = v
	}
	for _, h := range cliHeaders {
		key, val, ok := strings.Cut(h, "=")
		if ok {
			merged[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func headerMapToPairs(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(headers))
	for k, v := range headers {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}

// convertHooks converts config.HookConfig to extension.HookConfig.
func convertHooks(cfgHooks []config.HookConfig) []extension.HookConfig {
	hooks := make([]extension.HookConfig, len(cfgHooks))
	for i, h := range cfgHooks {
		hooks[i] = extension.HookConfig{
			Event:   h.Event,
			Command: h.Command,
			Tools:   h.Tools,
			Timeout: h.Timeout,
		}
	}
	return hooks
}

// detectGitRoot returns the git repository root for the given directory,
// or empty string if not inside a git repo.
func detectGitRoot(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// loadDotEnv loads environment variables from ~/.pi-go/.env.
// This file is written by the /login command.
func loadDotEnv() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(home, ".pi-go", ".env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Don't override existing env vars.
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
