# pi-go

[![CI](https://github.com/dimetron/pi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/dimetron/pi-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/dimetron/pi-go)](https://goreportcard.com/report/github.com/dimetron/pi-go)
[![Go Version](https://img.shields.io/github/go-mod/go-version/dimetron/pi-go)](go.mod)
[![License](https://img.shields.io/github/license/dimetron/pi-go)](LICENSE)
[![Release](https://img.shields.io/github/v/release/dimetron/pi-go?include_prereleases)](https://github.com/dimetron/pi-go/releases)
[![codecov](https://codecov.io/gh/dimetron/pi-go/graph/badge.svg)](https://codecov.io/gh/dimetron/pi-go)

A terminal-based coding agent built on [Google ADK Go](https://google.github.io/adk-go/) with multi-provider LLM support, sandboxed tool execution, optional extension points, and a subagent system.

![pi-go TUI](docs/screen/pi-go.png)

## Features

- **Multi-provider LLM** — Claude (Anthropic), GPT/O-series (OpenAI), Gemini (Google), Ollama for local models, plus data-driven compatible provider/model registration through config or discoverable `models/*.json` resources
- **Sandboxed tools** — File read/write/edit, shell execution, grep, find, tree, and git operations, all restricted to the project directory via `os.Root`
- **Interactive TUI** — Bubble Tea v2 terminal UI with Markdown rendering (Glamour), focused slash commands, built-in themes, and discoverable custom theme resources
- **Session persistence** — JSONL append-only event logs with branching, compaction, and resume
- **Model roles** — Named configurations (default, smol, slow, plan) selectable via CLI flags
- **Optional LSP integration** — In-tree JSON-RPC client and LSP tools for Go, TypeScript/JS, Python, Rust that can be wired in by extensions or custom startup code
- **Core Git visibility** — Repository overview, file diffs, and hunk parsing remain available as tools without prescribing commit workflows
- **RPC server** — Unix socket JSON-RPC 2.0 for IDE/editor integration
- **Extension runtime** — Manifest-discovered extensions and shareable resource packages are the primary customization surface, contributing prompt fragments, hooks, MCP toolsets, skills, prompt templates, themes, and narrow TUI slash commands
- **Minimal core startup** — Default startup wires core tools, sessions, and the extension runtime without assuming optional subsystems or policy/workflow layers such as LSP, persistent memory, token guardrails, or built-in commit/audit helpers

## Architecture

```
cmd/pi/             Entry point — CLI parsing, output mode selection
internal/
├── agent/          ADK agent setup, retry logic, runner
├── cli/            Cobra CLI flags, output modes (interactive, print, json, rpc)
├── config/         Global and project config (roles, hooks, themes, optional integration settings)
├── audit/          Optional skill-audit helpers kept in-tree for custom wiring
├── extension/      Hooks, skills, and optional MCP building blocks
├── guardrail/      Optional token-guardrail helpers kept in-tree for custom wiring
├── lsp/            Optional LSP JSON-RPC client, language registry, manager, hooks
├── provider/       LLM providers implementing genai model interface
├── jsonrpc/        Unix socket JSON-RPC 2.0 server
├── session/        JSONL persistence, branching, compaction
├── tools/          Sandboxed tools (read, write, edit, bash, grep, find, git) plus optional LSP helpers
└── tui/            Bubble Tea v2 UI and slash commands
```

`internal/memory/` remains in-tree as an optional subsystem, but the default core startup no longer initializes it or exposes memory tools automatically.

### Request flow

```
User input → CLI → Agent → LLM provider → Tool calls → Sandbox → Response → TUI
                     ↕
              Session store
              (JSONL events)

Optional capability:
Agent/extensions → LSP tools & hooks → LSP servers
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed documentation.

## Requirements

- Go 1.25+
- At least one LLM provider API key (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY` or `GEMINI_API_KEY`) or a running Ollama instance

## Build

```bash
make build      # build the pi binary
make test       # run unit tests
make lint       # go vet
make e2e        # run E2E integration tests
make clean      # remove binary
```

## Usage

```bash
# Default interactive mode
./pi

# Select a model by prefix
./pi --model claude-sonnet-4-6
./pi --model gpt-4o
./pi --model gemini-2.5-pro
./pi --model ollama/qwen3.5:latest
./pi --model minimax-m2.5:cloud #automatically detect ollama if :cloud

# Use model roles
./pi --smol          # fast, cheap model
./pi --slow          # most capable model
./pi --plan          # planning-oriented model

# Additional options
./pi --continue      # continue last session
./pi --session <id>  # resume specific session
./pi --system "..." # custom system instructions
./pi --url "..."    # custom API endpoint URL

# Non-interactive modes
./pi --mode print "explain this codebase"
./pi --mode json "list all TODO comments"
./pi --mode rpc --socket /tmp/pi-go.sock   # start RPC server

# Resource package lifecycle
./pi package install ~/Downloads/my-pi-package
./pi package install --project https://github.com/acme/pi-package.git
./pi package list
./pi package update my-pi-package
./pi package remove my-pi-package
```

### Slash commands

| Command          | Description                                |
|------------------|--------------------------------------------|
| `/help`          | Show available commands                   |
| `/clear`         | Clear conversation                        |
| `/model`         | Show current model and roles              |
| `/session`       | Show current session info                 |
| `/context`       | Show context usage                        |
| `/branch`        | Create a conversation branch              |
| `/compact`       | Compact session history                   |
| `/history`       | Show command history                      |
| `/login`         | Configure API keys                        |
| `/skills`        | List skill commands and available skills  |
| `/skill-create`  | Create a new skill                        |
| `/skill-list`    | List available skills                     |
| `/skill-load`    | Reload skills from disk                   |
| `/theme`         | List or switch themes                     |
| `/ping`          | Test model connectivity                   |
| `/restart`       | Restart pi-go                             |
| `/exit`          | Exit the agent                            |
| `/quit`          | Exit the agent                            |

Skills are automatically scanned on load — skills with critical findings (Unicode tags, BiDi overrides, variation selector attacks) are blocked from loading.

The scanner and related reporting code remain in-tree as optional building blocks, but the default CLI no longer exposes a dedicated `pi audit` workflow.

## Configuration

Pi looks for configuration in `~/.pi-go/config.json` (global) and `.pi-go/config.json` (project-local):

- **Model roles** — Map role names to specific model strings
- **Hooks** — Shell commands triggered on tool events (e.g., post-write formatting)
- **Themes** — Terminal color schemes via `theme` config field
- **Extension manifests** — place `extension.json` files under `~/.pi-go/extensions/<name>/` or `.pi-go/extensions/<name>/`
- **Shareable resources** — install packages into `~/.pi-go/packages/<name>/` or `.pi-go/packages/<name>/` with `extensions/`, `skills/`, `prompts/`, `themes/`, and `models/` subdirectories
- **Opt-in helper settings** — Internal helper packages may define extra config fields, but the default core path ignores policy/workflow-specific helpers unless custom startup code wires them in

## Extensions and resources

See [docs/extensions.md](docs/extensions.md) for extension discovery, resource directories, project-vs-global loading order, prompt templates, provider/model registry resources, package lifecycle, and narrow TUI command integration.

## Provider and model customization

Provider/model registration is now data-driven instead of hardcoded into startup.

You can customize compatible providers in either:

- `~/.pi-go/config.json` or `.pi-go/config.json` via `providers` / `models`
- discoverable `models/*.json` resources under:
  - `~/.pi-go/packages/*/models/`
  - `~/.pi-go/models/`
  - `.pi-go/packages/*/models/`
  - `.pi-go/models/`

`providers` declare a provider `name`, transport `family` (`anthropic`, `openai`, `gemini`, or `ollama`), env/base-URL settings, optional default headers, and model match rules. `models` declare exact aliases that map a friendly name to a provider and target model.

Example:

```json
{
  "providers": [
    {
      "name": "openrouter",
      "family": "openai",
      "api_key_env": ["OPENROUTER_API_KEY"],
      "base_url_env": "OPENROUTER_BASE_URL",
      "default_base_url": "https://openrouter.ai/api/v1",
      "ping_endpoint": "/models",
      "default_headers": {
        "HTTP-Referer": "https://example.com/my-pi-go"
      },
      "match": [
        { "prefix": "openrouter/", "strip_prefix": true }
      ]
    }
  ],
  "models": [
    {
      "name": "router-sonnet",
      "provider": "openrouter",
      "target": "anthropic/claude-sonnet-4"
    }
  ]
}
```

That lets you use either:

```bash
./pi --model router-sonnet
./pi --model openrouter/meta-llama/llama-4-maverick
```

This path is intentionally limited to providers that are wire-compatible with the built-in transport families. If a backend needs a custom SDK or auth flow, keep that as a new core family or a separate integration instead of forcing a generic plugin layer.

## License

See [LICENSE](LICENSE) for details.
