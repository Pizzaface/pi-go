# Scrollable Slash Command List Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the exact-`/` + `Tab` chat-message command dump with a compact, scrollable slash-command overlay that does not consume chat viewport height.

**Architecture:** Add a dedicated slash-command overlay state in the TUI model, backed by a structured command-row builder shared with existing command metadata. Render the overlay as a true layer above the input area, reuse model-picker-style selection/scroll math, and preserve `/help` as the full textual reference.

**Tech Stack:** Go, Bubble Tea v2, Lip Gloss v2, existing `internal/tui` view/update architecture, Go test

---

## File Structure

### Existing files to modify
- `internal/tui/tui.go`
  - Add slash-command overlay state to the root model.
  - Handle open/close/navigation keys.
  - Render the overlay as a layer that does not reduce chat viewport height.
- `internal/tui/input.go`
  - Add structured command inventory helpers for built-in, extension, and skill commands.
  - Keep slash descriptions and ordering logic centralized.
- `internal/tui/commands.go`
  - Keep `/help` unchanged.
  - Stop using `showCommandList()` for the normal exact-`/` + `Tab` path.
  - Retain a short-terminal fallback path if needed by the final implementation.
- `internal/tui/tui_test.go`
  - Add end-to-end-ish model tests for open/close/insert behavior.
- `internal/tui/completion_test.go`
  - Add tests covering exact-`/` behavior vs prefixed completion behavior.

### New files to create
- `internal/tui/slash_command_overlay.go`
  - Define overlay row/state structs, row builder, selection movement, and rendering helpers.
- `internal/tui/slash_command_overlay_test.go`
  - Unit tests for row building, dedupe, grouping, scroll clamping, and visible window calculations.

---

## Chunk 1: Overlay data model and row-building helpers

### Task 1: Add failing unit tests for row building and ordering

**Files:**
- Create: `internal/tui/slash_command_overlay_test.go`
- Reference: `internal/tui/input.go`

- [ ] **Step 1: Write failing tests for row construction**

Cover:
- built-in rows appear in existing built-in slash command order
- extension rows preserve declaration order
- skill rows preserve loader/config order
- same-name rows are deduped by precedence: built-in > extension > skill
- headers appear only for non-empty sections
- headers are non-selectable

- [ ] **Step 2: Run the new test file to verify failure**

Run:
```bash
go test ./internal/tui -run 'TestSlashCommandOverlayRows|TestSlashCommandOverlayDedupe|TestSlashCommandOverlayHeaders'
```
Expected: FAIL because overlay types/helpers do not exist yet.

### Task 2: Implement overlay row/state types

**Files:**
- Create: `internal/tui/slash_command_overlay.go`
- Modify: `internal/tui/input.go`

- [ ] **Step 3: Add command row types in `internal/tui/slash_command_overlay.go`**

Implement focused types/helpers:
```go
type slashCommandRowKind int

const (
    slashCommandRowHeader slashCommandRowKind = iota
    slashCommandRowCommand
)

type slashCommandRow struct {
    kind        slashCommandRowKind
    section     string
    command     string
    description string
}

type slashCommandOverlayState struct {
    rows      []slashCommandRow
    selected  int
    scrollOff int
    height    int
}
```

- [ ] **Step 4: Add a structured inventory builder in `internal/tui/input.go`**

Add a helper that gathers built-ins, extensions, and skills into deterministic sections and returns deduped command entries with descriptions. Keep description sourcing next to `slashCommandDesc()` so command metadata stays centralized.

- [ ] **Step 5: Add row-building helpers**

Implement:
- section-aware row generation
- header insertion only for non-empty sections
- same-tier ordering rules from the spec
- cross-tier dedupe before grouping/order

- [ ] **Step 6: Run unit tests again**

Run:
```bash
go test ./internal/tui -run 'TestSlashCommandOverlayRows|TestSlashCommandOverlayDedupe|TestSlashCommandOverlayHeaders'
```
Expected: PASS.

- [ ] **Step 7: Commit chunk 1**

```bash
git add internal/tui/input.go internal/tui/slash_command_overlay.go internal/tui/slash_command_overlay_test.go
git commit -m "feat: add slash command overlay row model"
```

---

## Chunk 2: Selection, scrolling, and viewport math

### Task 3: Add failing tests for selection and visible range behavior

**Files:**
- Modify: `internal/tui/slash_command_overlay_test.go`
- Reference: `internal/tui/model_picker.go`

