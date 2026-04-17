package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/dimetron/pi-go/internal/extension/host"
	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// HostedAPIHandler serves inbound JSON-RPC calls from a hosted extension.
// It owns the bridge between the extension's API calls and the host's
// capability gate, dispatcher, and tool registry.
type HostedAPIHandler struct {
	manager *host.Manager
	reg     *host.Registration
	bridge  SessionBridge

	mu    sync.Mutex
	tools map[string]hostedTool
}

type hostedTool struct {
	Name        string          `json:"name"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// NewHostedHandler constructs a handler for the registration. The caller
// wires reg.Conn to route inbound requests here via RPCConn.
func NewHostedHandler(manager *host.Manager, reg *host.Registration, bridge SessionBridge) *HostedAPIHandler {
	if bridge == nil {
		bridge = NoopBridge{}
	}
	return &HostedAPIHandler{
		manager: manager,
		reg:     reg,
		bridge:  bridge,
		tools:   map[string]hostedTool{},
	}
}

// Tools returns the names of tools registered by this extension.
func (h *HostedAPIHandler) Tools() map[string]hostedTool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]hostedTool, len(h.tools))
	for k, v := range h.tools {
		out[k] = v
	}
	return out
}

// Handle dispatches a single JSON-RPC method. Return value is marshalled
// as the response result; an error becomes an RPC error response.
func (h *HostedAPIHandler) Handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case hostproto.MethodHostCall:
		return h.handleHostCall(params)
	case hostproto.MethodSubscribeEvent:
		return h.handleSubscribeEvent(params)
	case hostproto.MethodToolUpdate:
		// Legacy method name — route through tool_stream.update for one release.
		return h.handleToolStreamUpdate(params)
	case hostproto.MethodLog:
		// Legacy method name — route through log.append for one release.
		return h.handleLogAppend(params)
	default:
		return nil, fmt.Errorf("unsupported method %q", method)
	}
}

func (h *HostedAPIHandler) handleHostCall(params json.RawMessage) (any, error) {
	var p hostproto.HostCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("host_call: invalid params: %w", err)
	}
	capability := p.Service + "." + p.Method
	if ok, reason := h.manager.Gate().Allowed(h.reg.ID, capability, h.reg.Trust); !ok {
		return nil, fmt.Errorf("capability denied: %s (%s)", capability, reason)
	}
	switch p.Service {
	case hostproto.ServiceTools:
		if p.Method == "register" {
			return h.registerTool(p.Payload)
		}
	case "exec":
		if p.Method == "shell" {
			return h.execShell(p.Payload)
		}
	case hostproto.ServiceSession:
		return h.handleSession(p.Method, p.Payload)
	case hostproto.ServiceSessionControl:
		return h.handleSessionControl(p.Method, p.Payload)
	case hostproto.ServiceToolStream:
		if p.Method == hostproto.MethodToolStreamUpdate {
			return h.handleToolStreamUpdate(p.Payload)
		}
	case hostproto.ServiceLog:
		if p.Method == hostproto.MethodLogAppend {
			return h.handleLogAppend(p.Payload)
		}
	}
	return nil, fmt.Errorf("service %s.%s not implemented", p.Service, p.Method)
}

func (h *HostedAPIHandler) registerTool(payload json.RawMessage) (any, error) {
	var t hostedTool
	if err := json.Unmarshal(payload, &t); err != nil {
		return nil, fmt.Errorf("tools.register: invalid payload: %w", err)
	}
	if t.Name == "" {
		return nil, fmt.Errorf("tools.register: name is required")
	}
	h.mu.Lock()
	h.tools[t.Name] = t
	h.mu.Unlock()
	return map[string]any{"registered": true}, nil
}

func (h *HostedAPIHandler) execShell(payload json.RawMessage) (any, error) {
	// Spec #1 delegates to the compiled exec — but hosted extensions
	// run without the host doing the exec. For now, return a stub.
	var p struct {
		Cmd     string   `json:"cmd"`
		Args    []string `json:"args"`
		Timeout int      `json:"timeout"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("exec.shell: invalid payload: %w", err)
	}
	tmp := &host.Registration{ID: h.reg.ID, Trust: host.TrustCompiledIn, Metadata: h.reg.Metadata}
	capi := NewCompiled(tmp, h.manager, NoopBridge{}).(*compiledAPI)
	res, err := capi.Exec(context.Background(), p.Cmd, p.Args, piapi.ExecOptions{Timeout: p.Timeout})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (h *HostedAPIHandler) handleSession(method string, payload json.RawMessage) (any, error) {
	switch method {
	case hostproto.MethodSessionAppendEntry:
		var p hostproto.SessionAppendEntryParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		var body any
		if len(p.Payload) > 0 {
			_ = json.Unmarshal(p.Payload, &body)
		}
		return map[string]any{}, h.bridge.AppendEntry(h.reg.ID, p.Kind, body)

	case hostproto.MethodSessionSendCustomMessage:
		var p hostproto.SessionSendCustomMessageParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SendCustomMessage(h.reg.ID,
			piapi.CustomMessage{CustomType: p.CustomType, Content: p.Content, Display: p.Display, Details: p.Details},
			piapi.SendOptions{DeliverAs: p.DeliverAs, TriggerTurn: p.TriggerTurn})

	case hostproto.MethodSessionSendUserMessage:
		var p hostproto.SessionSendUserMessageParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		parts := make([]piapi.ContentPart, 0, len(p.Content))
		for _, c := range p.Content {
			parts = append(parts, piapi.ContentPart{Type: c.Type, Text: c.Text})
		}
		return map[string]any{}, h.bridge.SendUserMessage(h.reg.ID,
			piapi.UserMessage{Content: parts},
			piapi.SendOptions{DeliverAs: p.DeliverAs, TriggerTurn: p.TriggerTurn})

	case hostproto.MethodSessionSetTitle:
		var p hostproto.SessionSetTitleParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SetSessionTitle(p.Title)

	case hostproto.MethodSessionGetTitle:
		return hostproto.SessionGetTitleResult{Title: h.bridge.GetSessionTitle()}, nil

	case hostproto.MethodSessionSetEntryLabel:
		var p hostproto.SessionSetEntryLabelParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return map[string]any{}, h.bridge.SetEntryLabel(p.EntryID, p.Label)
	}
	return nil, fmt.Errorf("session.%s not implemented", method)
}

