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

- **Multi-provider LLM** — Claude (Anthropic), GPT/O-series (OpenAI), Gemini (Google), and Ollama for local models
- **Sandboxed tools** — File read/write/edit, shell execution, grep, find, tree, and git operations, all restricted to the project directory via `os.Root`
- **Interactive TUI** — Bubble Tea v2 terminal UI with Markdown rendering (Glamour), focused slash commands, and theming
- **Session persistence** — JSONL append-only event logs with branching, compaction, and resume
- **Model roles** — Named configurations (default, smol, slow, plan) selectable via CLI flags
- **Optional LSP integration** — In-tree JSON-RPC client and LSP tools for Go, TypeScript/JS, Python, Rust that can be wired in by extensions or custom startup code
- **Core Git visibility** — Repository overview, file diffs, and hunk parsing remain available as tools without prescribing commit workflows
- **RPC server** — Unix socket JSON-RPC 2.0 for IDE/editor integration
- **Extension runtime** — Manifest-discovered extensions are the primary customization surface, contributing prompt fragments, hooks, MCP toolsets, skills, and narrow TUI slash commands
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
├── rpc/            Unix socket JSON-RPC 2.0 server
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
- At least one LLM provider API key (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`) or a running Ollama instance

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
./pi --model claude:sonnet
./pi --model openai:gpt-4o
./pi --model gemini:gemini-2.5-pro
./pi --model ollama:qwen3.5:latest
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
- **Opt-in helper settings** — Internal helper packages may define extra config fields, but the default core path ignores policy/workflow-specific helpers unless custom startup code wires them in

## Extensions

See [docs/extensions.md](docs/extensions.md) for extension discovery, manifest format, tool/hook lifecycle contributions, and narrow TUI command integration.

## License

See [LICENSE](LICENSE) for details.
