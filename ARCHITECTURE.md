# go-pi Architecture

## Overview

go-pi is a minimal coding-agent harness built on Google ADK Go.

The architecture is intentionally centered on a small core:

1. **agent loop**
2. **sandboxed tools**
3. **file-backed sessions**
4. **Bubble Tea terminal UI**
5. **resource / extension discovery**
6. **provider + model resolution**

Optional integrations can live in-tree, but the default startup path should stay small and predictable.

## Core shape

```text
go-pi/
├── cmd/pi/                  # CLI entry point (binary still built as `pi`)
└── internal/
    ├── agent/               # ADK runner + system instruction
    ├── cli/                 # Cobra flags, interactive/non-interactive wiring
    ├── config/              # global + project config loading
    ├── extension/           # resource discovery, manifests, packages, runtime
    ├── jsonrpc/             # RPC mode
    ├── provider/            # built-in families + compatible alias registry
    ├── session/             # JSONL sessions, branch state, compaction helpers
    ├── tools/               # sandboxed tools
    └── tui/                 # Bubble Tea/Bubbles-first terminal UX
```

Other directories such as `auth/`, `lsp/`, `audit/`, `guardrail/`, or `memory/` remain available as building blocks, but they are no longer assumed to be part of the default product story.

## Request flow

```text
User input
  → CLI/TUI
  → agent runner
  → provider/model
  → tool calls (optional)
  → sandbox / MCP toolsets
  → streamed response
  → session persistence
  → TUI rendering
```

## Session architecture

Sessions are stored on disk under `~/.pi-go/sessions/`.

Each session directory contains:

- `meta.json` — ID, app name, title, user, worktree, timestamps
- `events.jsonl` — append-only event log
- `branches.json` — active branch + branch metadata
- `branches/<name>/events.jsonl` — per-branch persisted timelines

Key behaviors:

- append-only event persistence
- recent-session lookup for `--continue`
- branch creation/switching
- transcript reload into the TUI
- optional history compaction

The UX layer builds Pi-style commands on top of this storage:

- `/new`
- `/resume`
- `/session`
- `/fork`
- `/tree`
- `/compact`

More: [docs/sessions.md](docs/sessions.md)

## Provider architecture

Provider selection is data-driven.

The registry is composed from:

1. built-in compatible families
2. discoverable `models/*.json` resources
3. config-local `providers` / `models` overrides

Built-in families:

- `anthropic`
- `openai`
- `gemini`
- `ollama`

The registry supports:

- provider aliases with custom env vars / base URLs / headers / match rules
- model aliases with friendly names
- family-aware API key / base URL lookup
- explicit explanation of compatibility limits

This is intentionally a **compatible-family seam**, not a general provider plugin system.

More: [docs/providers.md](docs/providers.md)

## Resource / extension architecture

The extension runtime discovers resources from loose directories and packages.

Resource types:

- `extensions/`
- `skills/`
- `prompts/`
- `themes/`
- `models/`

Loading precedence is global package → global loose → project package → project loose, with later entries overriding earlier ones by name.

That gives go-pi a composable customization story without growing the core bootstrap path.

More:

- [docs/extensions.md](docs/extensions.md)
- [docs/packages.md](docs/packages.md)
- [docs/customization.md](docs/customization.md)

## TUI architecture

The terminal UX is built around the Charm stack already used in-tree:

- **Bubble Tea** for update/render flow
- **Lip Gloss** for styling
- **Glamour** for markdown rendering
- small, focused submodels for input/chat/status/theme handling

The reboot direction is to keep the UX aligned with Bubble Tea/Bubbles primitives and avoid custom workflow widgets when a standard terminal component pattern is sufficient.

Important TUI responsibilities:

- session-aware slash commands
- transcript reload and navigation
- status bar and context display
- theme switching
- auth/login flows
- rendering agent/tool output cleanly

## Settings model

Primary user-facing settings live in:

- `~/.pi-go/config.json`
- `.pi-go/config.json`
- `~/.pi-go/.env`

The TUI `/settings` command points to these locations and helps users discover how providers, themes, aliases, and packages are loaded.

More: [docs/settings.md](docs/settings.md)

## Design constraints

The reboot tries to preserve these rules:

- keep the core minimal
- prefer discoverable resources over hardcoded workflow features
- keep session UX strong and recoverable
- make settings/customization obvious
- explain compatibility boundaries clearly
- favor the Charm ecosystem already in use over bespoke terminal mechanisms
