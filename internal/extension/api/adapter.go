package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// NewPiapiToolAdapter wraps a piapi.ToolDescriptor so it satisfies the
// ADK tool.Tool interface. The descriptor's Parameters field, if present,
// is parsed as a JSON schema and used as the input schema; the output
// shape is the piapi.ToolResult envelope.
func NewPiapiToolAdapter(desc piapi.ToolDescriptor) (tool.Tool, error) {
	if err := desc.Validate(); err != nil {
		return nil, err
	}
	var schema *jsonschema.Schema
	if len(desc.Parameters) > 0 {
		schema = &jsonschema.Schema{}
		if err := json.Unmarshal(desc.Parameters, schema); err != nil {
			return nil, fmt.Errorf("piapi adapter: parse parameters for %q: %w", desc.Name, err)
		}
	}
	// map[string]any input lets the schema drive validation; map output is
	// what ADK runners expect for tool results.
	handler := func(ctx tool.Context, args map[string]any) (map[string]any, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		callID := ""
		var runCtx = context.Background()
		if ctx != nil {
			runCtx = ctx
			callID = ctx.FunctionCallID()
		}
		result, err := desc.Execute(runCtx, piapi.ToolCall{
			ID:   callID,
			Name: desc.Name,
			Args: raw,
		}, nil)
		if err != nil {
			return nil, err
		}
		return toolResultToMap(result), nil
	}
	return functiontool.New[map[string]any, map[string]any](
		functiontool.Config{
			Name:        desc.Name,
			Description: desc.Description,
			InputSchema: schema,
		},
		handler,
	)
}

func toolResultToMap(r piapi.ToolResult) map[string]any {
	out := map[string]any{}
	if len(r.Content) > 0 {
		parts := make([]map[string]any, len(r.Content))
		for i, c := range r.Content {
			parts[i] = map[string]any{"type": c.Type}
			if c.Text != "" {
				parts[i]["text"] = c.Text
			}
		}
		out["content"] = parts
	}
	if r.IsError {
		out["is_error"] = true
	}
	if len(r.Details) > 0 {
		out["details"] = r.Details
	}
	return out
}
