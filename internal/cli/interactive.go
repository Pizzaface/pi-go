package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	adkmodel "google.golang.org/adk/model"

	"github.com/dimetron/pi-go/internal/agent"
	"github.com/dimetron/pi-go/internal/config"
	"github.com/dimetron/pi-go/internal/extension"
	"github.com/dimetron/pi-go/internal/guardrail"
	"github.com/dimetron/pi-go/internal/logger"
	"github.com/dimetron/pi-go/internal/provider"
	pisession "github.com/dimetron/pi-go/internal/session"
	"github.com/dimetron/pi-go/internal/tools"
	"github.com/dimetron/pi-go/internal/tui"
)

// initResources tracks resources created during deferred init for cleanup.
type initResources struct {
	sandbox    *tools.Sandbox
	sessionLog *logger.Logger
}

func (r *initResources) cleanup() {
	if r.sessionLog != nil {
		_ = r.sessionLog.Close()
	}
	if r.sandbox != nil {
		_ = r.sandbox.Close()
	}
}

// runInteractive starts the TUI immediately and performs heavy initialization
// in a background goroutine, reporting progress via InitEvent channel.
func runInteractive(
	ctx context.Context,
	cfg config.Config,
	llm adkmodel.LLM,
	info provider.Info,
	reg *provider.Registry,
	activeRole, cwd, sandboxRoot string,
	debugTracer *provider.DebugTracer,
) error {
	initCh := make(chan tui.InitEvent, 32)

	var res initResources
	initDone := make(chan struct{})

	// Create a child context so deferred init is canceled when the TUI exits.
	initCtx, initCancel := context.WithCancel(ctx)

	go func() {
		defer close(initDone)
		defer close(initCh)
		deferredInit(initCtx, cfg, llm, cwd, sandboxRoot, initCh, &res)
	}()

	tuiErr := tui.Run(ctx, tui.Config{
		LLM:              llm,
		ModelName:        llm.Name(),
		ProviderName:     info.Provider,
		ActiveRole:       activeRole,
		Roles:            cfg.Roles,
		ProviderRegistry: reg,
		WorkDir:          cwd,
		ThemeName:        cfg.Theme,
		DeferredInit:     initCh,
		DebugTracer:      debugTracer,
	})

	initCancel() // signal deferred init to stop
	<-initDone
	res.cleanup()
	return tuiErr
}

