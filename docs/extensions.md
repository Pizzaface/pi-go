# Extensions

Extensions customize pi-go at runtime. An extension is a Go package (or
TypeScript module, or a separately-compiled Go binary) that registers
tools, subscribes to lifecycle events, and interacts with the running
session through a stable API.

pi-go supports three execution modes, each with a different trust tier.

| Mode | Tier | How it runs | How it's trusted |
| --- | --- | --- | --- |
| **Compiled-in** | `compiled-in` | Linked into the `pi` binary at build time | Implicit — never prompts |
| **Hosted Go** | `third-party` (default) | Launched as a subprocess; speaks JSON-RPC 2.0 over stdio | Requires entry in `approvals.json` |
| **Hosted TS** | `third-party` (default) | Launched as `node <embedded-host> --entry <file>`; same wire protocol | Requires entry in `approvals.json` |

First-party packages (those signed by the pi-go project and shipped
alongside the binary) may be tagged with `trust_class: "first-party"`
in `approvals.json` to skip per-capability prompts — they still require
an approvals entry. The full reference lives in
[the design spec](superpowers/specs/2026-04-14-extensions-core-sdk-rpc-design.md).

## Quick Start — Hosted TypeScript

1. Scaffold your extension directory:

   ```
   my-ext/
     package.json
     tsconfig.json
     src/index.ts
   ```

2. Install the SDK:

   ```bash
   npm install @pi-go/extension-sdk
   ```

3. `package.json` declares the extension metadata in a top-level `pi` block:

   ```json
   {
     "name": "my-ext",
     "version": "0.1.0",
     "type": "module",
     "dependencies": { "@pi-go/extension-sdk": "^0.1.0" },
     "pi": {
       "entry": "src/index.ts",
       "description": "A friendly extension.",
       "requested_capabilities": [
         "tools.register",
         "events.session_start"
       ]
     }
   }
   ```

4. `src/index.ts` exports a default `register(pi)` function:

   ```ts
   import type { ExtensionAPI } from "@pi-go/extension-sdk";
   import { EventNames, Type } from "@pi-go/extension-sdk";

   export default async function register(pi: ExtensionAPI): Promise<void> {
     pi.on(EventNames.SessionStart, () => ({ control: null }));
     pi.registerTool({
       name: "greet",
       label: "Greet",
       description: "Returns a friendly greeting.",
       parameters: Type.Object({
         name: Type.String({ description: "Name to greet" }),
       }),
       execute: async (_id, { name }) => ({
         content: [{ type: "text", text: `Hello, ${name ?? "world"}!` }],
       }),
     });
   }
   ```

5. Symlink (or copy) the directory into a discovery path and approve it
   (see **Discovery paths** and **Trust & Approvals** below). pi-go
   launches the extension via an embedded `pi-go-extension-host` bundle
   running on Node.

The canonical fixture lives at `examples/extensions/hosted-hello-ts/`.

## Quick Start — Hosted Go

1. Scaffold the extension module:

   ```
   my-ext/
     go.mod
     main.go
     pi.toml
   ```

2. `go.mod` (adjust module path and replace directives to point at your
   local `pi-go` checkout, or rely on published `pkg/piapi` + `pkg/piext`
   once released):

   ```
   module example.com/my-ext

   go 1.22

   require (
       github.com/pizzaface/go-pi/pkg/piapi v0.0.0
       github.com/pizzaface/go-pi/pkg/piext v0.0.0
   )
   ```

3. `pi.toml` declares metadata and the launch command:

   ```toml
   name = "my-ext"
   version = "0.1.0"
   runtime = "hosted"
   command = ["go", "run", "."]
   requested_capabilities = [
       "tools.register",
       "events.session_start",
   ]
   ```

4. `main.go` calls `piext.Run`:

   ```go
   package main

   import (
       "context"

       "github.com/pizzaface/go-pi/pkg/piapi"
       "github.com/pizzaface/go-pi/pkg/piext"
   )

   var Metadata = piapi.Metadata{
       Name:                  "my-ext",
       Version:               "0.1.0",
       RequestedCapabilities: []string{"tools.register", "events.session_start"},
   }

   func register(pi piapi.API) error {
       return pi.RegisterTool(piapi.ToolDescriptor{
           Name:        "greet",
           Description: "Returns a friendly greeting.",
           Execute: func(context.Context, piapi.ToolCall, piapi.UpdateFunc) (piapi.ToolResult, error) {
               return piapi.ToolResult{
                   Content: []piapi.ContentPart{{Type: "text", Text: "hi"}},
               }, nil
           },
       })
   }

   func main() { _ = piext.Run(Metadata, register) }
   ```

5. Symlink (or copy) the directory into a discovery path and approve it.
   pi-go invokes your `command` from `pi.toml` as the subprocess.

The canonical fixture lives at `examples/extensions/hosted-hello-go/`.

## Discovery paths

Four directories are walked in order; later layers win on name collision:

1. `~/.pi-go/packages/<name>/`
2. `~/.pi-go/extensions/<name>/`
3. `<cwd>/.pi-go/packages/<name>/`
4. `<cwd>/.pi-go/extensions/<name>/`

Each layer may contain:

- A directory with `package.json` (TypeScript, `pi.entry` required) — mode `hosted-ts`.
- A directory with `pi.toml` (or `pi.json`) — mode `hosted-go`.
- A single `*.ts` file — mode `hosted-ts` with a derived metadata name.

Compiled-in extensions are not discovered at runtime; they register
themselves from `init()` at program startup by calling
`compiled.Append`.

## settings.json additions

pi-go reads `~/.pi-go/settings.json` (optional) to tune extension behavior:

