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
func NewHostedHandler(manager *host.Manager, reg *host.Registration) *HostedAPIHandler {
	return &HostedAPIHandler{
		manager: manager,
		reg:     reg,
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
		// Spec #1: accept & drop — spec #5 wires this to the UI.
		return map[string]any{}, nil
	case hostproto.MethodLog:
		// Spec #1: accept & drop.
		return map[string]any{}, nil
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
	switch {
	case p.Service == "tools" && p.Method == "register":
		return h.registerTool(p.Payload)
	case p.Service == "exec" && p.Method == "shell":
		return h.execShell(p.Payload)
	default:
		return nil, fmt.Errorf("service %s.%s not implemented", p.Service, p.Method)
	}
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
