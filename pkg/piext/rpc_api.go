package piext

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

type rpcAPI struct {
	transport *Transport
	metadata  piapi.Metadata
	granted   map[string]map[string]bool // service → method → true

	mu       sync.Mutex
	tools    map[string]piapi.ToolDescriptor
	handlers map[string][]piapi.EventHandler

	// v2.2 typed event callbacks.
	onCommandInvoke func(piapi.CommandsInvokeEvent) piapi.CommandsInvokeResult
	onSigilResolve  func(piapi.SigilResolveEvent) piapi.SigilResolveResult
	onSigilAction   func(piapi.SigilActionEvent) piapi.SigilActionResult
}

func newRPCAPI(t *Transport, meta piapi.Metadata, granted []GrantedService) *rpcAPI {
	gmap := make(map[string]map[string]bool)
	for _, svc := range granted {
		methods := make(map[string]bool, len(svc.Methods))
		for _, m := range svc.Methods {
			methods[m] = true
		}
		gmap[svc.Service] = methods
	}
	api := &rpcAPI{
		transport: t,
		metadata:  meta,
		granted:   gmap,
		tools:     map[string]piapi.ToolDescriptor{},
		handlers:  map[string][]piapi.EventHandler{},
	}
	t.HandleRequest("pi.extension/extension_event", api.onEvent)
	return api
}

func (a *rpcAPI) Name() string    { return a.metadata.Name }
func (a *rpcAPI) Version() string { return a.metadata.Version }

func (a *rpcAPI) checkGrant(service, method string) error {
	m, ok := a.granted[service]
	if !ok || !m[method] {
		return piapi.ErrCapabilityDenied{Capability: service + "." + method}
	}
	return nil
}

func (a *rpcAPI) hostCall(method string, payload any, result any) error {
	svc, m := splitCap(method)
	if err := a.checkGrant(svc, m); err != nil {
		return err
	}
	return a.transport.Call(context.Background(), "pi.extension/host_call", map[string]any{
		"service": svc, "version": 1, "method": m, "payload": payload,
	}, result)
}

func (a *rpcAPI) RegisterTool(desc piapi.ToolDescriptor) error {
	if err := desc.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	a.tools[desc.Name] = desc
	a.mu.Unlock()

	payload := map[string]any{
		"name":              desc.Name,
		"label":             desc.Label,
		"description":       desc.Description,
		"prompt_snippet":    desc.PromptSnippet,
		"prompt_guidelines": desc.PromptGuidelines,
		"parameters":        json.RawMessage(desc.Parameters),
	}
	var result map[string]any
	return a.hostCall("tools.register", payload, &result)
}

func (a *rpcAPI) RegisterCommand(_ string, _ piapi.CommandDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterCommand", Spec: "#2"}
}
func (a *rpcAPI) RegisterShortcut(_ string, _ piapi.ShortcutDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterShortcut", Spec: "#6"}
}
func (a *rpcAPI) RegisterFlag(_ string, _ piapi.FlagDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterFlag", Spec: "#6"}
}
func (a *rpcAPI) RegisterProvider(_ string, _ piapi.ProviderDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterProvider", Spec: "#6"}
}
func (a *rpcAPI) UnregisterProvider(_ string) error {
	return piapi.ErrNotImplemented{Method: "UnregisterProvider", Spec: "#6"}
}
func (a *rpcAPI) RegisterMessageRenderer(_ string, _ piapi.RendererDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterMessageRenderer", Spec: "#6"}
}

func (a *rpcAPI) On(eventName string, handler piapi.EventHandler) error {
	if eventName != piapi.EventSessionStart {
		return piapi.ErrNotImplemented{Method: "On(" + eventName + ")", Spec: "#3"}
	}
	if err := a.checkGrant("events", "session_start"); err != nil {
		return err
	}
	a.mu.Lock()
	a.handlers[eventName] = append(a.handlers[eventName], handler)
	a.mu.Unlock()
	var result map[string]any
	return a.transport.Call(context.Background(), "pi.extension/subscribe_event", map[string]any{
		"events": []map[string]any{{"name": eventName, "version": 1}},
	}, &result)
}

