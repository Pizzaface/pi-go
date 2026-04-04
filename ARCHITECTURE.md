# pi-go Architecture

## Overview

pi-go is a coding agent built on [Google ADK Go](https://google.golang.org/adk) with multi-provider LLM support, sandboxed tool execution, session persistence, an interactive terminal UI, optional extension points, and a subagent orchestration system.

## Package Structure

```
pi-go/
├── cmd/pi/main.go                  # Entry point → cli.Execute()
└── internal/
    ├── agent/                       # ADK agent setup, retry logic
    ├── audit/                       # Optional hidden-character scanner helpers kept in-tree
    ├── auth/                        # OAuth PKCE/device-code login flows
    ├── cli/                         # CLI flags, output modes, wiring
    ├── config/                      # Config loading (global + project), model roles
    ├── extension/                    # Hooks, skills, optional MCP building blocks
    ├── guardrail/                    # Optional token-guardrail helpers kept in-tree
    ├── lsp/                         # Optional LSP integration (protocol, client, manager, languages, hooks)
    ├── logger/                      # Session logging to ~/.pi-go/log/
    ├── memory/                      # Optional persistent memory subsystem (not wired into default startup)
    ├── provider/                    # LLM providers (Anthropic, OpenAI, Gemini)
    ├── jsonrpc/                     # Unix socket JSON-RPC server
    ├── session/                     # JSONL persistence, branching, compaction
    ├── tools/                       # Sandboxed tools (read, write, edit, bash, grep, find, ls, tree, git) plus optional LSP helpers
    └── tui/                         # Bubble Tea v2 interactive UI
```

## Dependency Graph

```mermaid
graph TD
    main["cmd/pi/main.go"] --> cli["cli"]
    cli --> agent["agent"]
    cli --> config["config"]
    cli --> provider["provider"]
    cli --> tools["tools"]
    cli --> extension["extension"]
    cli --> session["session"]
    cli --> tui["tui"]
    cli --> rpc["rpc"]
    cli -. optional .-> lsp["lsp"]
    cli --> auth["auth"]
    cli --> logger["logger"]

    extension -. skill-load auditing .-> audit["audit"]

    agent --> adk_runner["ADK runner"]
    agent --> adk_llmagent["ADK llmagent"]
    agent --> adk_session["ADK session"]

    provider --> anthropic_sdk["anthropic-sdk-go"]
    provider --> openai_sdk["openai-go"]
    provider --> adk_gemini["ADK model/gemini"]

    tools --> sandbox["os.Root sandbox"]
    tools --> adk_tool["ADK tool/functiontool"]


    lsp --> config

    tui --> bubbletea["Bubble Tea v2"]
    tui --> glamour["Glamour (markdown)"]
    tui --> agent

    rpc --> agent

    extension --> mcp_sdk["MCP Go SDK"]

    session --> adk_session

    guardrail -. optional wrapper .-> provider

    audit -. optional scan helper .-> extension

    logger --> cli

    style main fill:#2d5016,color:#fff
    style cli fill:#1a3a5c,color:#fff
    style agent fill:#1a3a5c,color:#fff
    style provider fill:#5c1a3a,color:#fff
    style tools fill:#3a5c1a,color:#fff
    style tui fill:#5c3a1a,color:#fff
    style session fill:#1a5c5c,color:#fff
    style subagent fill:#3a1a5c,color:#fff
    style lsp fill:#5c5c1a,color:#fff
    style guardrail fill:#5c5c1a,color:#fff
    style auth fill:#5c3a5c,color:#fff
    style audit fill:#3a5c5c,color:#fff
    style logger fill:#5c5c5c,color:#fff
```

## Request Flow

```mermaid
sequenceDiagram
    participant U as User
    participant CLI as CLI / TUI
    participant A as Agent
    participant R as ADK Runner
    participant LLM as LLM Provider
    participant T as Tool (sandboxed)
    participant S as Session Store
    participant LSP as Optional LSP Integration

    U->>CLI: prompt text
    CLI->>A: Run(ctx, sessionID, message)
    A->>R: runner.Run(content)
    R->>LLM: GenerateContent(req)
    LLM-->>R: Response (text or tool call)

    alt Tool Call
        R->>T: Execute tool
        T-->>R: Tool result
        R->>LLM: GenerateContent(with tool result)
        LLM-->>R: Final text response
    end

    opt Optional LSP wiring
        T->>LSP: Call opt-in LSP tool or callback
        LSP-->>T: Diagnostics / formatting / symbol data
    end

    R->>S: AppendEvent(event)
    R-->>A: yield events
    A-->>CLI: iter.Seq2[Event, error]
    CLI-->>U: render output
```

## Tool System

```mermaid
graph LR
    subgraph Sandbox["os.Root Sandbox (cwd)"]
        read["read<br/>Read file with line numbers"]
        write["write<br/>Write/create file"]
        edit["edit<br/>Find & replace in file"]
        ls["ls<br/>List directory"]
        tree["tree<br/>Directory tree view"]
        find["find<br/>Glob file search"]
        grep["grep<br/>Regex content search"]
    end

    subgraph GitTools["Git Tools"]
        git_overview["git-overview<br/>Repo status & info"]
        git_file_diff["git-file-diff<br/>Unified file diff"]
        git_hunk["git-hunk<br/>Parsed diff hunks"]
    end

    subgraph LSPTools["LSP Tools"]
        lsp_diag["lsp-diagnostics<br/>Errors & warnings"]
        lsp_def["lsp-definition<br/>Go to definition"]
        lsp_ref["lsp-references<br/>Find references"]
        lsp_hover["lsp-hover<br/>Type info & docs"]
        lsp_sym["lsp-symbols<br/>Document symbols"]
    end

    bash["bash<br/>Shell command<br/>(runs in sandbox dir)"]
    registry["CoreTools(sandbox)"] --> read
    registry --> write
    registry --> edit
    registry --> bash
    registry --> grep
    registry --> find
    registry --> ls
    registry --> tree
    registry --> git_overview
    registry --> git_file_diff
    registry --> git_hunk

    lsp_registry["LSPTools(manager)"] --> lsp_diag
    lsp_registry --> lsp_def
    lsp_registry --> lsp_ref
    lsp_registry --> lsp_hover
    lsp_registry --> lsp_sym

    style Sandbox fill:#1a2a1a,stroke:#4a4,color:#fff
    style GitTools fill:#1a1a2a,stroke:#44a,color:#fff
    style LSPTools fill:#2a1a1a,stroke:#a44,color:#fff
    style registry fill:#333,color:#fff
    style lsp_registry fill:#333,color:#fff
```

All file tools operate through the `Sandbox` which uses Go's `os.Root` to restrict access to the working directory tree. Paths cannot escape via `..` or symlinks.

| Tool | Input | Output | Limits |
|------|-------|--------|--------|
| read | file_path, offset, limit | content, total_lines | 2000 lines default, 100KB |
| write | file_path, content | path, bytes_written | Auto-creates parent dirs |
| edit | file_path, old_string, new_string | path, replacements | Unique match required |
| bash | command, timeout | stdout, stderr, exit_code | 2min default, 10min max |
| grep | pattern, path, glob | matches, total_matches | 200 matches max |
| find | pattern, path | files, total_files | 500 results max |
| ls | path | entries (name, is_dir, size) | — |
| tree | path, depth | tree, dirs, files | Depth 10 max, 500 entries |
| git-overview | — | branch, commits, staged, unstaged, untracked | 10s timeout |
| git-file-diff | file, staged | diff | 10s timeout |
| git-hunk | file, staged | hunks (header, content, lines) | 10s timeout |

## Model Roles

The model roles system maps abstract role names to specific LLM models, enabling different components to use appropriate models for their task complexity.

```
config.json:
{
  "roles": {
    "default": { "model": "claude-sonnet-4-20250514" },
    "smol":    { "model": "claude-haiku-3-20240307" },
    "plan":    { "model": "claude-sonnet-4-20250514" },
    "slow":    { "model": "claude-opus-4-20250514" }
  }
}
```

`ResolveRole(role)` resolves a role name to a model and provider. Falls back to "default" role if the requested role is not configured. The provider is auto-detected from the model name prefix (claude→anthropic, gpt/o1-4→openai, gemini→gemini).

CLI flags `--smol`, `--plan`, `--slow` override the active role for a single invocation.

## Optional LSP Integration

The LSP system remains available in-tree, but it is no longer part of default core startup. Extensions or custom startup code can opt in to two pieces:

**Hooks** (opt-in, via `BuildLSPAfterToolCallback`):
- **Format-on-write**: After `write` tool calls, requests formatting from the language server and applies edits (5s timeout)
- **Diagnostics-on-edit**: After file modifications, collects compiler errors/warnings with a 2s delay for server processing

**Explicit tools** (opt-in, via `tools.LSPTools`):
- `lsp-diagnostics` — Get errors and warnings for a file
- `lsp-definition` — Go to definition of symbol at position
- `lsp-references` — Find all references to a symbol
- `lsp-hover` — Get type information and documentation
- `lsp-symbols` — List all symbols in a file

The `Manager` starts language servers on demand based on file extension, caches connections, and shuts them down on exit. Supported languages: Go (gopls), TypeScript (typescript-language-server), Python (pylsp), Rust (rust-analyzer).

## Provider System

```mermaid
graph TD
    config["config roles + providers/models"] --> registry["provider registry"]
    resources["discoverable models/*.json resources"] --> registry
    builtin["built-in compatible families"] --> registry
    registry --> resolve["Resolve(model, provider?)"]

    resolve --> anthropic["Anthropic family<br/>anthropic-sdk-go"]
    resolve --> openai["OpenAI family<br/>openai-go"]
    resolve --> gemini["Gemini family<br/>ADK native"]
    resolve --> ollama["Ollama family<br/>native Ollama API"]

    anthropic --> llm["model.LLM interface"]
    openai --> llm
    gemini --> llm
    ollama --> llm

    llm --> agent["Agent"]

    style registry fill:#333,color:#fff
    style resolve fill:#333,color:#fff
    style llm fill:#1a3a5c,color:#fff
```

Provider selection is now data-driven: built-ins seed a small compatibility registry, discoverable `models/*.json` resources extend or override it, and config-local `providers` / `models` apply last. That keeps startup generic while still allowing aliases, alternate compatible backends, custom base URLs, and provider-specific default headers without editing core resolution logic.

Each provider family implements the ADK `model.LLM` interface:

```go
type LLM interface {
    Name() string
    GenerateContent(ctx, req *LLMRequest, stream bool) iter.Seq2[*LLMResponse, error]
}
```

Built-in families still default to their usual environment variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY`, etc.), but compatible custom providers can declare their own API key env vars, base URL env vars/defaults, ping endpoints, and default headers through the registry.

## Session Management

```mermaid
graph TD
    subgraph Storage["~/.pi-go/sessions/"]
        subgraph Session["<session-uuid>/"]
            meta["meta.json<br/>ID, AppName, UserID,<br/>WorkDir, Model, timestamps"]
            events["events.jsonl<br/>Append-only event log"]
            subgraph Branches["branches/"]
                main["main/events.jsonl"]
                feat["feature-x/events.jsonl"]
            end
            bstate["branches.json<br/>Active branch state"]
        end
    end

    create["CreateSession"] --> meta
    create --> events
    append["AppendEvent"] --> events
    branch["CreateBranch"] --> Branches
    branch --> bstate
    compact["Compact"] -->|"summarize old events"| events

    style Storage fill:#0a1a2a,color:#fff
    style Session fill:#1a2a3a,color:#fff
```

- **Persistence**: JSONL append-only event log per session
- **Branching**: Fork conversations, switch between branches
- **Compaction**: Replace old events with summary when token count exceeds threshold
- **Resume**: `--continue` resumes last session, `--session <id>` resumes specific session

## Output Modes

```mermaid
graph LR
    agent["Agent Events"] --> mode{Output Mode}
    mode -->|"interactive<br/>(tty default)"| tui["TUI<br/>Bubble Tea + Markdown"]
    mode -->|"print<br/>(pipe default)"| print["Print<br/>Text → stdout<br/>Status → stderr"]
    mode -->|"json"| json["JSON<br/>JSONL streaming events"]
    mode -->|"rpc"| rpc["RPC<br/>Unix socket JSON-RPC 2.0"]

    style mode fill:#333,color:#fff
```

**JSON event types**: `message_start`, `thinking_delta`, `text_delta`, `tool_call`, `tool_result`, `message_end`

## Extension System

```mermaid
graph TD
    subgraph Runtime["Extension Runtime"]
        discovery["Resource discovery<br/>packages + loose dirs"]
        prompts["Prompt fragments<br/>prompt / prompt_file"]
        prompt_templates["Prompt templates<br/>prompts/*.md"]
        models["Provider/model registries<br/>models/*.json"]
        hooks["Tool hooks<br/>before_tool / after_tool"]
        lifecycle["Lifecycle hooks<br/>startup / session_start"]
        skills["Skills<br/>skills_dir + resource dirs"]
        mcp["Tool registration<br/>mcp_servers"]
        tui["Narrow TUI points<br/>slash commands only"]
        themes["Theme resources<br/>themes/*.json"]
    end

    discovery --> prompts
    discovery --> prompt_templates
    discovery --> models
    discovery --> hooks
    discovery --> lifecycle
    discovery --> skills
    discovery --> mcp
    discovery --> tui
    discovery --> themes

    prompts --> agent["Agent instruction"]
    hooks --> agent
    lifecycle --> agent
    skills --> agent
    mcp --> agent
    tui --> tui_shell["Bubble Tea shell"]

    style Runtime fill:#1a1a2a,color:#fff
```

The extension runtime is now the **primary customization surface** for pi-go.

**Discovery**: `DiscoverResourceDirs(...)` builds an ordered list of global and project resource directories, including installed packages under `packages/*/`. Later directories override earlier ones by resource name.

**Prompt contributions**: Extensions can append system-instruction fragments with `prompt` or `prompt_file`.

**Prompt templates**: Markdown files in discoverable `prompts/` directories are loaded as first-class `PromptTemplate` resources and exposed to the TUI through the existing slash-command seam.

**Provider/model registries**: JSON files in discoverable `models/` directories extend the built-in provider registry with compatible provider definitions, exact model aliases, custom base URLs, env names, and default headers.

**Tool hooks**: `before_tool` and `after_tool` shell hooks are merged into the agent callback chain.

**Lifecycle hooks**: `startup` and `session_start` hooks let extensions participate in bootstrap without expanding the core.

**Skills**: Extensions can point at a `skills_dir` containing `SKILL.md` folders, and skills can also arrive through discovered resource directories and installed packages.

**Themes**: The TUI overlays discoverable `themes/*.json` resources on top of embedded themes, with project resources overriding global ones.

**Tool registration**: Extension-owned tools should come in through `mcp_servers`, which are bridged into ADK toolsets by the runtime.

**TUI extension points**: The TUI deliberately stays narrow. Extensions and prompt templates may contribute slash commands, but they do not register custom widgets or replace the Bubble Tea model.

See [docs/extensions.md](docs/extensions.md) for the authoring guide.

## Configuration

```
~/.pi-go/config.json          # Global config
.pi-go/config.json             # Project config (overrides global)
.pi-go/AGENTS.md               # Project-specific agent instructions
~/.pi-go/skills/*.SKILL.md     # Global skills
.pi-go/skills/*.SKILL.md       # Project skills (override global)
~/.pi-go/extensions/*/extension.json  # Global extension manifests
.pi-go/extensions/*/extension.json    # Project extension manifests
~/.pi-go/models/*.json                # Global provider/model registry resources
.pi-go/models/*.json                  # Project provider/model registry resources
~/.pi-go/packages/*/                  # Global resource packages
.pi-go/packages/*/                    # Project resource packages
~/.pi-go/sessions/             # Session storage
~/.pi-go/log/                  # Session logs
~/.pi-go/.env                  # API keys (written by /login)
```

Planning and SOP directories are no longer part of core configuration. Any spec-driven or SOP-driven workflow is expected to come from extensions, prompts, or external packages.

**Configuration schema** (`config.json`):
```json
{
  "roles": { "default": {...}, "smol": {...} },
  "hooks": [...],
  "compactor": { "enabled": true }
}
```

Optional integrations may define additional config fields such as `mcp` or helper-specific settings, but they are not part of the minimal core path.

## Initialization Flow

The TUI uses a **deferred initialization** pattern to show the UI immediately while initializing subsystems in the background:

```mermaid
sequenceDiagram
    participant TUI as TUI (Bubble Tea)
    participant Init as Deferred Init Goroutine
    participant Runtime as Extension Runtime
    participant Git as Git
    participant Agent as Agent Builder
    participant LSP as Optional LSP Package

    TUI->>Init: Start background init
    Init->>Runtime: Phase 1: Create sandbox + build extension runtime
    par Parallel Initialization
        Init->>Git: Detect repo and diff stats
        Init->>Runtime: Discover manifests, skills, hooks, and MCP toolsets
    end
    Init->>Agent: Phase 3: Build orchestrator + agent from runtime output
    opt Custom startup or extension wires LSP
        Agent->>LSP: Register manager, tools, callback
    end
    Init->>TUI: InitEvent{Result: InitResult}
    TUI->>User: Ready to accept input
```

**Key patterns:**
- TUI starts immediately with spinner showing initialization progress
- Heavy I/O operations run in parallel for the minimal path (git + extension discovery)
- Agent is created last after the extension runtime has assembled tools, hooks, skills, prompt fragments, prompt templates, and TUI commands
- LSP remains opt-in; extension-owned MCP toolsets are assembled by the extension runtime when manifests declare them
- Progress sent via `InitEvent` channel

## Retry & Error Handling

```mermaid
graph TD
    call["LLM Call"] --> check{Error?}
    check -->|No| done["Success"]
    check -->|Yes| transient{Transient?}
    transient -->|"429, 5xx,<br/>timeout, reset"| retry["Wait (exp backoff)<br/>1s → 2s → 4s"]
    transient -->|"400, auth,<br/>other"| fail["Fail immediately"]
    retry --> attempt{Retries<br/>exhausted?}
    attempt -->|No| call
    attempt -->|Yes| fail

    style retry fill:#5c5c1a,color:#fff
    style fail fill:#5c1a1a,color:#fff
    style done fill:#1a5c1a,color:#fff
```

Defaults: 3 retries, 1s initial delay, 30s max delay. Partial results prevent retry to preserve data integrity.

## TUI Architecture

```mermaid
graph TD
    subgraph BubbleTea["Bubble Tea v2"]
        init["Init()"] --> loop["Update/View Loop"]
        loop --> key["KeyPressMsg"]
        loop --> agent_msg["agentMsg (channel)"]
        loop --> resize["WindowSizeMsg"]
    end

    key -->|Enter| submit["submit()"]
    key -->|"/cmd"| slash["handleSlashCommand()"]
    submit --> goroutine["Agent goroutine"]
    goroutine -->|"agentTextMsg<br/>agentToolCallMsg<br/>agentToolResultMsg<br/>agentDoneMsg"| agent_msg

    agent_msg --> render["View()"]
    render --> messages["renderMessages()"]
    render --> status["renderStatusBar()"]
    render --> input["renderInput()"]

    messages --> markdown["Glamour<br/>Markdown Render"]

    style BubbleTea fill:#1a2a1a,color:#fff
```

**Slash commands**: `/help`, `/clear`, `/model`, `/session`, `/context`, `/branch`, `/compact`, `/history`, `/login`, `/skills`, `/skill-create`, `/skill-load`, `/skill-list`, `/theme`, `/ping`, `/restart`, `/exit`, `/quit`

**Keyboard**: Enter (submit), Ctrl+C/Esc (quit), Up/Down (history), PgUp/PgDown (scroll)

## Optional helper packages

The repo still keeps a few non-core helpers available for custom startup code, but they are no longer part of the default CLI/TUI surface:

- `internal/guardrail/` can still wrap an LLM with daily token tracking and limits
- `internal/audit/` can still scan skill files for hidden Unicode/security issues
- compactor metrics can still be collected internally without exposing dedicated `/rtk` UI commands
- commit-generation helpers remain available in-tree for custom shells or extensions, but default TUI no longer ships a `/commit` workflow

Default core behavior now avoids assuming any policy or workflow opinion beyond generic harness essentials.

## Authentication System

```mermaid
graph TD
    subgraph Providers["OAuth Providers"]
        anthropic["Anthropic"]
        openai["OpenAI"]
        codex["OpenAI Codex"]
        gemini["Google Gemini"]
    end

    subgraph Flows["Auth Flows"]
        pkce["PKCE Flow"]
        device["Device Code Flow"]
        pkce --> token["Token → API Key"]
        device --> token
    end

    subgraph Storage["Storage"]
        token --> dotenv["~/.pi-go/.env"]
    end

    style anthropic fill:#1a3a5c,color:#fff
    style openai fill:#3a5c1a,color:#fff
    style codex fill:#5c3a1a,color:#fff
    style gemini fill:#5c1a3a,color:#fff
    style dotenv fill:#1a1a2a,color:#fff
```

**Features:**
- **OAuth PKCE flow**: Browser-based authorization for Anthropic, Google
- **Device code flow**: CLI-friendly flow for OpenAI
- **TLS preflight**: Detects certificate chain issues for OpenAI OAuth
- **Key storage**: Saves API keys to `~/.pi-go/.env`

**CLI command**: `/login [provider]` in TUI

## Logger System

```mermaid
graph TD
    subgraph Session["Session Logging"]
        user["User Message"] --> logger
        llm["LLM Text"] --> logger
        tool["Tool Call"] --> logger
        result["Tool Result"] --> logger
    end

    logger --> logfile["~/.pi-go/log/yyyy-mm-dd/session-HH-MM-SS.log"]

    style logger fill:#1a3a5c,color:#fff
    style logfile fill:#1a1a2a,color:#fff
```

**Features:**
- **Structured JSON logs**: Machine-parseable event log
- **Entry types**: `session_start`, `user`, `llm_text`, `tool_call`, `tool_result`, `error`, `info`
- **File location**: `~/.pi-go/log/YYYY-MM-DD/session-HH-MM-SS.log`
- **Session metadata**: Session ID, model name, mode recorded at start

## Planning and workflow guidance

Planning workflows and subagent orchestration are no longer built into core. pi-go's core provides a generic chat TUI, tools, skills, extensions, and model roles; any spec-driven workflows, security-policy layers, or git-assistant flows should be layered on through prompts, skills, extensions, or external packages.

## Memory System

`internal/memory/` and the `mem-search` / `mem-timeline` / `mem-get` tools still exist in-tree, but they are no longer part of the default core bootstrap path.

Current core behavior:
- startup does **not** open a SQLite memory database
- startup does **not** start compression/background memory workers
- startup does **not** inject memory context into the base system prompt
- default tool registration does **not** expose memory search tools

If persistent memory returns in the future, it should be wired in explicitly as an optional subsystem or extension rather than assumed by core.
