# hosted-hello

Minimal hosted extension example for pi-go, built against the v2 extension
protocol (`docs/superpowers/specs/2026-04-11-extension-platform-v2-design.md`).

## What it demonstrates

- The v2 handshake with `requested_services`
- The `sdk.Client.Serve` lifecycle helper
- A `commands.register` host_call
- A `ui.status` host_call

## Files

- `extension.json`: hosted runtime manifest (declares the capabilities it needs)
- `main.go`: the extension process, using the SDK

## Install

Copy this folder into one of the extension discovery locations:

```
~/.pi-go/extensions/hosted-hello/
```

## Approvals

Hosted extensions require explicit approval in `~/.pi-go/extensions/approvals.json`:

```json
{
  "approvals": [
    {
      "extension_id": "hosted-hello",
      "trust_class": "hosted_third_party",
      "hosted_required": true,
      "granted_capabilities": [
        "ui.status",
        "commands.register"
      ]
    }
  ]
}
```

## Run

The manifest starts the extension with:

```bash
go run .
```

from this directory. pi-go invokes this automatically when the extension is enabled.

## Notes

- The host performs the handshake, returns a catalog of `host_services`, and then
  the extension issues two `host_call` RPCs to register its command and status line.
- Both registrations are best-effort: any error is logged and the extension continues.
- Shutdown is clean on SIGINT/SIGTERM or when the host closes stdin.
