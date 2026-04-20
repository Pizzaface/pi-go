# hosted-hello-ts

Canonical TypeScript-language hosted extension fixture for go-pi.

## What it does

- Default-exports a `register(pi)` function that:
  - Subscribes to the `session_start` event (logs to the host via
    `console.log`, which `@go-pi/extension-host` forwards as a
    `pi.extension/log` notification).
  - Registers a `greet` tool that returns `"Hello, <name>!"`.
- Launched through `@go-pi/extension-host`, which does the handshake
  and provides the `ExtensionAPI` instance.

## Install

```bash
cd examples/extensions/hosted-hello-ts
npm install
```

This vendors the local `@go-pi/extension-sdk` via the `file:` dependency.

## Use from go-pi

1. Symlink (or copy) this directory into one of go-pi's discovery paths,
   e.g. `~/.go-pi/extensions/hosted-hello-ts`.
2. Approve it in `~/.go-pi/extensions/approvals.json`.
3. go-pi extracts its embedded `go-pi-extension-host` bundle on first
   use and launches the extension via
   `node <bundle> --entry src/index.ts --name hosted-hello-ts`.

See [docs/extensions.md](../../../docs/extensions.md) for the full pipeline.
