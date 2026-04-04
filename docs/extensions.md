# Extensions

pi-go's primary customization surface is the **extension runtime**.

If you want to add new behavior, prefer an extension over expanding the core. The core should stay small: agent loop, tools sandbox, sessions, TUI shell, and the runtime that loads extensions.

## Discovery

The runtime now discovers **shareable resources** across loose directories and installed packages.

### Resource directories

These resource types are discoverable:

- `extensions/` — manifest-driven runtime contributions
- `skills/` — reusable `SKILL.md` folders
- `prompts/` — markdown prompt templates exposed as slash commands
- `themes/` — JSON theme files layered on top of built-in themes
- `models/` — JSON provider/model registry documents for compatible transport families

### Loading order

Resources load in this order, with **later entries overriding earlier ones** by resource name:

1. `~/.pi-go/packages/*/<resource>/`
2. `~/.pi-go/<resource>/`
3. `.pi-go/packages/*/<resource>/`
4. `.pi-go/<resource>/`

Skills also keep the existing project-local compatibility directories after `.pi-go/skills/`:

- `.claude/skills/`
- `.cursor/skills/`

That means project resources override global ones, and loose project resources override packaged project resources.

### Extension manifest discovery

For extensions specifically, manifests are read from:

1. `~/.pi-go/packages/*/extensions/*/extension.json`
2. `~/.pi-go/extensions/*/extension.json`
3. `.pi-go/packages/*/extensions/*/extension.json`
4. `.pi-go/extensions/*/extension.json`

Project extensions override global extensions by `name`.

Each extension lives in its own directory:

```text
.pi-go/
└── extensions/
    └── demo/
        ├── extension.json
        ├── prompt.md
        └── skills/
            └── demo-skill/
                └── SKILL.md
```

## Manifest

Minimal example:

```json
{
  "name": "demo",
  "description": "Demo extension",
  "prompt": "You can use the demo extension when the user asks for demo behavior.",
  "hooks": [
    { "event": "before_tool", "command": "echo before read", "tools": ["read"] },
    { "event": "after_tool", "command": "echo after write", "tools": ["write"] }
  ],
  "lifecycle": [
    { "event": "startup", "command": "echo extension started >/tmp/demo-ext.log" },
    { "event": "session_start", "command": "echo session ready >>/tmp/demo-ext.log" }
  ],
  "mcp_servers": [
    { "name": "docs", "command": "node", "args": ["./server.js"] }
  ],
  "skills_dir": "skills",
  "tui": {
    "commands": [
      {
        "name": "demo",
        "description": "Run the demo workflow",
        "prompt": "Run the demo workflow with args: {{args}}"
      }
    ]
  }
}
```

## Packages

Packages are just installable directories that can contain any combination of:

```text
my-package/
├── extensions/
├── models/
├── prompts/
├── skills/
└── themes/
```

A package does not need its own runtime daemon or plugin API. It participates by dropping resources into the existing discovery model.

### Package lifecycle

Use the CLI to manage installed packages:

```bash
pi package install <path-or-git-url>
pi package install --project <path-or-git-url>
pi package list
pi package update <name>
pi package remove <name>
```

- default install scope is **global**: `~/.pi-go/packages/<name>/`
- `--project` installs into `.pi-go/packages/<name>/`
- local directory sources are copied in
- git sources are cloned
- `update` refreshes from the recorded source

This keeps customization lightweight and shareable without expanding core startup.

## Provider and model registries

Compatible provider/model customization comes in through discoverable `models/*.json` resources and matching `providers` / `models` arrays in config.

A registry document can declare:

- `providers[]` — provider name, compatible `family` (`anthropic`, `openai`, `gemini`, `ollama`), API key env vars, base URL env/default, optional default headers, and `match` rules
- `models[]` — exact aliases mapping a friendly name to a provider and target model

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

Loading order follows the same resource precedence as everything else: packaged global → loose global → packaged project → loose project, with later entries overriding earlier ones by provider/model name. Config-local `providers` / `models` are applied last.