func (h *HostedAPIHandler) handleSessionControl(method string, payload json.RawMessage) (any, error) {
	switch method {
	case hostproto.MethodSessionControlWaitIdle:
		return map[string]any{}, h.bridge.WaitForIdle(context.Background())
	case hostproto.MethodSessionControlNew:
		r, err := h.bridge.NewSession(piapi.NewSessionOptions{})
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlNewResult{ID: r.ID, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlFork:
		var p hostproto.SessionControlForkParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		r, err := h.bridge.Fork(p.EntryID)
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlForkResult{BranchID: r.BranchID, BranchTitle: r.BranchTitle, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlNavigate:
		var p hostproto.SessionControlNavigateParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		r, err := h.bridge.NavigateBranch(p.TargetID)
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlNavigateResult{BranchID: r.BranchID, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlSwitch:
		var p hostproto.SessionControlSwitchParams
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		r, err := h.bridge.SwitchSession(p.SessionPath)
		if err != nil {
			return nil, err
		}
		return hostproto.SessionControlSwitchResult{SessionID: r.SessionID, Cancelled: r.Cancelled}, nil
	case hostproto.MethodSessionControlReload:
		return map[string]any{}, h.bridge.Reload(context.Background())
	}
	return nil, fmt.Errorf("session_control.%s not implemented", method)
}

func (h *HostedAPIHandler) handleToolStreamUpdate(payload json.RawMessage) (any, error) {
	var p hostproto.ToolStreamUpdateParams
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	var partial piapi.ToolResult
	if len(p.Partial) > 0 {
		_ = json.Unmarshal(p.Partial, &partial)
	}
	return map[string]any{}, h.bridge.EmitToolUpdate(p.ToolCallID, partial)
}

func (h *HostedAPIHandler) handleLogAppend(payload json.RawMessage) (any, error) {
	var p hostproto.LogParams
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if p.Level == "" {
		p.Level = "info"
	}
	return map[string]any{}, h.bridge.AppendExtensionLog(h.reg.ID, p.Level, p.Message, p.Fields)
}

func (h *HostedAPIHandler) handleSubscribeEvent(params json.RawMessage) (any, error) {
	var p hostproto.SubscribeEventParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("subscribe_event: invalid params: %w", err)
	}
	for _, ev := range p.Events {
		capability := "events." + ev.Name
		if ok, reason := h.manager.Gate().Allowed(h.reg.ID, capability, h.reg.Trust); !ok {
			return nil, fmt.Errorf("events.%s denied: %s", ev.Name, reason)
		}
		evName := ev.Name
		h.manager.Dispatcher().Subscribe(evName, host.Subscriber{
			ExtensionID: h.reg.ID,
			Call: func(ctx context.Context, payload json.RawMessage) (piapi.EventResult, error) {
				if h.reg.Conn == nil {
					return piapi.EventResult{}, fmt.Errorf("%s: connection not ready", h.reg.ID)
				}
				req := hostproto.ExtensionEventParams{
					Event:   evName,
					Version: 1,
					Payload: payload,
				}
				var resp piapi.EventResult
				if err := h.reg.Conn.Call(ctx, hostproto.MethodExtensionEvent, req, &resp); err != nil {
					if strings.Contains(err.Error(), "closed") {
						return piapi.EventResult{}, nil
					}
					return piapi.EventResult{}, err
				}
				return resp, nil
			},
		})
	}
	return map[string]any{"subscribed": len(p.Events)}, nil
}