// deferredInit performs all heavy initialization, sending progress via ch.
// Resources that need cleanup are stored in res.
func deferredInit(
	ctx context.Context,
	cfg config.Config,
	llm adkmodel.LLM,
	cwd, sandboxRoot string,
	ch chan<- tui.InitEvent,
	res *initResources,
) {
	send := func(item string, done bool) {
		ch <- tui.InitEvent{Item: item, Done: done}
	}
	fail := func(err error) {
		ch <- tui.InitEvent{Err: err}
	}

	// --- Phase 1: Core tools (fast, needed by everything) ---
	send("tools", false)

	sandbox, err := tools.NewSandbox(sandboxRoot)
	if err != nil {
		fail(fmt.Errorf("creating sandbox: %w", err))
		return
	}
	res.sandbox = sandbox

	screen := &tui.Screen{}
	restartCh := make(chan struct{}, 1)
	runtime, err := extension.BuildRuntime(ctx, extension.RuntimeConfig{
		Config:          cfg,
		WorkDir:         cwd,
		Sandbox:         sandbox,
		BaseInstruction: baseInstruction(),
		ScreenProvider:  screen,
		RestartFunc: func() {
			select {
			case restartCh <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		fail(fmt.Errorf("building extension runtime: %w", err))
		return
	}
	send("tools", true)

	// --- Phase 2: Parallel subsystems ---
	type parallelState struct {
		mu sync.Mutex

		// Git
		repoRoot    string
		gitBranch   string
		diffAdded   int
		diffRemoved int
	}
	var ps parallelState
	var wg sync.WaitGroup

	// Git discovery
	wg.Add(1)
	go func() {
		defer wg.Done()
		send("git", false)
		ps.repoRoot = detectGitRoot(cwd)
		ps.gitBranch = detectBranch(cwd)
		ps.diffAdded, ps.diffRemoved = computeDiffStats(cwd)
		send("git", true)
	}()

	send("skills", false)
	send("skills", true)

	wg.Wait()

	// --- Phase 3: Sequential finalization ---
	send("agent", false)

	// Build callbacks.
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
	afterCBs := append(runtime.AfterToolCallbacks, compactorCB)

	// Session service.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fail(fmt.Errorf("getting home dir: %w", err))
		return
	}
	sessionsDir := filepath.Join(homeDir, ".pi-go", "sessions")
	sessionSvc, err := pisession.NewFileService(sessionsDir)
	if err != nil {
		fail(fmt.Errorf("creating session service: %w", err))
		return
	}

	// Create token/usage tracker and wrap the LLM so every provider
	// response records actual token counts.
	tracker := guardrail.New(guardrail.DefaultMaxDailyTokens)
	if ctxLimit := provider.KnownContextWindow(llm.Name()); ctxLimit > 0 {
		tracker.SetContextLimit(ctxLimit)
	}
	wrappedLLM := guardrail.WrapModel(llm, tracker)

	// Create agent.
	ag, err := agent.New(agent.Config{
		Model:               wrappedLLM,
		Tools:               runtime.Tools,
		Toolsets:            runtime.Toolsets,
		Instruction:         runtime.Instruction,
		SessionService:      sessionSvc,
		BeforeToolCallbacks: runtime.BeforeToolCallbacks,
		AfterToolCallbacks:  afterCBs,
	})
	if err != nil {
		fail(fmt.Errorf("creating agent: %w", err))
		return
	}

	// Resolve session (--continue is resolved in fast path, flagSession is set).
	sessionID := flagSession
	if sessionID == "" {
		sessionID, err = ag.CreateSession(ctx)
		if err != nil {
			fail(fmt.Errorf("creating session: %w", err))
			return
		}
	}

	if err := runtime.RunLifecycleHooks(ctx, extension.LifecycleEventSessionStart, map[string]any{"session_id": sessionID, "mode": "interactive"}); err != nil {
		fail(fmt.Errorf("running extension session_start hooks: %w", err))
		return
	}

	// Session logger.
	sessionLog, logErr := logger.New()
	if logErr == nil {
		res.sessionLog = sessionLog
		sessionLog.SessionStart(sessionID, llm.Name(), "interactive")
	}

	send("agent", true)

	// Send final result.
	ch <- tui.InitEvent{
		Done: true,
		Result: &tui.InitResult{
			Agent:             ag,
			SessionID:         sessionID,
			SessionService:    sessionSvc,
			Logger:            sessionLog,
			Skills:            runtime.Skills,
			SkillDirs:         runtime.SkillDirs,
			ExtensionCommands: runtime.SlashCommands,
			TokenTracker:      tracker,
			WrapLLM: func(m adkmodel.LLM) adkmodel.LLM {
				return guardrail.WrapModel(m, tracker)
			},
			CompactMetrics: compactorMetrics,
			RestartCh:      restartCh,
			Screen:         screen,
			GitBranch:      ps.gitBranch,
			DiffAdded:      ps.diffAdded,
			DiffRemoved:    ps.diffRemoved,
		},
	}
}

// detectBranch returns the current git branch name.
func detectBranch(workDir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if workDir != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// computeDiffStats returns added and removed line counts from git diff,
// including lines from untracked files.
func computeDiffStats(cwd string) (added, removed int) {
	cmd := exec.Command("git", "diff", "--numstat", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var a, r int
		if _, err := fmt.Sscanf(line, "%d\t%d\t", &a, &r); err == nil {
			added += a
			removed += r
		}
	}
	added += countUntrackedLines(cwd)
	return added, removed
}

// countUntrackedLines counts total lines across untracked files.
func countUntrackedLines(cwd string) int {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	total := 0
	for _, file := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if file == "" {
			continue
		}
		wc := exec.Command("wc", "-l", file)
		wc.Dir = cwd
		wcOut, err := wc.Output()
		if err != nil {
			continue
		}
		var lines int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(wcOut)), "%d", &lines); err == nil {
			total += lines
		}
	}
	return total
}
