# Extensions

Extensions are the primary runtime customization surface for go-pi.

If you want to add behavior without growing the default core, prefer an extension.

## Resource discovery

The runtime discovers resources across loose directories and installed packages.

### Resource types

- `extensions/` — manifest-driven runtime contributions
- `skills/` — reusable `SKILL.md` folders
- `prompts/` — markdown prompt templates exposed as slash commands
- `themes/` — JSON themes layered on top of built-ins
- `models/` — compatible provider/model registry documents

### Loading order

Later entries override earlier ones by resource name:

1. `~/.pi-go/packages/*/<resource>/`
2. `~/.pi-go/<resource>/`
3. `.pi-go/packages/*/<resource>/`
4. `.pi-go/<resource>/`

Project-local compatibility skill directories still load after `.pi-go/skills/`:

- `.claude/skills/`
- `.cursor/skills/`

## Extension manifest discovery

Extension manifests are loaded from:

1. `~/.pi-go/packages/*/extensions/*/extension.json`
2. `~/.pi-go/extensions/*/extension.json`
3. `.pi-go/packages/*/extensions/*/extension.json`
4. `.pi-go/extensions/*/extension.json`

## Minimal example

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
        "name": "triage",
        "description": "Summarize the current workspace",
        "prompt": "Triage the current workspace. Extra context: {{args}}"
      }
    ]
  }
}
```

## Supported contributions

### Prompt contributions

Use `prompt` or `prompt_file` to append instruction text.

### Tool registration

Use `mcp_servers` to register external toolsets.

### Tool hooks

Use `hooks` with:

- `before_tool`
- `after_tool`

### Lifecycle hooks

Use `lifecycle` with:

- `startup`
- `session_start`

### Skills

Use `skills_dir` for extension-local `SKILL.md` folders.

### Narrow TUI contributions

The TUI seam is intentionally small.

Today, extensions can contribute:

- slash commands via `tui.commands`

Those commands map onto the existing Bubble Tea chat flow rather than injecting arbitrary UI widgets.

## Relationship to packages

Extensions can live loose in `.pi-go/extensions/` or `~/.pi-go/extensions/`, but packages are often the better distribution format when you want to ship a reusable bundle.

More: [packages](packages.md)

## Relationship to providers

Extensions are not the main provider customization mechanism.

For compatible provider/model aliases, prefer `models/*.json` resources and config-local `providers` / `models`.

More: [providers](providers.md)
