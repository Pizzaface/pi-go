# Extensions

Extensions are the runtime customization surface for go-pi.

The runtime keeps `internal/extension.BuildRuntime()` as the assembly boundary and supports three extension runtime
classes:

- Declarative manifests (default)
- Compiled-in extensions (`runtime.type = "compiled_in"`)
- Hosted extensions over stdio JSON-RPC (`runtime.type = "hosted_stdio_jsonrpc"`)

## Resource discovery

Resource loading is layered and last-write-wins by name:

1. `~/.pi-go/packages/*/<resource>/`
2. `~/.pi-go/<resource>/`
3. `.pi-go/packages/*/<resource>/`
4. `.pi-go/<resource>/`

Resource types:

- `extensions/`
- `skills/`
- `prompts/`
- `themes/`
- `models/`

Project compatibility skills are also loaded from:

- `~/.agents/skills/`
- `.agents/skills/`
- `~/.claude/skills/`
- `.claude/skills/`
- `.cursor/skills/`

## Manifest basics

Minimal declarative extension:

```json
{
  "name": "demo",
  "description": "Demo extension",
  "prompt": "Use this extension when requested.",
  "skills_dir": "skills",
  "tui": {
    "commands": [
      {
        "name": "demo",
        "description": "Run demo flow",
        "prompt": "demo {{args}}"
      }
    ]
  }
}
```

Backward compatibility: if `runtime` is omitted, the extension remains declarative and all existing fields (`prompt`,
`hooks`, `lifecycle`, `mcp_servers`, `skills_dir`, `tui.commands`) keep working.

## Runtime block

`runtime` is optional:

```json
{
  "runtime": {
    "type": "hosted_stdio_jsonrpc",
    "command": "go",
    "args": ["run", "."],
    "env": {
      "LOG_LEVEL": "debug"
    }
  }
}
```

Runtime rules:

- Omitted `runtime` means declarative extension.
- `compiled_in` is resolved from the compiled registry (not executable paths).
- `hosted_stdio_jsonrpc` requires `runtime.command`.

## Trust and approvals

Approvals are app-owned and loaded from:

- `~/.pi-go/extensions/approvals.json`

Trust classes:

- `declarative`
- `compiled_in`
- `hosted_first_party`
- `hosted_third_party`

Hosted extensions require explicit approval (`hosted_required: true`) and explicit capability grants. An unapproved
hosted
extension enters `pending_approval` state at startup and never blocks the TUI. The user approves, denies, or manages
extensions interactively via `/extensions`.

Example approval entry:

```json
{
  "extension_id": "hosted-hello",
  "trust_class": "hosted_third_party",
  "hosted_required": true,
  "granted_capabilities": [
    "commands.register",
    "ui.status",
    "render.text"
  ]
}
```

### Extension states

| State              | Meaning                                         |
|--------------------|-------------------------------------------------|
| `pending_approval` | Manifest known, no approval in `approvals.json` |
| `ready`            | Approved, not yet launched                      |
| `running`          | Hosted process alive + handshake OK             |
| `stopped`          | Stopped by user                                 |
| `errored`          | Launch, handshake, or runtime failure           |
| `denied`           | User explicitly denied this session (in-memory) |

Declarative and compiled-in extensions go straight to `ready`. Hosted extensions start at `pending_approval` or `ready`
depending on whether an approval record exists.

### In-TUI lifecycle management

`/extensions` opens a panel showing all extensions grouped by state. From the panel:

| Key | Action                                      |
|-----|---------------------------------------------|
| `a` | Approve (pending only) — opens confirmation |
| `d` | Deny (pending only)                         |
| `r` | Restart (running/stopped/errored)           |
| `s` | Stop (running)                              |
| `x` | Revoke approval (running/stopped)           |
| `R` | Reload manifests from disk                  |

Scriptable forms: `/extensions approve <id>`, `/extensions deny <id>`, `/extensions stop <id>`,
`/extensions restart <id>`, `/extensions revoke <id>`, `/extensions reload`.

The status bar shows a compact extension summary (`ext: 1! 2✓ 1✗`) when any pending, running, or errored extensions
exist.

## Capabilities

Current capability keys:

- `commands.register`
- `tools.register`
- `tools.intercept`
- `ui.status`
- `ui.widget`
- `ui.dialog`
- `render.text`
- `render.markdown`

Policy notes:

- `tools.intercept` is denied for hosted third-party by default unless explicitly approved.
- Hosted capabilities are checked during registration/assembly, not deferred to TUI time.

## Hosted protocol (stdio JSON-RPC v2)

Protocol package: `internal/extension/hostproto`.

The v2 protocol replaces the v1 per-capability method set with two generic RPC envelopes:

- `pi.extension/handshake` — capability negotiation
- `pi.extension/host_call` — extension → host service calls
- `pi.extension/shutdown` — graceful teardown

All other operations (command registration, UI updates, tool registration, etc.) are dispatched through `host_call` to
a namespaced service registry. See the v2 design spec for the full service catalog.

### Handshake flow

The v2 handshake is **extension-initiated**: the extension sends a `HandshakeRequest` with its `requested_services`,
and the host responds with a `HandshakeResponse` containing grants and host service availability. The host also sends
its own handshake request first (which well-behaved SDKs ignore), allowing future host-initiated patterns.

The host detects which flow is in use by probing the first message from the extension: if it contains a `method` field,
it is an extension-initiated request and the host responds; if it contains a `result` field, it is a response to the
host's own request (legacy/in-process fakes).

```
1. Host sends HandshakeRequest (extension ignores or responds)
2. Extension sends HandshakeRequest with requested_services
3. Host validates protocol version, grants services, sends HandshakeResponse
4. Extension begins sending host_call requests
5. Host runs ServeInbound loop dispatching to the service registry
```

### Shutdown

`Process.Shutdown` closes stdin first (cross-platform EOF signal), sends `os.Interrupt` on Unix (skipped on Windows),
then waits with a bounded context. If the process does not exit within the timeout, it is killed via
`cmd.Process.Kill()`. All call sites use `HostedShutdownTimeout` (3s).

## Async TUI intents

Extensions can emit UI intents through the manager; TUI consumes them asynchronously via Bubble Tea messages.

Supported UI intents:

- Status line update
- Widget above editor
- Widget below editor
- Non-blocking notification
- Dialog modal

TUI applies these through app-owned rendering and ignores them when extension UI is disabled.

## Renderer constraints

Custom renderers are constrained:

- Payload kinds: `text`, `markdown` only
- Ownership: one extension per surface (conflicts are rejected)
- Timeout/error behavior: fallback to built-in rendering
- Cleanup: renderer ownership is removed on extension unload

Built-in layout ownership stays in the app; extensions provide content only.

## Example

Hosted example extension:

- `examples/extensions/hosted-hello/`

See its `README.md` for setup.

## Related docs

- [customization](customization.md)
- [packages](packages.md)
- [providers](providers.md)