func (a *rpcAPI) onEvent(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Event   string          `json:"event"`
		Version int             `json:"version"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	handlers := append([]piapi.EventHandler(nil), a.handlers[req.Event]...)
	a.mu.Unlock()

	var evt piapi.Event
	switch req.Event {
	case piapi.EventSessionStart:
		var e piapi.SessionStartEvent
		if err := json.Unmarshal(req.Payload, &e); err != nil {
			return nil, err
		}
		evt = e
	case piapi.EventToolExecute:
		return a.handleToolExecute(req.Payload)
	case "commands.invoke":
		a.mu.Lock()
		fn := a.onCommandInvoke
		a.mu.Unlock()
		if fn == nil {
			return piapi.CommandsInvokeResult{Handled: false}, nil
		}
		var ev piapi.CommandsInvokeEvent
		if err := json.Unmarshal(req.Payload, &ev); err != nil {
			return nil, err
		}
		return fn(ev), nil
	case "sigils/resolve":
		a.mu.Lock()
		fn := a.onSigilResolve
		a.mu.Unlock()
		if fn == nil {
			return piapi.SigilResolveResult{}, nil
		}
		var ev piapi.SigilResolveEvent
		if err := json.Unmarshal(req.Payload, &ev); err != nil {
			return nil, err
		}
		return fn(ev), nil
	case "sigils/action":
		a.mu.Lock()
		fn := a.onSigilAction
		a.mu.Unlock()
		if fn == nil {
			return piapi.SigilActionResult{}, nil
		}
		var ev piapi.SigilActionEvent
		if err := json.Unmarshal(req.Payload, &ev); err != nil {
			return nil, err
		}
		return fn(ev), nil
	default:
		return map[string]any{"control": nil}, nil
	}

	var result piapi.EventResult
	for _, h := range handlers {
		r, err := h(evt, nil)
		if err != nil {
			return nil, err
		}
		if r.Control != nil {
			result = r
		}
	}
	return result, nil
}

func (a *rpcAPI) handleToolExecute(payload json.RawMessage) (any, error) {
	var call struct {
		ToolCallID string          `json:"tool_call_id"`
		Name       string          `json:"name"`
		Args       json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(payload, &call); err != nil {
		return nil, err
	}
	a.mu.Lock()
	desc, ok := a.tools[call.Name]
	a.mu.Unlock()
	if !ok {
		return map[string]any{"is_error": true, "content": []piapi.ContentPart{{Type: "text", Text: "unknown tool: " + call.Name}}}, nil
	}
	onUpdate := func(p piapi.ToolResult) {
		_ = a.transport.Notify("pi.extension/tool_update", map[string]any{
			"tool_call_id": call.ToolCallID,
			"partial":      p,
		})
	}
	result, err := desc.Execute(context.Background(), piapi.ToolCall{
		ID: call.ToolCallID, Name: call.Name, Args: call.Args,
	}, onUpdate)
	if err != nil {
		return map[string]any{"is_error": true, "content": []piapi.ContentPart{{Type: "text", Text: err.Error()}}}, nil
	}
	return result, nil
}

func (a *rpcAPI) Events() piapi.EventBus { return noopBus{} }

func (a *rpcAPI) SendMessage(msg piapi.CustomMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" {
		return piapi.ErrIncoherentOptions{Reason: "SendMessage cannot steer; use SendUserMessage"}
	}
	payload := map[string]any{
		"custom_type": msg.CustomType, "content": msg.Content,
		"display": msg.Display, "details": msg.Details,
		"deliver_as": opts.DeliverAs, "trigger_turn": opts.TriggerTurn,
	}
	var res map[string]any
	return a.hostCall("session.send_custom_message", payload, &res)
}

func (a *rpcAPI) SendUserMessage(msg piapi.UserMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" && !opts.TriggerTurn {
		return piapi.ErrIncoherentOptions{Reason: "steer requires TriggerTurn=true"}
	}
	content := make([]map[string]any, 0, len(msg.Content))
	for _, c := range msg.Content {
		content = append(content, map[string]any{"type": c.Type, "text": c.Text})
	}
	payload := map[string]any{
		"content":      content,
		"deliver_as":   opts.DeliverAs,
		"trigger_turn": opts.TriggerTurn,
	}
	var res map[string]any
	return a.hostCall("session.send_user_message", payload, &res)
}

func (a *rpcAPI) AppendEntry(kind string, payload any) error {
	if !isValidPiextKind(kind) {
		return piapi.ErrInvalidKind{Kind: kind}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	p := map[string]any{"kind": kind, "payload": json.RawMessage(body)}
	var res map[string]any
	return a.hostCall("session.append_entry", p, &res)
}

func (a *rpcAPI) SetSessionName(name string) error {
	var res map[string]any
	return a.hostCall("session.set_title", map[string]any{"title": name}, &res)
}

func (a *rpcAPI) GetSessionName() string {
	var res struct {
		Title string `json:"title"`
	}
	if err := a.hostCall("session.get_title", map[string]any{}, &res); err != nil {
		return ""
	}
	return res.Title
}

func (a *rpcAPI) SetLabel(entryID, label string) error {
	var res map[string]any
	return a.hostCall("session.set_entry_label", map[string]any{"entry_id": entryID, "label": label}, &res)
}
func (a *rpcAPI) GetActiveTools() []string      { return nil }
func (a *rpcAPI) GetAllTools() []piapi.ToolInfo { return nil }
func (a *rpcAPI) SetActiveTools(_ []string) error {
	return piapi.ErrNotImplemented{Method: "SetActiveTools", Spec: "#3"}
}
func (a *rpcAPI) SetModel(_ piapi.ModelRef) (bool, error) {
	return false, piapi.ErrNotImplemented{Method: "SetModel", Spec: "#3"}
}
func (a *rpcAPI) GetThinkingLevel() piapi.ThinkingLevel { return piapi.ThinkingOff }
func (a *rpcAPI) SetThinkingLevel(_ piapi.ThinkingLevel) error {
	return piapi.ErrNotImplemented{Method: "SetThinkingLevel", Spec: "#3"}
}
func (a *rpcAPI) Exec(ctx context.Context, cmd string, args []string, _ piapi.ExecOptions) (piapi.ExecResult, error) {
	if err := a.checkGrant("exec", "shell"); err != nil {
		return piapi.ExecResult{}, err
	}
	c := exec.CommandContext(ctx, cmd, args...)
	var stdout, stderr []byte
	var err error
	stdout, err = c.Output()
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	code := 0
	if c.ProcessState != nil {
		code = c.ProcessState.ExitCode()
	}
	return piapi.ExecResult{
		Stdout: string(stdout), Stderr: string(stderr), Code: code,
		Killed: ctx.Err() != nil,
	}, nil
}
func (a *rpcAPI) GetCommands() []piapi.CommandInfo { return nil }
func (a *rpcAPI) GetFlag(_ string) any             { return nil }

type noopBus struct{}

func (noopBus) On(string, func(any)) error {
	return piapi.ErrNotImplemented{Method: "Events.On", Spec: "#3"}
}
func (noopBus) Emit(string, any) error {
	return piapi.ErrNotImplemented{Method: "Events.Emit", Spec: "#3"}
}

func (a *rpcAPI) UnregisterTool(name string) error {
	a.mu.Lock()
	delete(a.tools, name)
	a.mu.Unlock()
	var result map[string]any
	return a.hostCall("tools.unregister", map[string]any{"name": name}, &result)
}

// Ready bypasses checkGrant: ext.ready is an unconditional lifecycle signal.
func (a *rpcAPI) Ready() error {
	var result map[string]any
	return a.transport.Call(context.Background(), "pi.extension/host_call", map[string]any{
		"service": "ext", "version": 1, "method": "ready", "payload": map[string]any{},
	}, &result)
}

// ---- v2.2 service helpers (APIv22) ----

func (a *rpcAPI) hostCallRaw(service, method string, payload any, result any) error {
	if err := a.checkGrant(service, method); err != nil {
		return err
	}
	return a.transport.Call(context.Background(), "pi.extension/host_call", map[string]any{
		"service": service, "version": 1, "method": method, "payload": payload,
	}, result)
}

func (a *rpcAPI) StateGet(_ context.Context) (json.RawMessage, bool, error) {
	var res struct {
		Value  json.RawMessage `json:"value,omitempty"`
		Exists bool            `json:"exists"`
	}
	err := a.hostCallRaw("state", "get", map[string]any{}, &res)
	return res.Value, res.Exists, err
}

func (a *rpcAPI) StateSet(_ context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var res map[string]any
	return a.hostCallRaw("state", "set", map[string]any{"value": json.RawMessage(b)}, &res)
}

func (a *rpcAPI) StatePatch(_ context.Context, patch json.RawMessage) error {
	var res map[string]any
	return a.hostCallRaw("state", "patch", map[string]any{"patch": patch}, &res)
}

func (a *rpcAPI) StateDelete(_ context.Context) error {
	var res map[string]any
	return a.hostCallRaw("state", "delete", map[string]any{}, &res)
}

func (a *rpcAPI) CommandsRegister(_ context.Context, name, label, description, argHint string) error {
	var res map[string]any
	return a.hostCallRaw("commands", "register", map[string]any{
		"name": name, "label": label, "description": description, "arg_hint": argHint,
	}, &res)
}

func (a *rpcAPI) CommandsUnregister(_ context.Context, name string) error {
	var res map[string]any
	return a.hostCallRaw("commands", "unregister", map[string]any{"name": name}, &res)
}

func (a *rpcAPI) OnCommandInvoke(fn func(piapi.CommandsInvokeEvent) piapi.CommandsInvokeResult) {
	a.mu.Lock()
	a.onCommandInvoke = fn
	a.mu.Unlock()
	a.ensureEventSubscribed("commands.invoke")
}

func (a *rpcAPI) UIStatus(_ context.Context, text, style string) error {
	var res map[string]any
	return a.hostCallRaw("ui", "status", map[string]any{"text": text, "style": style}, &res)
}

func (a *rpcAPI) UIClearStatus(_ context.Context) error {
	var res map[string]any
	return a.hostCallRaw("ui", "clear_status", map[string]any{}, &res)
}

func (a *rpcAPI) UIWidget(_ context.Context, id, title string, lines []string, pos piapi.Position) error {
	var res map[string]any
	return a.hostCallRaw("ui", "widget", map[string]any{
		"id": id, "title": title, "lines": lines,
		"position": map[string]any{
			"mode": pos.Mode, "anchor": pos.Anchor,
			"offset_x": pos.OffsetX, "offset_y": pos.OffsetY, "z": pos.Z,
		},
	}, &res)
}

func (a *rpcAPI) UIClearWidget(_ context.Context, id string) error {
	var res map[string]any
	return a.hostCallRaw("ui", "clear_widget", map[string]any{"id": id}, &res)
}

func (a *rpcAPI) UINotify(_ context.Context, level, text string, timeoutMs int) error {
	var res map[string]any
	return a.hostCallRaw("ui", "notify", map[string]any{
		"level": level, "text": text, "timeout_ms": timeoutMs,
	}, &res)
}

func (a *rpcAPI) UIDialog(_ context.Context, title string, fields []piapi.DialogField, buttons []piapi.DialogButton) (string, error) {
	var res struct {
		DialogID string `json:"dialog_id"`
	}
	err := a.hostCallRaw("ui", "dialog", map[string]any{
		"title": title, "fields": fields, "buttons": buttons,
	}, &res)
	return res.DialogID, err
}

func (a *rpcAPI) SigilsRegister(_ context.Context, prefixes []string) error {
	var res map[string]any
	return a.hostCallRaw("sigils", "register", map[string]any{"prefixes": prefixes}, &res)
}

func (a *rpcAPI) SigilsUnregister(_ context.Context, prefixes []string) error {
	var res map[string]any
	return a.hostCallRaw("sigils", "unregister", map[string]any{"prefixes": prefixes}, &res)
}

func (a *rpcAPI) OnSigilResolve(fn func(piapi.SigilResolveEvent) piapi.SigilResolveResult) {
	a.mu.Lock()
	a.onSigilResolve = fn
	a.mu.Unlock()
	a.ensureEventSubscribed("sigils/resolve")
}

func (a *rpcAPI) OnSigilAction(fn func(piapi.SigilActionEvent) piapi.SigilActionResult) {
	a.mu.Lock()
	a.onSigilAction = fn
	a.mu.Unlock()
	a.ensureEventSubscribed("sigils/action")
}

func (a *rpcAPI) SessionGetMetadata(_ context.Context) (piapi.SessionMetadataSnapshot, error) {
	var res piapi.SessionMetadataSnapshot
	err := a.hostCallRaw("session", "get_metadata", map[string]any{}, &res)
	return res, err
}

func (a *rpcAPI) SessionSetName(_ context.Context, name string) error {
	var res map[string]any
	return a.hostCallRaw("session", "set_name", map[string]any{"name": name}, &res)
}

func (a *rpcAPI) SessionSetTags(_ context.Context, tags []string) error {
	var res map[string]any
	return a.hostCallRaw("session", "set_tags", map[string]any{"tags": tags}, &res)
}

// ensureEventSubscribed fires a subscribe_event for the given name. Safe
// to call repeatedly; the host dedupes server-side.
func (a *rpcAPI) ensureEventSubscribed(name string) {
	var result map[string]any
	_ = a.transport.Call(context.Background(), "pi.extension/subscribe_event", map[string]any{
		"events": []map[string]any{{"name": name, "version": 1}},
	}, &result)
}

var piextKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func isValidPiextKind(k string) bool { return piextKindPattern.MatchString(k) }

type transportLogWriter struct {
	api *rpcAPI
}

// Write splits p on newlines; each non-empty line becomes a log.append
// notification. Returns len(p) unconditionally (never blocks stderr semantics).
func (w transportLogWriter) Write(p []byte) (int, error) {
	lines := strings.Split(string(p), "\n")
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		_ = w.api.transport.Notify("pi.extension/host_call", map[string]any{
			"service": "log", "version": 1, "method": "append",
			"payload": map[string]any{"level": "info", "message": ln},
		})
	}
	return len(p), nil
}
