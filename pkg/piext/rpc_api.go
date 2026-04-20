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