This is intentionally a **compatible transport** seam, not a full provider-plugin framework. Use it when a backend can ride one of the existing families. If it needs a brand new protocol, SDK, or auth flow, add that intentionally instead of stretching the registry beyond compatibility.

## Prompt templates

Prompt templates live in `prompts/*.md` and are first-class runtime resources.

Example:

```markdown
---
name: triage
description: Summarize the current repo state and propose next steps
---
Triage the current workspace. Extra context: {{args}}
```

At runtime, prompt templates are loaded through the same discovery model and surfaced through the narrow TUI seam as slash commands. The body supports the same minimal `{{args}}` placeholder as extension-defined `tui.commands`.

Project prompt templates override global ones by `name`.

## Themes

Custom themes can be dropped into any discovered `themes/` directory as JSON files containing either:

- a single theme object, or
- a map of theme-name → theme object

Discovered themes overlay the built-in theme set. Project themes override global themes by theme name.

## Supported contributions

### 1. Prompt contributions

Use either:

- `prompt` — inline text added to the system instruction
- `prompt_file` — path relative to the extension directory

These fragments are appended to the base instruction at startup.

### 2. Tool registration

Use `mcp_servers` to register external toolsets. This is the main path for extension-owned tools without growing the core.

Each server is launched as an MCP subprocess and exposed through the agent runtime.

### 3. Tool hooks

Use `hooks` with these events:

- `before_tool`
- `after_tool`

Each hook can restrict itself to specific tool names via `tools`.

### 4. Lifecycle hooks

Use `lifecycle` for startup/session wiring:

- `startup`
- `session_start`

These hooks run through the same shell-command hook mechanism as tool hooks, but are fired by the extension runtime during bootstrap.

### 5. Skills

Use `skills_dir` to load extension-local `SKILL.md` folders. This is a good fit for reusable instruction workflows that do not need custom tools.

### 6. Narrow TUI extension points

The TUI intentionally exposes only a **small** surface.

Today, extensions can contribute:

- slash commands via `tui.commands`

A TUI extension command does **not** inject custom widgets. It maps a slash command to a prompt template and reuses the existing Bubble Tea chat flow.

Example:

```json
{
  "tui": {
    "commands": [
      {
        "name": "triage",
        "description": "Summarize the current workspace and propose next steps",
        "prompt": "Triage the current workspace. Additional context: {{args}}"
      }
    ]
  }
}
```

This keeps the UI aligned with Bubble Tea/Bubbles primitives and avoids a bespoke plugin-widget framework.

## Design guidance

When deciding whether something belongs in core or an extension:

### Put it in core when it is:

- required for every session
- part of the generic harness boundary
- needed to safely host extensions

### Put it in an extension when it is:

- opinionated workflow logic
- provider/integration-specific capability
- extra tooling beyond the minimal coding-agent shell
- recoverable feature work that used to be built in

## Current stable interfaces for future work

Phase 2 work should build on these seams instead of rewriting startup again:

- manifest discovery: `LoadManifests(...)`
- runtime assembly: `BuildRuntime(...)`
- lifecycle dispatch: `(*Runtime).RunLifecycleHooks(...)`
- shared discovery model: `DiscoverResourceDirs(...)`
- prompt-template discovery: `LoadPromptTemplates(...)`
- package lifecycle: `InstallPackage(...)`, `UpdatePackage(...)`, `RemovePackage(...)`, `ListInstalledPackages(...)`
- provider/model registry assembly: `DiscoverResourceDirs(...)` + `BuildProviderRegistry(...)`
- tool registration path: `mcp_servers`
- prompt contribution path: `prompt` / `prompt_file`
- extension resource dir path: `skills_dir`
- narrow TUI command path: `tui.commands` and `PromptTemplate.SlashCommand()`

Those are the intended extension-runtime interfaces for the next round of capability work.