- [ ] **Step 8: Write failing tests for interaction math**

Cover:
- initial selection clamps to first selectable row
- up/down skip headers
- scroll offset clamps at top/bottom
- visible window includes the selected row
- tiny-height states require at least one visible selectable row

- [ ] **Step 9: Run the targeted tests and verify failure**

Run:
```bash
go test ./internal/tui -run 'TestSlashCommandOverlaySelection|TestSlashCommandOverlayScroll|TestSlashCommandOverlayVisibleWindow'
```
Expected: FAIL until movement/clamping helpers exist.

### Task 4: Implement movement and viewport helpers

**Files:**
- Modify: `internal/tui/slash_command_overlay.go`
- Reference: `internal/tui/model_picker.go`

- [ ] **Step 10: Add selection helpers**

Implement focused methods such as:
```go
func (s *slashCommandOverlayState) clampToCommand(idx int) int
func (s *slashCommandOverlayState) move(delta int)
func (s *slashCommandOverlayState) ensureSelectionVisible()
func (s *slashCommandOverlayState) hasVisibleSelectableRow() bool
```

- [ ] **Step 11: Add viewport helpers**

Implement helpers that:
- compute visible row slice from `scrollOff` and `height`
- clamp `scrollOff` on resize/rebuild
- enforce the “at least one visible selectable row” invariant

- [ ] **Step 12: Re-run the interaction tests**

Run:
```bash
go test ./internal/tui -run 'TestSlashCommandOverlaySelection|TestSlashCommandOverlayScroll|TestSlashCommandOverlayVisibleWindow'
```
Expected: PASS.

- [ ] **Step 13: Commit chunk 2**

```bash
git add internal/tui/slash_command_overlay.go internal/tui/slash_command_overlay_test.go
git commit -m "feat: add slash command overlay navigation"
```

---

## Chunk 3: Root-model state and exact-`/` key handling

### Task 5: Add failing TUI tests for opening/closing/inserting

**Files:**
- Modify: `internal/tui/tui_test.go`
- Modify: `internal/tui/completion_test.go`

- [ ] **Step 14: Add failing TUI tests**

Cover:
- `Tab` on exact `/` opens overlay instead of appending a chat message
- `Shift+Tab` on exact `/` also opens overlay
- typing `/m` closes the overlay and does not auto-open another completion UI
- `Esc` closes the overlay and returns focus to input
- `Enter` inserts selected command into input without trailing space
- `Tab` while overlay is open does not insert
- prefixed completion for `/mo` still behaves as before

- [ ] **Step 15: Run targeted TUI tests and verify failure**

Run:
```bash
go test ./internal/tui -run 'TestSlashCommandOverlay|TestTabOnSlash|TestSlashCompletion'
```
Expected: FAIL until root-model behavior is wired in.

### Task 6: Add overlay state to the root model and update loop

