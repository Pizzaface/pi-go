# Scrollable Slash Command List Design

**Date:** 2026-04-04  
**Status:** Approved direction; interaction details specified below

## Goal

Keep the slash command list shown for the exact-`/` command-list interaction compact in height while still allowing access to the full command set by scrolling.

## Problem

Today `showCommandList()` appends a normal assistant chat message containing every built-in, extension, and skill command. As the list grows, it expands the conversation vertically and pushes useful chat content out of view. The user wants the list to be shorter on screen, but still fully browsable.

## Recommended Approach

Add a dedicated scrollable slash-command picker overlay for the exact-`/` plus `Tab` entry point instead of rendering the full list as a regular chat message.

This keeps the visible UI height bounded, gives users explicit scroll behavior, and avoids mixing a transient picker into persistent conversation history.

## Alternatives Considered

### 1. Scrollable chat message block
- **Pros:** smallest conceptual change; keeps current message-based flow
- **Cons:** the current chat renderer is line-oriented, not component-oriented; adding an embedded scrollable region inside a rendered markdown message would be awkward and brittle

### 2. Dedicated overlay picker (**recommended**)
- **Pros:** bounded height, native keyboard scrolling, clearer transient behavior, easier to extend later with selection/highlighting
- **Cons:** requires new transient TUI state and rendering logic

### 3. Shortened static list with hint text
- **Pros:** easiest implementation
- **Cons:** does not actually satisfy the requirement to be scrollable

## UX Design

### Trigger behavior
- Typing `/` alone should not open the overlay.
- Pressing `Tab` while the input is exactly `/` should open the dedicated transient slash-command overlay described in this spec.
- Existing prefix completion for `/mo`, `/se`, etc. should remain unchanged unless the input is exactly `/`.
- When the overlay opens, the first selectable command row is selected automatically.

### Overlay behavior
- Render a bounded panel as a true overlay anchored near the input area.
- The overlay must not reduce the chat/message viewport height while open.
- Preferred placement is directly above the input.
- If there is not enough space above the input, clamp the panel height using this fallback order: target 3 visible selectable command rows plus overlay chrome, then 2 rows, then 1 row.
- If even a 1-row usable overlay with at least one visible selectable command does not fit during the initial explicit exact `/` + `Tab` open attempt, do not open the overlay. Instead, show a short non-persistent status hint telling the user the terminal is too small for the scrollable command list.
- If an already-open overlay later becomes unusable because of resize or inventory change, close it silently without appending a chat message.
- Show a fixed-height viewport with as many rows as fit inside the panel.
- Support scrolling through the full list with keyboard navigation.
- Only one transient overlay may be active at a time; opening the slash-command overlay closes any other active popup and gives focus to the slash-command overlay.
- Include built-in commands, then extension commands, then skill commands.
- If multiple sources expose the same command text, dedupe rows using this deterministic pipeline: collect all candidates, dedupe by dispatch precedence first, then group and order surviving rows. Precedence is built-in over extension over skill-derived. Within the same tier, first declaration wins and its description/metadata are retained in the surviving row.
- Group headers are rendered as non-selectable rows, count toward viewport height, and are skipped automatically by keyboard selection.
- Dismiss cleanly on `Esc`, on command insertion, or on input-state transitions that no longer represent the exact `/` overlay case.

### Input-state transitions
- If the user types additional characters after `/` (for example `/m`), close the overlay and do not open any other completion UI until the next explicit `Tab`.
- If the user backspaces from `/x` to exact `/`, reopening the exact-slash list is acceptable via the normal trigger path rather than keeping the prior overlay alive.
- Cursor movement, paste, or edits that make the input anything other than exact `/` should close the overlay.
- Pressing `Enter` while the overlay is open should insert the selected command rather than sending the message.
- Submitting exact `/` without opening the overlay should preserve existing behavior rather than changing command submission semantics in this iteration.
- On dismissal or insertion, focus returns to the main input.

### Content
- Each selectable row should show:
  - command name
  - short description when available
- Use a deterministic order within each section:
  - built-in commands follow the existing built-in slash command order
  - extension commands preserve manifest/config declaration order; if that source order is unavailable, sort lexicographically by command name
  - skills preserve loader/config order; if that source order is unavailable, sort selectable rows lexicographically by command text
- If the number of selectable commands exceeds the viewport, show a `selected/total` indicator in the overlay chrome, where both numbers count selectable command rows only and exclude headers.

