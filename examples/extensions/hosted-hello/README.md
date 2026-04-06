# hosted-hello

Minimal hosted extension example for go-pi.

This example demonstrates:

- stdio JSON-RPC handshake
- optional command registration request
- optional status UI intent request
- clean shutdown handling

## Files

- `extension.json`: hosted runtime manifest
- `main.go`: extension process implementation

## Install

Copy this folder into one of the extension discovery locations, for example:

- `~/.pi-go/extensions/hosted-hello/`

## Approvals

Hosted extensions require explicit approval in:

- `~/.pi-go/extensions/approvals.json`

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

## Run

The manifest starts the extension with:

```bash
go run .
```

from this directory.

## Notes

- The host always performs handshake and shutdown.
- Command/intent requests are best-effort and should be treated as optional until the host side subscribes to each
  method in your deployment.
