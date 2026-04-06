# Customization philosophy

go-pi keeps core behavior small and stable, then layers product-specific behavior through explicit extension points.

## Core owns

Core should keep durable primitives:

- agent loop
- sandboxed tools
- session persistence
- terminal shell
- resource discovery
- provider/model compatibility routing

## Push out of core

Prefer externalized customization for:

- workflow-specific policies
- org/project conventions
- custom review pipelines
- domain-specific UI behavior
- provider-specific one-offs

## Preferred extension points

Use these in order:

1. Extensions
2. Packages
3. Skills
4. Themes
5. Models
6. Custom startup code (only when needed)

## Extension model: declarative vs hosted

### Declarative extensions

Best for static contributions:

- prompt text
- hook commands
- lifecycle hooks
- MCP servers
- skill directories
- slash commands

They are low-friction and remain backward compatible when no `runtime` block is present.

### Compiled-in extensions

Best for first-party behaviors that should ship in-process with strict ownership and testing.

### Hosted extensions

Best for process-isolated integrations that need their own runtime.

Hosted extensions run over stdio JSON-RPC and are permission-gated through approvals.

## Trust and permission posture

Hosted extensions are not implicitly trusted.

- Approval source: `~/.pi-go/extensions/approvals.json`
- Trust class + capability grants are checked before registration/use
- Intercept-style behavior (`tools.intercept`) is denied by default for hosted third-party extensions

## TUI customization posture

TUI remains app-owned.

Extensions can request UI behavior through async intents, but do not take over layout/state directly.

Supported intent surfaces:

- status text
- widgets above/below editor
- notifications
- dialog modal

Renderer constraints:

- kinds: text/markdown only
- conflicts: deterministic single owner per surface
- failures/timeouts: fallback to built-in rendering

## Bubble Tea guidance

Keep extension UI integration aligned with Bubble Tea patterns:

- message-driven updates
- explicit state transitions
- app-owned rendering

Avoid direct mutable cross-thread UI writes from extension runtimes.

## Sessions remain a product surface

Session UX is intentionally core:

- resumable
- branchable
- inspectable in TUI
- compatible with extension/session state namespaces
