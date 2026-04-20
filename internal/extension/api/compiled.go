package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// compiledAPI is a direct in-process implementation of piapi.API for
// compiled-in extensions. Compiled-in extensions bypass the capability
// gate entirely (TrustCompiledIn is implicit).
type compiledAPI struct {
	reg     *host.Registration
	manager *host.Manager
	bridge  SessionBridge

	mu       sync.Mutex
	tools    map[string]piapi.ToolDescriptor
	handlers map[string][]piapi.EventHandler
}

// NewCompiled builds a piapi.API backed by direct in-process dispatch.
// The bridge receives every spec #5 method call. Pass NoopBridge{} when
// a real host is not available.
func NewCompiled(reg *host.Registration, manager *host.Manager, bridge SessionBridge) piapi.API {
	if bridge == nil {
		bridge = NoopBridge{}
	}
	return &compiledAPI{
		reg:      reg,
		manager:  manager,
		bridge:   bridge,
		tools:    map[string]piapi.ToolDescriptor{},
		handlers: map[string][]piapi.EventHandler{},
	}
}

// CompiledTools reads the registered tools from a compiled-in extension's
// API returned by NewCompiled. Returns nil if the supplied api did not
// originate from NewCompiled.
func CompiledTools(api piapi.API) map[string]piapi.ToolDescriptor {
	if c, ok := api.(*compiledAPI); ok {
		return c.Tools()
	}
	return nil
}

// Tools returns the map of tool descriptors registered on this API. The
// runtime assembler reads this after Register returns.
func (c *compiledAPI) Tools() map[string]piapi.ToolDescriptor {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]piapi.ToolDescriptor, len(c.tools))
	for k, v := range c.tools {
		out[k] = v
	}
	return out
}

// Handlers returns the map of event handlers registered on this API.
func (c *compiledAPI) Handlers() map[string][]piapi.EventHandler {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string][]piapi.EventHandler, len(c.handlers))
	for k, v := range c.handlers {
		out[k] = append([]piapi.EventHandler(nil), v...)
	}
	return out
}

func (c *compiledAPI) Name() string    { return c.reg.Metadata.Name }
func (c *compiledAPI) Version() string { return c.reg.Metadata.Version }

func (c *compiledAPI) RegisterTool(desc piapi.ToolDescriptor) error {
	if err := desc.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.tools[desc.Name]; exists {
		return fmt.Errorf("piapi: tool %q already registered", desc.Name)
	}
	c.tools[desc.Name] = desc
	return nil
}

func (c *compiledAPI) RegisterCommand(string, piapi.CommandDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterCommand", Spec: "#2"}
}
func (c *compiledAPI) RegisterShortcut(string, piapi.ShortcutDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterShortcut", Spec: "#6"}
}
func (c *compiledAPI) RegisterFlag(string, piapi.FlagDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterFlag", Spec: "#6"}
}
func (c *compiledAPI) RegisterProvider(string, piapi.ProviderDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterProvider", Spec: "#6"}
}
func (c *compiledAPI) UnregisterProvider(string) error {
	return piapi.ErrNotImplemented{Method: "UnregisterProvider", Spec: "#6"}
}
func (c *compiledAPI) RegisterMessageRenderer(string, piapi.RendererDescriptor) error {
	return piapi.ErrNotImplemented{Method: "RegisterMessageRenderer", Spec: "#6"}
}

func (c *compiledAPI) UnregisterTool(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tools[name]; !ok {
		return fmt.Errorf("piapi: tool %q not registered", name)
	}
	delete(c.tools, name)
	return nil
}

func (c *compiledAPI) Ready() error {
	// Compiled-in extensions are ready synchronously when Register returns.
	return nil
}

