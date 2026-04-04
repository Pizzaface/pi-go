# go-pi Settings

## Where settings live

go-pi keeps settings file-based and intentionally lightweight.

- **Global config:** `~/.pi-go/config.json`
- **Project config:** `.pi-go/config.json`
- **API keys / login output:** `~/.pi-go/.env`

Project config overrides global config.

> Compatibility note: go-pi still uses `.pi-go` directories on disk for compatibility with earlier phases of the reboot.

## What belongs in config.json

Common fields:

- `roles` — named model presets such as `default`, `smol`, `plan`, `slow`
- `theme` — default TUI theme name
- `thinkingLevel` — provider-specific reasoning level where supported
- `extraHeaders` — extra HTTP headers for compatible providers
- `insecureSkipTLS` — opt-in TLS relaxation for local/dev endpoints
- `hooks` — shell hooks around tool execution
- `providers` / `models` — compatible provider/model alias definitions
- `compactor` — session output compaction tuning

## Example

```json
{
  "theme": "tokyo-night",
  "roles": {
    "default": { "model": "claude-sonnet-4-6" },
    "smol": { "model": "gemini-2.5-flash" },
    "plan": { "model": "gpt-5.4" }
  },
  "models": [
    {
      "name": "router-sonnet",
      "provider": "openrouter",
      "target": "anthropic/claude-sonnet-4"
    }
  ]
}
```

## Discoverability in the TUI

Inside the TUI:

- `/settings` — show config paths, current theme, role, provider/model, and loaded aliases
- `/theme` — list or switch themes
- `/model` — inspect the active model/role and loaded aliases
- `/login` — configure API keys

## Theme resources

Themes can come from:

- built-in themes
- `~/.pi-go/themes/*.json`
- `.pi-go/themes/*.json`
- installed packages that contain `themes/`

The TUI persists a chosen theme back to `~/.pi-go/config.json`.

## Hooks and optional settings

The default product keeps policy/workflow features out of the main core path. That means some config fields may only matter when custom startup code or extensions wire them in.

Examples:

- extra tool hooks
- optional MCP integrations
- optional helper subsystems

If a setting feels product-specific rather than core, prefer an extension/package/custom startup path over adding more built-in runtime assumptions.

## Recommended mental model

Think of go-pi settings in layers:

1. **base runtime settings** — role, theme, provider choice
2. **compatible alias settings** — providers/models
3. **resource loading** — extensions, prompts, skills, packages, themes, models
4. **optional behavior** — hooks and downstream integrations
