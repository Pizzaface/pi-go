# Rich TUI Extension Platform Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn go-pi's current manifest-driven extension seam into a richer extension platform with compiled-in first-party extensions, hosted third-party extensions over stdio JSON-RPC, permissioned capabilities, async TUI integration, and constrained custom rendering.

**Architecture:** Keep `internal/extension.BuildRuntime()` as the top-level assembly boundary. Introduce an extension manager plus capability gateway inside `internal/extension/`, with hosted processes managed under `internal/extension/hostruntime/` and the transport schema in `internal/extension/hostproto/`. TUI integration stays app-owned: extensions emit events/intents, and `internal/tui/` applies them asynchronously via Bubble Tea messages instead of letting extensions mutate UI state directly.

**Tech Stack:** Go, Google ADK Go, Bubble Tea v2, JSON-RPC 2.0 over stdio, JSON-backed manifest loading, JSON-backed
extension approvals, JSON session state under `~/.go-pi/sessions/`

---

## Scope and sequencing notes

- Execute this work in a **dedicated worktree**.
- Do **not** attempt all stages in one PR. Land one chunk at a time.
- Preserve backward compatibility for existing declarative extensions (`prompt`, `hooks`, `lifecycle`, `mcp_servers`, `skills_dir`, `tui.commands`).
- Treat `tools.intercept` as unavailable to `hosted_third_party` in the first pass unless the permission gate is already in place.
- Prefer additive files over growing `internal/extension/runtime.go` into a god file.

## File structure map

### Existing files to modify
- `internal/extension/runtime.go` — keep `BuildRuntime()` as the startup boundary, but have it construct the richer manager-backed runtime
- `internal/extension/resources.go` — preserve resource discovery, wire any manifest/runtime metadata parsing helpers that belong near discovery
- `internal/cli/interactive.go` — pass richer runtime data into deferred init and TUI config
- `internal/tui/types.go` — expand TUI config/init result for dynamic extension surfaces and extension bridge wiring
- `internal/tui/tui.go` — initialize async extension bridge state and subscribe to runtime-driven UI messages
- `internal/tui/commands.go` — route dynamic commands through the extension manager instead of static slash-command-only contributions
- `internal/tui/input.go` — include dynamic command names in completion/cycling/help discovery
- `docs/extensions.md` — document runtime manifest fields, trust model, permissions, and hosted protocol expectations
- `docs/customization.md` — update customization story so users understand the difference between declarative and hosted extensions

### New files to create
- `internal/extension/manifest.go` — manifest structs including new `runtime` block
- `internal/extension/manager.go` — extension manager, runtime registration, conflict checks, lifecycle supervision
- `internal/extension/events.go` — typed extension event definitions and subscription helpers
- `internal/extension/intents.go` — capability/intention types flowing from extensions to app/TUI
- `internal/extension/permissions.go` — trust classes, capability checks, approval storage loading/saving
- `internal/extension/state_store.go` — app-owned extension state persistence under session namespaces
- `internal/extension/registry.go` — compiled-in extension registration interface and registry
- `internal/extension/hostproto/protocol.go` — JSON-RPC method names, request/response payload types, version constants
- `internal/extension/hostruntime/process.go` — hosted process launcher over stdio
- `internal/extension/hostruntime/client.go` — handshake, request/response, event delivery, timeouts
- `internal/tui/extension_bridge.go` — async Bubble Tea bridge from extension intents to UI state changes
- `internal/tui/extension_msgs.go` — Bubble Tea messages/commands for extension-driven UI updates

### Tests to add or extend
- `internal/extension/manifest_test.go`
- `internal/extension/manager_test.go`
- `internal/extension/permissions_test.go`
- `internal/extension/state_store_test.go`
- `internal/extension/hostproto/protocol_test.go`
- `internal/extension/hostruntime/client_test.go`
- `internal/extension/hostruntime/process_test.go`
- `internal/tui/extension_bridge_test.go`
- `internal/tui/extension_commands_test.go` (extend)
- `internal/tui/tui_update_test.go` (extend)
- `internal/extension/runtime_test.go` (extend)

---

## Chunk 1: Foundation and runtime assembly

### Task 1: Add manifest runtime metadata and keep old manifests working