func (c *compiledAPI) On(eventName string, handler piapi.EventHandler) error {
	if eventName != piapi.EventSessionStart {
		return piapi.ErrNotImplemented{Method: "On(" + eventName + ")", Spec: "#3"}
	}
	c.mu.Lock()
	c.handlers[eventName] = append(c.handlers[eventName], handler)
	c.mu.Unlock()
	c.manager.Dispatcher().Subscribe(eventName, host.Subscriber{
		ExtensionID: c.reg.ID,
		Call: func(ctx context.Context, payload json.RawMessage) (piapi.EventResult, error) {
			evt := piapi.SessionStartEvent{}
			if len(payload) > 0 {
				_ = json.Unmarshal(payload, &evt)
			}
			return handler(evt, nil)
		},
	})
	return nil
}

func (c *compiledAPI) Events() piapi.EventBus {
	return notImplementedBus{}
}

func (c *compiledAPI) SendMessage(msg piapi.CustomMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" {
		return piapi.ErrIncoherentOptions{Reason: "SendMessage cannot steer; use SendUserMessage"}
	}
	return c.bridge.SendCustomMessage(c.reg.ID, msg, opts)
}
func (c *compiledAPI) SendUserMessage(msg piapi.UserMessage, opts piapi.SendOptions) error {
	if opts.DeliverAs == "steer" && !opts.TriggerTurn {
		return piapi.ErrIncoherentOptions{Reason: "steer requires TriggerTurn=true"}
	}
	return c.bridge.SendUserMessage(c.reg.ID, msg, opts)
}
func (c *compiledAPI) AppendEntry(kind string, payload any) error {
	if !isValidKind(kind) {
		return piapi.ErrInvalidKind{Kind: kind}
	}
	return c.bridge.AppendEntry(c.reg.ID, kind, payload)
}
func (c *compiledAPI) SetSessionName(name string) error {
	return c.bridge.SetSessionTitle(name)
}
func (c *compiledAPI) GetSessionName() string { return c.bridge.GetSessionTitle() }
func (c *compiledAPI) SetLabel(entryID, label string) error {
	return c.bridge.SetEntryLabel(entryID, label)
}

func (c *compiledAPI) GetActiveTools() []string      { return nil }
func (c *compiledAPI) GetAllTools() []piapi.ToolInfo { return nil }
func (c *compiledAPI) SetActiveTools([]string) error {
	return piapi.ErrNotImplemented{Method: "SetActiveTools", Spec: "#3"}
}
func (c *compiledAPI) SetModel(piapi.ModelRef) (bool, error) {
	return false, piapi.ErrNotImplemented{Method: "SetModel", Spec: "#3"}
}
func (c *compiledAPI) GetThinkingLevel() piapi.ThinkingLevel { return piapi.ThinkingOff }
func (c *compiledAPI) SetThinkingLevel(piapi.ThinkingLevel) error {
	return piapi.ErrNotImplemented{Method: "SetThinkingLevel", Spec: "#3"}
}

func (c *compiledAPI) Exec(ctx context.Context, cmd string, args []string, opts piapi.ExecOptions) (piapi.ExecResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Millisecond)
		defer cancel()
	}
	command := exec.CommandContext(ctx, cmd, args...)
	var stdout, stderr collectingBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	res := piapi.ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			res.Killed = true
			res.Code = -1
			return res, nil
		}
		if ee, ok := err.(*exec.ExitError); ok {
			res.Code = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	res.Code = 0
	return res, nil
}

func (c *compiledAPI) GetCommands() []piapi.CommandInfo { return nil }
func (c *compiledAPI) GetFlag(string) any               { return nil }

var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func isValidKind(kind string) bool { return kindPattern.MatchString(kind) }

type collectingBuffer struct {
	data []byte
}

func (b *collectingBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *collectingBuffer) String() string { return string(b.data) }

type notImplementedBus struct{}

func (notImplementedBus) On(string, func(any)) error {
	return piapi.ErrNotImplemented{Method: "events.On", Spec: "#3"}
}
func (notImplementedBus) Emit(string, any) error {
	return piapi.ErrNotImplemented{Method: "events.Emit", Spec: "#3"}
}
