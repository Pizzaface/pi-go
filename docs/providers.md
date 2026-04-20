# Providers and model aliases

## Built-in compatible families

go-pi ships with four built-in provider families:

- `anthropic`
- `openai`
- `gemini`
- `ollama`

These are the transport/auth families the core understands out of the box.

## What the registry layer is for

The provider registry lets you add **compatible aliases** without editing core startup code.

You can define:

- **provider aliases** — custom provider names that still behave like one of the built-in families
- **model aliases** — friendly names that map to a provider + target model

Sources, in load order:

1. built-in families
2. discoverable `models/*.json` resources
3. `providers` / `models` in config

Config-local definitions win last.

## Example provider alias

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
        "HTTP-Referer": "https://example.com/my-go-pi"
      },
      "match": [
        { "prefix": "openrouter/", "strip_prefix": true }
      ]
    }
  ]
}
```

## Example model alias

```json
{
  "models": [
    {
      "name": "router-sonnet",
      "provider": "openrouter",
      "target": "anthropic/claude-sonnet-4"
    }
  ]
}
```

That enables:

```bash
./pi --model router-sonnet
./pi --model openrouter/meta-llama/llama-4-maverick
```

## Compatibility limits

This layer is intentionally limited.

Use the registry when a backend is **wire-compatible** with one of the built-in families.

Good fits:

- custom OpenAI-compatible endpoints
- custom Anthropic-compatible endpoints
- alternate API keys/base URLs/headers
- friendly aliases for long model names

Not a good fit:

- a backend requiring a brand-new SDK
- a backend requiring a brand-new auth flow
- a backend with a fundamentally different protocol

When that happens, add a new core integration intentionally instead of stretching `models/*.json` beyond compatibility.

## Discoverability

The TUI exposes the loaded registry via:

- `/model`
- `/settings`

These surfaces show the active role/model plus loaded provider and model aliases so users can see what the runtime actually discovered.

## File locations

Provider/model resources can live under:

- `~/.go-pi/packages/*/models/`
- `~/.go-pi/models/`
- `.go-pi/packages/*/models/`
- `.go-pi/models/`

And directly in config via:

- `~/.go-pi/config.json`
- `.go-pi/config.json`
