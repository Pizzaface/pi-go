package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/pizzaface/go-pi/internal/extension/host"
	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

// defaultHostedToolTimeoutMS is the per-call upper bound the host waits on
// the extension before declaring the invocation timed out.
const defaultHostedToolTimeoutMS = 30_000

// hostedToolAdapter embeds the functiontool-produced tool.Tool and keeps a
// handle on the originating HostedToolEntry so test helpers can drive the
// invocation path without routing through an ADK runner.
type hostedToolAdapter struct {
	tool.Tool
	entry HostedToolEntry
}

// HostedEntry returns the HostedToolEntry that produced this adapter.
// Exposed for tests; the ADK runner never calls it.
func (h hostedToolAdapter) HostedEntry() HostedToolEntry { return h.entry }

// NewHostedToolAdapter wraps a HostedToolEntry as an ADK tool.Tool. On
// invocation, the adapter gate-checks events.tool_execute and then sends
// pi.extension/extension_event with a "tool_execute" payload over the
// extension's RPC connection. The reply is converted to the ADK-shaped
// result envelope (content / details / is_error).
func NewHostedToolAdapter(entry HostedToolEntry) (tool.Tool, error) {
	// The hosted Desc.Execute is intentionally nil — invocation happens via
	// RPC, not in-process — so we can't use Desc.Validate() here (it insists
	// on a non-nil Execute). Enforce the few fields we rely on directly.
	if entry.Desc.Name == "" {
		return nil, fmt.Errorf("hosted adapter: tool name is required")
	}

	var schema *jsonschema.Schema
	if len(entry.Desc.Parameters) > 0 {
		schema = &jsonschema.Schema{}
		if err := json.Unmarshal(entry.Desc.Parameters, schema); err != nil {
			return nil, fmt.Errorf("hosted adapter %q: parse schema: %w", entry.Desc.Name, err)
		}
	}

	handler := func(ctx tool.Context, args map[string]any) (map[string]any, error) {
		runCtx := context.Background()
		callID := ""
		if ctx != nil {
			runCtx = ctx
			callID = ctx.FunctionCallID()
		}
		return invokeHosted(runCtx, callID, entry, args)
	}
	ft, err := functiontool.New[map[string]any, map[string]any](
		functiontool.Config{
			Name:        entry.Desc.Name,
			Description: entry.Desc.Description,
			InputSchema: schema,
		},
		handler,
	)
	if err != nil {
		return nil, err
	}
	return hostedToolAdapter{Tool: ft, entry: entry}, nil
}

// invokeHosted performs the gate check, builds the extension_event payload,
// dispatches it over the extension's RPC connection, and reshapes the reply
// into the map envelope ADK expects from a tool result.
func invokeHosted(ctx context.Context, callID string, entry HostedToolEntry, args map[string]any) (map[string]any, error) {
	if entry.Manager != nil {
		var trust host.TrustClass
		if entry.Reg != nil {
			trust = entry.Reg.Trust
		}
		if ok, reason := entry.Manager.Gate().Allowed(entry.ExtID, "events.tool_execute", trust); !ok {
			return nil, fmt.Errorf("events.tool_execute denied for %s: %s", entry.ExtID, reason)
		}
	}
	if entry.Reg == nil || entry.Reg.Conn == nil {
		return map[string]any{
			"is_error": true,
			"content":  []map[string]any{{"type": "text", "text": "extension not connected"}},
		}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	innerPayload, err := json.Marshal(map[string]any{
		"tool_call_id": callID,
		"name":         entry.Desc.Name,
		"args":         json.RawMessage(rawArgs),
		"timeout_ms":   defaultHostedToolTimeoutMS,
	})
	if err != nil {
		return nil, err
	}
	params := hostproto.ExtensionEventParams{
		Event:   "tool_execute",
		Version: 1,
		Payload: innerPayload,
	}
	var resp struct {
		Content []map[string]any `json:"content"`
		Details map[string]any   `json:"details"`
		IsError bool             `json:"is_error"`
	}
	if err := entry.Reg.Conn.Call(ctx, hostproto.MethodExtensionEvent, params, &resp); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(resp.Content) > 0 {
		anyContent := make([]any, len(resp.Content))
		for i, c := range resp.Content {
			anyContent[i] = c
		}
		out["content"] = anyContent
	}
	if resp.IsError {
		out["is_error"] = true
	}
	if len(resp.Details) > 0 {
		out["details"] = resp.Details
	}
	return out, nil
}

// invokeHostedAdapterForTest lets adapter_hosted_test.go drive invokeHosted
// without standing up an ADK function-tool runner. It recovers the originating
// HostedToolEntry from the adapter wrapper and calls invokeHosted directly.
func invokeHostedAdapterForTest(tl tool.Tool, ctx context.Context, callID string, args map[string]any) (map[string]any, error) {
	h, ok := tl.(interface{ HostedEntry() HostedToolEntry })
	if !ok {
		return nil, fmt.Errorf("tool was not built by NewHostedToolAdapter")
	}
	return invokeHosted(ctx, callID, h.HostedEntry(), args)
}
