# Sessions

## Session model

go-pi persists conversations to disk so the terminal UX can support Pi-style session flows instead of a single throwaway transcript.

Each session stores:

- metadata (`meta.json`)
- append-only events (`events.jsonl`)
- branch state (`branches.json`)
- per-branch event logs (`branches/<name>/events.jsonl`)

Sessions live under `~/.pi-go/sessions/`.

## TUI commands

### `/new`
Create a fresh session and clear the current visible transcript.

### `/resume [id]`
- with no argument: list recent sessions
- with an ID or unique prefix: switch to that session and reload its transcript

### `/session`
Show the current session’s title, ID, branch, worktree, and update time.

### `/fork <name>`
Create and switch to a new branch of the current session.

### `/tree`
Render the current session’s branch tree.

### `/compact`
Compact older session history when the transcript has grown large.

## CLI resume flow

Outside the TUI:

```bash
./pi --continue
./pi --session <id>
```

`--continue` resolves the most recently updated session for the current app/user.

## Naming

Sessions now carry a lightweight title in metadata.

The title starts as a simple timestamp fallback and is upgraded automatically from the first user message when available. That makes `/resume` and `/session` much easier to scan.

## Reloadability

A key UX goal of the reboot is that sessions should be easy to reload instead of feeling disposable.

That means:

- recent sessions are listable
- transcripts can be reloaded back into the TUI
- branch switching reloads the active conversation view
- session metadata is rich enough to identify the right thread quickly

## Branching model

Branching is conversation branching, not git branching.

It is designed for:

- exploring alternate solutions
- splitting design vs implementation lines of thought
- preserving a main thread while trying a risky path elsewhere

If you need a fresh conversation, use `/new`.
If you need a divergent continuation of the same conversation, use `/fork`.