**Files:**
- Create: `internal/extension/manifest.go`
- Modify: `internal/extension/runtime.go`
- Test: `internal/extension/manifest_test.go`
- Test: `internal/extension/runtime_test.go`

- [ ] **Step 1: Write the failing manifest parsing tests**

```go
func TestLoadManifests_ParsesHostedRuntimeBlock(t *testing.T) {
    // write extension.json with runtime.type, command, args, env
    // assert manifest.Runtime.Type == "hosted_stdio_jsonrpc"
}

func TestLoadManifests_BackwardCompatibleWithoutRuntimeBlock(t *testing.T) {
    // write old-style extension.json without runtime
    // assert manifest loads successfully and runtime stays zero-value/declarative
}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension -run 'TestLoadManifests_(ParsesHostedRuntimeBlock|BackwardCompatibleWithoutRuntimeBlock)'`
Expected: FAIL because the manifest schema/runtime block is not implemented yet.

- [ ] **Step 3: Extract manifest types, then add runtime metadata**

Move these existing types and helpers out of `internal/extension/runtime.go` into `internal/extension/manifest.go` before extending them:
- `Manifest`
- `SlashCommand`
- `TUIConfig`
- `Manifest.enabled()`
- `Manifest.resolvePrompt()`
- `Manifest.resolveSkillsDir()`

Then add a runtime block like:

```go
type RuntimeSpec struct {
    Type    string            `json:"type,omitempty"`
    Command string            `json:"command,omitempty"`
    Args    []string          `json:"args,omitempty"`
    Env     map[string]string `json:"env,omitempty"`
}
```

Keep these rules explicit in code/comments:
- missing `runtime` block means declarative-only extension
- `compiled_in` is resolved by registry, not by loading a binary path
- `hosted_stdio_jsonrpc` requires `command`

- [ ] **Step 4: Extend runtime tests for backward compatibility**

Add a test that builds the runtime with an old declarative extension and asserts prompt/hooks/skills/`tui.commands` still work unchanged.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/manifest.go internal/extension/runtime.go internal/extension/manifest_test.go internal/extension/runtime_test.go
rtk git commit -m "feat: add extension runtime manifest metadata"
```

### Task 2: Introduce the extension registry and manager skeleton

**Files:**
- Create: `internal/extension/registry.go`
- Create: `internal/extension/manager.go`
- Create: `internal/extension/events.go`
- Modify: `internal/extension/runtime.go`
- Test: `internal/extension/manager_test.go`
- Test: `internal/extension/runtime_test.go`

- [ ] **Step 1: Write the failing manager assembly tests**

```go
func TestBuildRuntime_InitializesExtensionManager(t *testing.T) {
    rt := buildTestRuntime(t)
    if rt.Manager == nil {
        t.Fatal("expected manager")
    }
}

func TestManager_AcceptsEventSubscription(t *testing.T) {
    // register ext subscription for session_start
    // assert the manager stores the subscription without fan-out yet
}

func TestManager_RejectsDuplicateDynamicCommandNames(t *testing.T) {
    // register cmd from ext A, then ext B with same name
    // expect deterministic error, not last-write-wins
}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension -run 'Test(BuildRuntime_InitializesExtensionManager|Manager_AcceptsEventSubscription|Manager_RejectsDuplicateDynamicCommandNames)'`
Expected: FAIL because there is no manager/registry/subscription slot/conflict policy yet.

- [ ] **Step 3: Implement the smallest useful manager contract**

Define clear seams before adding behavior:

```go
type CompiledExtension interface {
    ID() string
    Register(*Registrar) error
}

type Manager struct {
    // manifests, compiled registry, commands, tools, subscriptions, health
}
```

Support only:
- compiled-in registration
- event subscription registration/storage
- dynamic command/tool ownership tracking
- duplicate rejection
- no hosted process launch yet

- [ ] **Step 4: Wire `BuildRuntime()` to construct the manager without changing public startup shape**

Keep `BuildRuntime()` as the top-level constructor. It should build core tools/resources first, then attach a manager and richer runtime fields.

Explicitly add `Manager *Manager` to the `Runtime` struct so tests fail/pass for the right reason.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/registry.go internal/extension/manager.go internal/extension/events.go internal/extension/runtime.go internal/extension/manager_test.go internal/extension/runtime_test.go
rtk git commit -m "feat: add extension manager foundation"
```