```json
{
  "extensions": {
    "disabled": ["some-ext"],
    "node_path": "/usr/local/bin/node",
    "host_timeout_ms": 5000
  }
}
```

All fields are optional. Unknown fields are preserved across writes for
forward compatibility.

## Trust & Approvals

Hosted extensions sit in `StatePending` until they're approved. The recommended flow is to approve them from inside pi-go:

1. Start pi-go. Discovered but un-approved extensions appear as a status-bar toast:

   ```
   2 extensions pending approval — press e to review
   ```

2. Press `e` (or type `/extensions`) to open the management panel.

3. Select a row with the arrow keys. Press `a` on a pending row to open the approval dialog, toggle capabilities with `space`, and press `enter` to approve. pi-go writes to `~/.pi-go/extensions/approvals.json` on your behalf.

4. Approved extensions auto-start on the next pi-go launch. You can also `s` (start), `x` (stop), `r` (restart), or `v` (revoke) from the panel at any time.

### approvals.json schema

If you prefer to edit the file directly (or a dotfile-management flow needs it), the schema is:

```json
{
  "version": 2,
  "extensions": {
    "my-ext": {
      "trust_class": "third-party",
      "approved": true,
      "approved_at": "2026-04-15T12:00:00Z",
      "granted_capabilities": [
        "tools.register",
        "events.session_start"
      ],
      "denied_capabilities": []
    }
  }
}
```

Semantics:

- `approved: false` or a missing entry → the extension lands in
  `StatePending` and cannot start.
- `granted_capabilities` lists the `service.method` pairs the extension
  may call. During the handshake, the host returns these (and only these)
  as `granted_services`.
- `denied_capabilities` takes precedence over `granted_capabilities`.
- `trust_class: "compiled-in"` is never written here — compiled-in
  extensions bypass the gate entirely.
- `trust_class: "first-party"` exists as a tier distinction for future
  UX (batch approval, reduced prompting); the current build treats it
  identically to `third-party`.

Fields the TUI doesn't name are preserved on disk — future pi-go releases may add fields without disturbing your edits.

Changes to `approvals.json` are picked up on the next pi-go start or on
an explicit extension-reload (`R` in the panel).

## Session & UI (Spec #5)

Extensions granted `session.*` capabilities can read and write the
running session.

### Messaging

| Method | Capability | Effect |
|---|---|---|
| `pi.AppendEntry(kind, payload)` | `session.append_entry` | Append a typed entry to the transcript. `kind` must match `^[a-z][a-z0-9_-]*$`. |
| `pi.SendMessage(msg, opts)` | `session.append_entry` + `session.trigger_turn` (if `TriggerTurn`) | Append an extension-authored custom message. `opts.DeliverAs="steer"` is rejected. |
| `pi.SendUserMessage(msg, opts)` | `session.send_user_message` + `session.trigger_turn` (if `TriggerTurn` or `DeliverAs="steer"`) | Inject a user-role message. See delivery modes below. |
| `pi.SetSessionName` / `GetSessionName` | `session.manage` | Read/write the current session's title. |
| `pi.SetLabel(entryID, label)` | `session.manage` | Rename a branch (`entryID` is the branch ID). |

Delivery modes:

- `nextTurn` — append to transcript; wait for user to press enter unless `TriggerTurn=true`.
- `followUp` — queue; run automatically after the current turn ends.
- `steer` — abort the current turn, prepend the message as the next turn's input. Requires `TriggerTurn=true`.

### Session control (command handlers only)

These methods only run inside `CommandContext` (i.e. spec #2 command handlers). From event handlers they return `ErrSessionControlInEventHandler`. From the CLI they return `ErrSessionControlUnsupportedInCLI`.

- `WaitForIdle(ctx)` — block until no turn is running.
- `NewSession()` — start a fresh session.
- `Fork(branchID)` — fork a branch. Empty string = current session.
- `NavigateTree(branchID)` — switch to an existing branch.
- `SwitchSession(sessionID)` — resume a session by ID.
- `Reload(ctx)` — re-read approvals and provider registry without restarting extensions.

### Lifecycle hooks

Extensions declare hooks in `pi.toml`:

```toml
[[hooks]]
event   = "session_start"   # startup | session_start | before_turn | after_turn | shutdown
command = "ext_announce"    # must be a tool the extension registers
tools   = ["*"]             # hook fires only when these tools are active; "*" = always
timeout = 5000              # ms; default 5000, max 60000
```

Declaring any `[[hooks]]` entry requires the `hooks.register` capability. `critical = true` aborts startup on hook failure; only first-party extensions may set it.

### Streaming & logs

Partial `ToolResult` updates from `onUpdate(partial)` callbacks reach the TUI tool-display panel and the trace log. Extension log writes via `piext.Log()` — and direct `log.append` calls — stream to the TUI trace panel and `~/.pi-go/logs/extensions.log` (rotated at 10 MB, last 3 retained).

### Deprecations

Direct invocation of the legacy JSON-RPC method names `pi.extension/tool_update` and `pi.extension/log` remains supported for one release but is deprecated. Use the service-form `tool_stream.update` and `log.append` instead. They're removed in spec #6.

## Related

- [Full design reference](superpowers/specs/2026-04-14-extensions-core-sdk-rpc-design.md)
- [`pkg/piapi`](../pkg/piapi) — Go-language API surface shared by compiled-in and hosted-go extensions.
- [`pkg/piext`](../pkg/piext) — Go-language RPC runtime used by hosted-go extensions.
- [`packages/extension-sdk`](../packages/extension-sdk) — TypeScript API surface.
- [`packages/extension-host`](../packages/extension-host) — Node host for TypeScript extensions (embedded into `pi`).