## Architecture

### New responsibilities
- Introduce a dedicated slash-command overlay state in the TUI model, similar in spirit to the existing model picker and branch popup state.
- Convert the current “build a command list string” path into a structured list model that can be rendered in a viewport.

### Likely code areas
- `internal/tui/commands.go`
  - stop using `showCommandList()` for the normal `/` + `Tab` happy path
  - retain `showCommandList()` only as the initial-open fallback when the overlay cannot render at a usable size
  - keep `/help` as the full textual reference output
- `internal/tui/input.go`
  - continue to own command inventory and descriptions
  - expose data needed by the overlay in structured form
- `internal/tui/tui.go`
  - add overlay state, key handling, height calculations, and rendering
- `internal/tui/model_picker.go`
  - use only as a reference for scroll offset and bounded list behavior, not for layout; the slash-command picker must render as an overlaid layer that does not subtract from message viewport height

## Data Model

Introduce a lightweight command-entry shape for rendering:
- command text
- description
- section/group label (built-in, extension, skill)
- row kind (`header` or `command`)

Headers are always non-selectable. Selection moves only across `command` rows, while scrolling and viewport calculations include both header and command rows.

This data should be derived from the same sources already used for completion and help text so command definitions stay consistent.

## Interaction Details

### Keyboard
- `Up` / `Down`: move visible selection and adjust scroll offset, skipping non-selectable headers
- `Enter`: when the input is exactly `/` and the overlay is open, replace the full input with the selected command and place the cursor at the end of the inserted command without adding a trailing space
- The first `Tab` on exact `/` opens the overlay only.
- Exact `/` + `Shift+Tab` follows the same behavior as `Tab` and opens the overlay.
- While the overlay is open, `Tab` and `Shift+Tab` do not accept; they are ignored for insertion to avoid accidental double-Tab selection.
- `Esc`: close overlay without mutating input
- When the overlay is not open, `Tab` keeps its existing completion behavior

### Mouse
- Wheel scrolling is out of scope for V1.
- Click selection and click-outside dismissal are out of scope for V1.

## Error Handling

- Empty extension/skill command lists should not create blank sections.
- Very small terminal heights should degrade gracefully according to the placement fallback rules above.
- Width priority rules:
  - preserve command text first
  - truncate descriptions before truncating command names
  - when inner panel width is below 40 columns, omit descriptions entirely
  - ellipsize command names only as a last resort
- Long descriptions should truncate to the available width instead of wrapping the panel excessively.

## Testing Strategy

### Unit tests
- Verify command-entry construction includes all expected commands and descriptions.
- Verify grouped row construction emits non-selectable headers only for non-empty sections.
- Verify overlay scrolling clamps correctly at top and bottom.
- Verify keyboard selection skips headers.
- Verify only the viewport subset is rendered for a large command list.
- Verify `/help` still shows the full textual command reference.

### TUI behavior tests
- Typing `/` then opening the list should activate overlay state instead of appending a chat message.
- `Tab` on exact `/` should open the overlay.
- Opening the overlay should auto-select the first selectable command.
- `Enter` on a highlighted row should replace exact `/` with the selected command.
- `Tab` while the overlay is open should not insert a command.
- `Esc` should close the overlay and return focus to the main input.
- Typing after `/` should close the overlay and return to normal prefix completion.
- Backspace/cursor-edit transitions that leave the input not exactly `/` should close the overlay.
- Terminal resize while the overlay is open should recompute viewport height safely.
- Narrow terminals should hide descriptions before truncating command names.

## Non-Goals

- Reworking `/help`
- Changing the autocomplete algorithm for prefixed slash commands
- Adding fuzzy search or multi-column layout
- Refactoring the broader command system beyond what is needed for the overlay

## Inventory Change Handling

If the command inventory changes while the overlay is open (for example after skill reload or extension reload), rebuild the rows, clamp selection to the first available selectable command, and close the overlay if no commands remain.

If initial open leaves zero selectable commands or not enough room to display at least one selectable command row, use the same initial-open fallback behavior above.

If rebuild or terminal resize leaves zero selectable commands or not enough room to display at least one selectable command row, close the overlay silently without appending a chat message.

## Implementation Notes

The cleanest first version is a simple, scrollable, single-column overlay backed by the existing command inventory. If the UX feels good, it can later evolve into a richer command palette without redoing the data plumbing.