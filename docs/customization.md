# Customization philosophy

The go-pi reboot is built around a simple rule:

> keep the core small, and push product-specific behavior to discoverable resources or custom startup wiring.

## What belongs in core

Core should own the durable primitives:

- agent loop
- sandboxed tools
- session persistence
- terminal shell
- resource discovery
- compatible provider/model resolution

These are the pieces every downstream product or fork is likely to need.

## What should usually stay out of core

Avoid baking these directly into the default product unless they are universally useful:

- workflow engines
- opinionated memory systems
- project-specific policies
- custom review pipelines
- provider-specific one-offs that do not generalize
- UI mechanisms that bypass the Charm ecosystem already in use

## Preferred extension points

Reach for these first:

1. **extensions** — manifests, prompts, hooks, MCP toolsets, slash commands
2. **packages** — installable bundles of resources
3. **skills** — reusable instruction workflows
4. **themes** — visual customization
5. **models** — compatible provider/model aliases
6. **custom startup code** — for intentionally productized integrations

## Bubble Tea / Charm guidance

The TUI reboot should stay aligned with the Charm stack already in use.

Prefer:

- Bubble Tea update/render flow
- Bubbles-style composition where it fits
- Lip Gloss styling
- small focused view-models

Avoid inventing a custom UI mechanism when a standard Charm pattern is already a good fit.

## Provider guidance

The provider registry is a compatibility seam, not a universal plugin layer.

If a backend is compatible with an existing transport family, use `models/*.json` or config.
If it is not, integrate it intentionally.

## Session guidance

Sessions are part of the product surface, not just an implementation detail.

That means they should be:

- resumable
- branchable
- named clearly enough to scan
- reloadable in the TUI

Pi-style session UX is a core product differentiator for go-pi, so improving session discoverability is worth doing in core.
