# Extensions

go-pi's primary customization surface is the **extension runtime**.

If you want to add new behavior, prefer an extension over expanding the core. The core should stay small: agent loop, tools sandbox, sessions, TUI shell, and the runtime that loads extensions.

## Discovery

The runtime discovers extension manifests from these directories, in this order:

1. `~/.pi-go/extensions/*/extension.json`
2. `.pi-go/extensions/*/extension.json`

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
- tool registration path: `mcp_servers`
- prompt contribution path: `prompt` / `prompt_file`
- narrow TUI command path: `tui.commands`

Those are the intended extension-runtime interfaces for the next round of capability work.
