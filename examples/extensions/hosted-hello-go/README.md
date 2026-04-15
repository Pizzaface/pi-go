# hosted-hello-go

Canonical Go-language hosted extension fixture for pi-go.

## What it does

- Subscribes to the `session_start` event (logs to stderr via `piext.Log()`).
- Registers a `greet` tool that returns `"Hello, <name>!"`.
- Speaks JSON-RPC 2.0 over stdio per pi-go's hostproto v2.1.

## Build

```bash
cd examples/extensions/hosted-hello-go
go build .
```

## Standalone run

```bash
go run .
```

The process will hang waiting for an inbound `pi.extension/handshake`
JSON-RPC request on stdin (one message per line). Send a shutdown
notification (`{"jsonrpc":"2.0","method":"pi.extension/shutdown"}`) on
stdin to terminate cleanly.

## Use from pi-go

1. Symlink (or copy) this directory into one of pi-go's discovery paths,
   e.g. `~/.pi-go/extensions/hosted-hello-go`.
2. Approve it in `~/.pi-go/extensions/approvals.json` (see
   [docs/extensions.md](../../../docs/extensions.md) for the schema).
3. Start pi-go; the extension is launched on demand by the host runtime.
