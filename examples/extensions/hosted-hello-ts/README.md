# hosted-hello-ts

Canonical TypeScript-language hosted extension fixture for pi-go.

## What it does

- Default-exports a `register(pi)` function that:
  - Subscribes to the `session_start` event (logs to the host via
    `console.log`, which `@pi-go/extension-host` forwards as a
    `pi.extension/log` notification).
  - Registers a `greet` tool that returns `"Hello, <name>!"`.
- Launched through `@pi-go/extension-host`, which does the handshake
  and provides the `ExtensionAPI` instance.

## Install

```bash
cd examples/extensions/hosted-hello-ts
npm install
```

This vendors the local `@pi-go/extension-sdk` via the `file:` dependency.

## Use from pi-go

1. Symlink (or copy) this directory into one of pi-go's discovery paths,
   e.g. `~/.pi-go/extensions/hosted-hello-ts`.
2. Approve it in `~/.pi-go/extensions/approvals.json`.
3. pi-go extracts its embedded `pi-go-extension-host` bundle on first
   use and launches the extension via
   `node <bundle> --entry src/index.ts --name hosted-hello-ts`.

See [docs/extensions.md](../../../docs/extensions.md) for the full pipeline.