**Files:**
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/input.go`

- [ ] **Step 16: Add slash overlay state to the root model**

Add a new field alongside existing transient UI state:
```go
slashOverlay *slashCommandOverlayState
```

- [ ] **Step 17: Intercept exact-`/` open behavior in input/update flow**

Implement the state transition so:
- exact `/` + `Tab` opens overlay
- exact `/` + `Shift+Tab` opens overlay
- opening the overlay closes other transient popups
- overlay auto-selects the first selectable command
- if the overlay cannot open at usable size, emit the chosen tiny-terminal fallback behavior

- [ ] **Step 18: Implement overlay-focused key handling**

While open:
- `Up` / `Down` move selection
- `Enter` inserts selected command into the input field
- `Esc` closes overlay
- `Tab` / `Shift+Tab` do not insert
- edits that make input not exactly `/` close overlay

- [ ] **Step 19: Re-run targeted TUI tests**

Run:
```bash
go test ./internal/tui -run 'TestSlashCommandOverlay|TestTabOnSlash|TestSlashCompletion'
```
Expected: PASS.

- [ ] **Step 20: Commit chunk 3**

```bash
git add internal/tui/tui.go internal/tui/input.go internal/tui/tui_test.go internal/tui/completion_test.go
git commit -m "feat: wire slash command overlay behavior"
```

---

## Chunk 4: Overlay rendering as a true layer

### Task 7: Add failing render tests for layout behavior

**Files:**
- Modify: `internal/tui/tui_test.go`
- Modify: `internal/tui/slash_command_overlay_test.go`

- [ ] **Step 21: Add failing tests for rendering/layout**

Cover:
- overlay render includes only visible rows
- headers render distinctly from commands
- narrow widths hide descriptions before truncating command names
- overlay rendering does not reduce message viewport height compared with the same screen state without overlay

- [ ] **Step 22: Run render-focused tests and verify failure**

Run:
```bash
go test ./internal/tui -run 'TestRenderSlashCommandOverlay|TestSlashCommandOverlayLayout|TestSlashCommandOverlayNarrowWidth'
```
Expected: FAIL until rendering exists.

### Task 8: Implement overlay rendering helpers

**Files:**
- Modify: `internal/tui/slash_command_overlay.go`
- Modify: `internal/tui/tui.go`

- [ ] **Step 23: Implement overlay row rendering**

Add a renderer that:
- renders a border/title/chrome with `selected/total`
- shows only the current viewport rows
- visually distinguishes headers and selected command row
- truncates descriptions before command text
- hides descriptions below the 40-column inner-width threshold

- [ ] **Step 24: Layer the overlay into `View()` without shrinking chat**

Update `View()` so the overlay is rendered after the main left panel is built, using overlaid composition rather than subtracting overlay height from `availableHeight`.

- [ ] **Step 25: Handle resize/rebuild invalidation**

When terminal size changes or inventory rebuilds:
- recompute overlay height
- clamp selection and scroll offset
- close overlay silently if it can no longer show a selectable row

- [ ] **Step 26: Re-run render/layout tests**

Run:
```bash
go test ./internal/tui -run 'TestRenderSlashCommandOverlay|TestSlashCommandOverlayLayout|TestSlashCommandOverlayNarrowWidth'
```
Expected: PASS.

- [ ] **Step 27: Commit chunk 4**

```bash
git add internal/tui/tui.go internal/tui/slash_command_overlay.go internal/tui/tui_test.go internal/tui/slash_command_overlay_test.go
git commit -m "feat: render scrollable slash command overlay"
```

---

## Chunk 5: Command-path cleanup and full verification

### Task 9: Align command-path behavior and fallback handling

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/input.go`
- Modify: `internal/tui/tui_test.go`

- [ ] **Step 28: Clean up the old exact-`/` list path**

Ensure:
- `/help` still prints the full textual command reference
- normal exact-`/` + `Tab` uses overlay, not chat history
- tiny-terminal fallback follows the design decision implemented in chunk 3
- no dead helper paths are left behind accidentally

- [ ] **Step 29: Add or update regression tests for `/help` and fallback behavior**

Cover:
- `/help` output still includes the full command list
- exact-`/` overlay path no longer appends the long command list in the normal case
- tiny-terminal fallback behavior is stable and non-spammy

- [ ] **Step 30: Run focused TUI package tests**

Run:
```bash
go test ./internal/tui
```
Expected: PASS.

### Task 10: Run broader verification

**Files:**
- No new code

- [ ] **Step 31: Run repo-wide tests**

Run:
```bash
go test ./...
```
Expected: PASS.

- [ ] **Step 32: Do a final diff scan**

Run:
```bash
git diff --stat
git diff -- internal/tui/tui.go internal/tui/input.go internal/tui/commands.go internal/tui/slash_command_overlay.go internal/tui/tui_test.go internal/tui/completion_test.go internal/tui/slash_command_overlay_test.go
```
Expected: only slash-command overlay related changes.

- [ ] **Step 33: Commit final cleanup**

```bash
git add internal/tui/commands.go internal/tui/input.go internal/tui/tui.go internal/tui/tui_test.go internal/tui/completion_test.go internal/tui/slash_command_overlay.go internal/tui/slash_command_overlay_test.go docs/superpowers/specs/2026-04-04-slash-command-list-scroll-design.md docs/superpowers/plans/2026-04-04-slash-command-list-scrollable-overlay.md
git commit -m "feat: add scrollable slash command overlay"
```

---

## Notes for the implementer

- Follow existing `internal/tui` patterns, but do **not** copy model-picker layout behavior that subtracts popup height from the chat viewport.
- Keep the overlay focused on exact `/` only. Do not expand scope into a general fuzzy command palette.
- Reuse existing command metadata where possible so `/help`, completion, and overlay stay aligned.
- Be careful with same-name command precedence so what the overlay shows matches what runtime dispatch will actually execute.
- Keep tests environment-independent; no terminal-size assumptions beyond what the test explicitly sets.
