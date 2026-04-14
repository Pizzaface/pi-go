package piapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

var toolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ToolCall is the payload delivered to a tool's Execute function.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ContentPart is a single fragment of tool output.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is what Execute returns.
type ToolResult struct {
	Content []ContentPart  `json:"content"`
	Details map[string]any `json:"details,omitempty"`
	IsError bool           `json:"is_error,omitempty"`
}

// UpdateFunc is the streaming-progress callback passed to Execute.
// Nil means the caller is not listening for updates.
type UpdateFunc func(partial ToolResult)

// ToolDescriptor is the registration payload for pi.RegisterTool.
type ToolDescriptor struct {
	Name             string
	Label            string
	Description      string
	PromptSnippet    string
	PromptGuidelines []string
	Parameters       json.RawMessage
	PrepareArguments func(raw json.RawMessage) (json.RawMessage, error)
	Execute          func(ctx context.Context, call ToolCall, onUpdate UpdateFunc) (ToolResult, error)
}

// Validate returns non-nil if the descriptor is missing required fields.
func (d ToolDescriptor) Validate() error {
	if !toolNameRe.MatchString(d.Name) {
		return fmt.Errorf("piapi: invalid tool name %q", d.Name)
	}
	if d.Description == "" {
		return fmt.Errorf("piapi: tool %q: description is required", d.Name)
	}
	if d.Execute == nil {
		return fmt.Errorf("piapi: tool %q: Execute is required", d.Name)
	}
	return nil
}

// ToolInfo is the read-only shape returned by API.GetAllTools (spec #3).
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	SourceInfo  SourceInfo      `json:"source_info"`
}

// SourceInfo describes where a tool or command came from.
type SourceInfo struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Scope  string `json:"scope"`
	Origin string `json:"origin"`
}