### Task 3: Add approvals and trust checks before hosted behavior

**Files:**
- Create: `internal/extension/permissions.go`
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/runtime.go`
- Test: `internal/extension/permissions_test.go`
- Test: `internal/extension/manager_test.go`

- [ ] **Step 1: Write the failing permission tests**

```go
func TestPermissions_HostedThirdPartyCannotUseInterceptByDefault(t *testing.T) {}
func TestPermissions_LoadsApprovalsFile(t *testing.T) {}
func TestManager_RejectsUnapprovedHostedCapability(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension -run 'Test(Permissions_|Manager_RejectsUnapprovedHostedCapability)'`
Expected: FAIL because no approvals/trust checks exist.

- [ ] **Step 3: Implement approval storage and trust resolution**

Use an app-owned approvals file, e.g. `~/.go-pi/extensions/approvals.json`, with fields for:
- extension id
- trust class
- granted capabilities
- hosted-required flag
- approved-at timestamp

- [ ] **Step 4: Gate registration on capability approval**

Make the manager reject disallowed capabilities early during runtime assembly/registration, not later during TUI use.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/permissions.go internal/extension/manager.go internal/extension/runtime.go internal/extension/permissions_test.go internal/extension/manager_test.go
rtk git commit -m "feat: add extension approvals and trust checks"
```

---

## Chunk 2: Hosted runtime and protocol

### Task 4: Define the hosted protocol before launching anything

**Files:**
- Create: `internal/extension/hostproto/protocol.go`
- Test: `internal/extension/hostproto/protocol_test.go`

- [ ] **Step 1: Write the failing protocol tests**

```go
func TestHandshake_IncludesModeAndCapabilityMask(t *testing.T) {}
func TestProtocol_RejectsIncompatibleMajorVersion(t *testing.T) {}
func TestEventPayload_RoundTrip(t *testing.T) {}
func TestHealthNotification_Serialization(t *testing.T) {}
func TestRenderPayload_OnlyAllowsSupportedKinds(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension/hostproto -run 'Test(Handshake_|Protocol_|EventPayload_|HealthNotification_|RenderPayload_)'`
Expected: FAIL because protocol types/constants do not exist.

- [ ] **Step 3: Implement protocol constants and payload types**

Create the `internal/extension/hostproto/` package and define version constants plus request/response payloads for:
- handshake
- event delivery
- capability intent envelopes
- command/tool registration
- render responses (`text`, `markdown` only in v1)
- health/error notifications
- shutdown/reload control

- [ ] **Step 4: Keep the render contract intentionally narrow**

Reject raw ANSI/layout ownership. Make the app own final styling and fallback rendering.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/hostproto/protocol.go internal/extension/hostproto/protocol_test.go
rtk git commit -m "feat: define hosted extension protocol"
```

### Task 5: Launch hosted extensions over stdio JSON-RPC

**Files:**
- Create: `internal/extension/hostruntime/process.go`
- Create: `internal/extension/hostruntime/client.go`
- Modify: `internal/extension/manager.go`
- Test: `internal/extension/hostruntime/client_test.go`
- Test: `internal/extension/hostruntime/process_test.go`
- Test: `internal/extension/manager_test.go`

- [ ] **Step 1: Write the failing hosted runtime tests**

```go
func TestHostedClient_PerformsHandshake(t *testing.T) {}
func TestHostedClient_TimeoutsSlowHandshake(t *testing.T) {}
func TestHostedClient_MarksUnhealthyOnCrash(t *testing.T) {}
func TestHostedProcess_CleanShutdown(t *testing.T) {}
func TestManager_RefusesToLaunchUnapprovedHostedExtension(t *testing.T) {}
func TestManager_LaunchesHostedExtensionFromManifestRuntime(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension/... -run 'Test(HostedClient_|HostedProcess_CleanShutdown|Manager_(RefusesToLaunchUnapprovedHostedExtension|LaunchesHostedExtensionFromManifestRuntime))'`
Expected: FAIL because there is no launcher/client or pre-launch approval gate.

- [ ] **Step 3: Implement process launcher and client**

Use `exec.CommandContext` with stdio pipes. Keep responsibilities split:
- `process.go`: spawn/stop/wait, env merge, stderr capture
- `client.go`: JSON-RPC encode/decode, request IDs, handshake, deadlines

- [ ] **Step 4: Integrate hosted startup into the manager**

Resolution rules:
- declarative-only extensions: no child process
- compiled-in extensions: registry path only
- hosted extensions: check approval/capability policy first, then launch from manifest `runtime.command`/`runtime.args`, then handshake before registration

Do not execute unapproved hosted extensions at all.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/hostruntime/process.go internal/extension/hostruntime/client.go internal/extension/manager.go internal/extension/hostruntime/client_test.go internal/extension/hostruntime/process_test.go internal/extension/manager_test.go
rtk git commit -m "feat: add hosted extension launcher and client"
```

### Task 6: Add extension state persistence before richer UI work

**Files:**
- Create: `internal/extension/state_store.go`
- Modify: `internal/extension/manager.go`
- Modify: `internal/cli/interactive.go`
- Test: `internal/extension/state_store_test.go`
- Test: `internal/extension/manager_test.go`
- Test: `internal/cli/interactive_test.go`

- [ ] **Step 1: Write the failing state-store tests**

```go
func TestStateStore_PersistsByExtensionAndSession(t *testing.T) {}
func TestStateStore_SerializesJSONValues(t *testing.T) {}
func TestManager_ProvidesStateNamespaceToExtensions(t *testing.T) {}
func TestInteractive_BindsSessionToManager(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension -run 'Test(StateStore_|Manager_ProvidesStateNamespaceToExtensions)' && rtk go test ./internal/cli -run 'TestInteractive_BindsSessionToManager'`
Expected: FAIL because no extension state store exists and `interactive.go` does not bind the session yet.

- [ ] **Step 3: Implement app-owned storage**

Store under session directories, e.g.:

- `~/.go-pi/sessions/<session-id>/state/extensions/<extension-id>.json`

Expose only a small namespaced API (`get`, `set`, `delete`) to manager/extension code.

- [ ] **Step 4: Thread session-aware state access through init/runtime wiring**

Use the `sessionID` created in `internal/cli/interactive.go` and bind it back into `runtime.Manager` after session creation so extensions get a stable per-session namespace.

Add an explicit late-binding API such as `Manager.BindSession(sessionID string, sessionsDir string)`.

Do this by extending the existing session plumbing, not by inventing a parallel directory resolver:
- the manager/state store should be inert or no-op until `BindSession(...)` is called
- pass the already-computed `sessionsDir` string from `interactive.go` instead of reaching into unexported `pisession.FileService` internals
- have the manager/state store compute `state/extensions/<extension-id>.json` under that existing session directory
- keep path ownership centralized instead of duplicating filesystem layout logic in multiple call sites

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/state_store.go internal/extension/manager.go internal/cli/interactive.go internal/extension/state_store_test.go internal/extension/manager_test.go internal/cli/interactive_test.go
rtk git commit -m "feat: add extension session state store"
```

---

## Chunk 3: Dynamic commands, tools, and events

### Task 7: Route commands through the manager and update completion/help

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/completion.go`
- Modify: `internal/tui/input.go`
- Modify: `internal/tui/types.go`
- Modify: `internal/tui/tui.go`
- Test: `internal/tui/extension_commands_test.go`
- Test: `internal/tui/completion_test.go`
- Test: `internal/tui/tui_update_test.go`

- [ ] **Step 1: Write the failing TUI command tests**

```go
func TestHandleSlashCommand_UsesDynamicExtensionCommand(t *testing.T) {}
func TestHandleSlashCommand_DeclarativeConflictWithBuiltinErrors(t *testing.T) {}
func TestManager_RejectsDynamicCommandConflictingWithBuiltin(t *testing.T) {}
func TestHelp_IncludesManagerCommands(t *testing.T) {}
func TestComplete_IncludesDynamicExtensionCommands(t *testing.T) {}
func TestMatchingCommands_IncludesDynamicExtensions(t *testing.T) {}
func TestCompleteSlashCommand_IncludesDynamicCommands(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/tui -run 'Test(HandleSlashCommand_UsesDynamicExtensionCommand|HandleSlashCommand_DeclarativeConflictWithBuiltinErrors|Manager_RejectsDynamicCommandConflictingWithBuiltin|Help_IncludesManagerCommands|Complete_IncludesDynamicExtensionCommands|MatchingCommands_IncludesDynamicExtensions|CompleteSlashCommand_IncludesDynamicCommands)'`
Expected: FAIL because commands still come only from static `ExtensionCommands`.

- [ ] **Step 3: Replace the narrow static command path with manager-backed lookup**

Split this into two focused sub-steps:
- **3a.** Add `Manager` (or a narrower command-provider interface) to `internal/tui/types.go` and wire it into `internal/tui/tui.go`
- **3a.1.** Seed the manager with the built-in command/reserved-name set so declarative and dynamic registrations can be checked against `/help`, `/clear`, `/model`, and the rest of the built-in slash-command namespace
- **3b.** Update every command-discovery path to read from that provider: the `handleSlashCommand` fallback, `AllCommandNames()`, `Complete()` / `matchingCommands()`, and `completeSlashCommand()` / `matchingSlashCommands()`
- **3c.** Make the standalone helper paths query the manager-backed source instead of the package-level static `slashCommands` slice; this likely requires a signature change or moving them behind a receiver/provider seam
  - in `input.go`: `completeSlashCommand()` and `matchingSlashCommands()`
  - in `completion.go`: `Complete()` and `matchingCommands()`

Keep existing declarative `tui.commands` behavior, but feed it through the manager so the TUI has one source of truth for command discovery and conflict handling.

Clarify registration phases:
- declarative `tui.commands` load first during static runtime assembly
- only after that phase is complete does dynamic runtime registration open
- duplicate rejection applies to dynamic registrations, not to the existing static manifest precedence rules during bootstrap

- [ ] **Step 4: Keep UX parity**

Verify:
- `/help` ordering stays deterministic
- command cycling works
- ghost/slash completion works through `Complete()` / `matchingCommands()` and `completeSlashCommand()` / `matchingSlashCommands()`
- old extension slash commands still submit prompts correctly
- declarative command precedence during bootstrap is preserved or fails with a clear error when it conflicts with a built-in

- [ ] **Step 5: Commit**

```bash
rtk git add internal/tui/commands.go internal/tui/completion.go internal/tui/input.go internal/tui/types.go internal/tui/tui.go internal/tui/extension_commands_test.go internal/tui/completion_test.go internal/tui/tui_update_test.go
rtk git commit -m "feat: route extension commands through manager"
```

### Task 8: Add typed events and safe tool registration hooks

**Files:**
- Modify: `internal/extension/events.go`
- Create: `internal/extension/intents.go`
- Modify: `internal/extension/manager.go`
- Modify: `internal/extension/runtime.go`
- Test: `internal/extension/manager_test.go`
- Test: `internal/extension/runtime_test.go`

- [ ] **Step 1: Write the failing event/tool tests**

```go
func TestManager_DeliversSessionStartEvent(t *testing.T) {}
func TestManager_DoesNotDeliverToUnsubscribedExtension(t *testing.T) {}
func TestManager_AllowsApprovedToolRegistration(t *testing.T) {}
func TestManager_DeniesToolInterceptForHostedThirdParty(t *testing.T) {}
func TestManager_RejectsDuplicateToolNames(t *testing.T) {}
func TestBuildRuntime_IncludesManagerRegisteredTools(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/extension -run 'TestManager_(DeliversSessionStartEvent|DoesNotDeliverToUnsubscribedExtension|AllowsApprovedToolRegistration|DeniesToolInterceptForHostedThirdParty|RejectsDuplicateToolNames)' && rtk go test ./internal/extension -run 'TestBuildRuntime_IncludesManagerRegisteredTools'`
Expected: FAIL because event delivery and dynamic tool registration are not implemented.

- [ ] **Step 3: Implement the smallest event fan-out**

Start with:
- startup
- session start
- command invoked
- tool start/result/error
- reload/shutdown

Explicitly defer these spec-listed events to later work once the core path is stable:
- user input submitted
- session switch/fork
- model change

Scope `internal/extension/intents.go` to command/tool intents in this task only. Add UI intents later in Task 9.

Do not add every possible event before these are stable.

- [ ] **Step 4: Register tools through the manager with trust gating**

Keep `tools.intercept` denied to `hosted_third_party` by default. Only allow dynamic tool registration when capability approval says so.

Use the approval/trust decisions already introduced in Task 3 through the manager's existing permission hooks; do not duplicate policy logic in new call sites.

Document namespaces explicitly: command names and tool names are separate namespaces in v1, so only same-kind collisions are rejected here unless the product later chooses a unified naming policy.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/events.go internal/extension/intents.go internal/extension/manager.go internal/extension/runtime.go internal/extension/manager_test.go internal/extension/runtime_test.go
rtk git commit -m "feat: add extension events and tool registration"
```

---

## Chunk 4: Async TUI bridge, render surfaces, and docs

### Task 9: Add the async TUI bridge before dialogs/widgets

**Files:**
- Create: `internal/tui/extension_msgs.go`
- Create: `internal/tui/extension_bridge.go`
- Modify: `internal/extension/intents.go`
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/types.go`
- Test: `internal/tui/extension_bridge_test.go`
- Test: `internal/tui/tui_update_test.go`

- [ ] **Step 1: Write the failing async bridge tests**

```go
func TestExtensionBridge_EmitsTeaMsgsWithoutBlocking(t *testing.T) {}
func TestTUI_AppliesStatusIntentFromExtensionMsg(t *testing.T) {}
func TestTUI_AppliesWidgetIntentAboveEditor(t *testing.T) {}
func TestTUI_AppliesWidgetIntentBelowEditor(t *testing.T) {}
func TestTUI_AppliesNotificationIntent(t *testing.T) {}
func TestTUI_AppliesDialogIntentAsModal(t *testing.T) {}
func TestTUI_IgnoresUIIntentInNonInteractiveMode(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/tui -run 'Test(ExtensionBridge_EmitsTeaMsgsWithoutBlocking|TUI_AppliesStatusIntentFromExtensionMsg|TUI_AppliesWidgetIntentAboveEditor|TUI_AppliesWidgetIntentBelowEditor|TUI_AppliesNotificationIntent|TUI_AppliesDialogIntentAsModal|TUI_IgnoresUIIntentInNonInteractiveMode)'`
Expected: FAIL because no extension bridge/messages or UI intents exist.

- [ ] **Step 3: Implement async message plumbing**

Use `tea.Cmd` / `tea.Msg` style dispatch so hosted extension I/O never blocks `Update()`.

Support only these UI intents first:
- status line update
- widget above/below editor
- non-blocking notification
- dialog intent rendered as a minimal modal/overlay surface

- [ ] **Step 4: Define UI intent types in `internal/extension/intents.go` and thread capability masks into the bridge**

Respect mode/trust constraints before applying UI intents.

Follow the existing `tui.go` async pattern: create a manager → bridge intent channel subscription plus a `tea.Cmd` listener (similar to the existing `waitFor*` helpers), then wire it from `Init()` so extension intent delivery never blocks `Update()`.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/intents.go internal/tui/extension_msgs.go internal/tui/extension_bridge.go internal/tui/tui.go internal/tui/types.go internal/tui/extension_bridge_test.go internal/tui/tui_update_test.go
rtk git commit -m "feat: add async tui extension bridge"
```

### Task 10: Add constrained custom renderers with fallback behavior

**Files:**
- Modify: `internal/extension/intents.go`
- Modify: `internal/extension/manager.go`
- Modify: `internal/tui/tool_display.go`
- Modify: `internal/tui/chat.go`
- Modify: `internal/tui/tui.go`
- Test: `internal/tui/tool_display_test.go`
- Test: `internal/tui/extension_bridge_test.go`
- Test: `internal/tui/tui_update_test.go`

- [ ] **Step 1: Write the failing renderer tests**

```go
func TestToolCallRow_UsesCustomRendererForSupportedType(t *testing.T) {}
func TestToolResult_FallsBackWhenExtensionRendererFails(t *testing.T) {}
func TestRenderer_FallsBackOnTimeout(t *testing.T) {}
func TestRenderer_RejectsConflictingOwnershipOnSameSurface(t *testing.T) {}
func TestRenderer_CleansUpOnExtensionUnload(t *testing.T) {}
func TestChat_UsesCustomMessageRendererForSupportedType(t *testing.T) {}
func TestRenderer_PlainTextAndMarkdownOnly(t *testing.T) {}
```

- [ ] **Step 2: Run the tests to verify failure**

Run: `rtk go test ./internal/tui -run 'Test(ToolCallRow_UsesCustomRendererForSupportedType|ToolResult_FallsBackWhenExtensionRendererFails|Renderer_FallsBackOnTimeout|Renderer_RejectsConflictingOwnershipOnSameSurface|Renderer_CleansUpOnExtensionUnload|Chat_UsesCustomMessageRendererForSupportedType|Renderer_PlainTextAndMarkdownOnly)'`
Expected: FAIL because custom renderers are not wired.

- [ ] **Step 3: Implement constrained renderer registration and fallback**

v1 rules:
- supported renderer payloads: `text`, `markdown`
- renderer errors/timeouts fall back to built-in rendering
- extensions do not own layout, only surface content

- [ ] **Step 4: Keep rendering deterministic**

Reject multiple renderers claiming the same resource/surface unless the resource owner matches. Add tests for ownership and unregister/reload cleanup.

- [ ] **Step 5: Commit**

```bash
rtk git add internal/extension/intents.go internal/extension/manager.go internal/tui/tool_display.go internal/tui/chat.go internal/tui/tui.go internal/tui/tool_display_test.go internal/tui/extension_bridge_test.go internal/tui/tui_update_test.go
rtk git commit -m "feat: add constrained extension renderers"
```

### Task 11: Update docs and examples after behavior is real

**Files:**
- Modify: `docs/extensions.md`
- Modify: `docs/customization.md`
- Create: `examples/extensions/hosted-hello/README.md`
- Create: `examples/extensions/hosted-hello/extension.json`
- Create: `examples/extensions/hosted-hello/main.go` or `main.js` (match chosen example runtime)
- Test: manual verification notes in plan only

- [ ] **Step 1: Write the docs/example acceptance checklist**

Document what must be true before docs land:
- manifest `runtime` fields exist
- approvals/trust model is implemented
- hosted command example actually runs
- renderer payload constraints are documented

- [ ] **Step 2: Update docs only after code/tests pass**

Describe:
- declarative vs compiled-in vs hosted extensions
- approval flow and trust classes
- supported v1 UI/render capabilities
- backward-compatible old manifest behavior

- [ ] **Step 3: Add a minimal hosted example**

Create the `examples/extensions/hosted-hello/` directory explicitly.

Note that this establishes a new top-level `examples/` convention in this repo. Before landing, verify it fits the repo layout and `.gitignore` expectations.

The example should demonstrate:
- handshake
- command registration
- status update or simple notification
- clean shutdown

- [ ] **Step 4: Run final targeted verification**

Run:
- `rtk go test ./internal/extension/...`
- `rtk go test ./internal/tui/...`
- `rtk go test ./internal/cli/...`

Expected: PASS across touched packages.

- [ ] **Step 5: Commit**

```bash
rtk git add docs/extensions.md docs/customization.md examples/extensions/hosted-hello
rtk git commit -m "docs: add hosted extension platform documentation"
```

---

## Plan review checklist

Before executing any chunk, confirm:
- the chunk changes one coherent subsystem
- tests fail first, then pass
- exact files are known before editing
- backward compatibility is covered where required
- each commit message matches the actual change

## Suggested execution order

1. Chunk 1 / Task 1
2. Chunk 1 / Task 2
3. Chunk 1 / Task 3
4. Review and pause
5. Chunk 2 / Tasks 4-6
6. Review and pause
7. Chunk 3 / Tasks 7-8
8. Review and pause
9. Chunk 4 / Tasks 9-11
10. Final verification and review

## Risks to watch during execution

- letting `runtime.go` become the dumping ground instead of keeping files focused
- blocking Bubble Tea `Update()` on hosted extension I/O
- reintroducing last-write-wins behavior for dynamic commands/tools
- allowing hosted third-party extensions to intercept tools too early
- documenting capabilities before the code path is actually working

## Done criteria

The implementation is done when:
- existing declarative extensions still work unchanged
- manager-backed dynamic command/tool registration works
- hosted extensions launch from manifest runtime metadata and complete handshake
- approvals/trust gating are enforced
- extension state persists per session/extension namespace
- TUI updates happen asynchronously and safely
- renderer failures fall back cleanly
- docs/examples reflect shipped behavior only

**Plan complete and saved to `docs/superpowers/plans/2026-04-05-rich-tui-extension-platform.md`. Ready to execute?**
