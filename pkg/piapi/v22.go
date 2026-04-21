package piapi

import (
	"context"
	"encoding/json"
)

// Types for v2.2 services (state/commands/ui/sigils/session-metadata).

// Position describes where a UI widget should anchor.
type Position struct {
	Mode    string `json:"mode,omitempty"`   // static|relative|absolute|sticky|fixed
	Anchor  string `json:"anchor,omitempty"` // top|bottom|left|right
	OffsetX int    `json:"offset_x,omitempty"`
	OffsetY int    `json:"offset_y,omitempty"`
	Z       int    `json:"z,omitempty"`
}

// DialogField is a single input field in a UIDialog.
type DialogField struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // text|password|choice|bool
	Label   string   `json:"label,omitempty"`
	Default string   `json:"default,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

// DialogButton is a button on a UIDialog.
type DialogButton struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style,omitempty"`
}

// CommandsInvokeEvent is dispatched when a user invokes a registered slash command.
type CommandsInvokeEvent struct {
	Name    string `json:"name"`
	Args    string `json:"args"`
	EntryID string `json:"entry_id,omitempty"`
}

// CommandsInvokeResult is the extension's response to a commands.invoke event.
type CommandsInvokeResult struct {
	Handled bool   `json:"handled"`
	Message string `json:"message,omitempty"`
	Silent  bool   `json:"silent,omitempty"`
}

// SigilResolveEvent is dispatched when a sigil appears in the input.
type SigilResolveEvent struct {
	Prefix  string `json:"prefix"`
	ID      string `json:"id"`
	Context string `json:"context,omitempty"`
}

// SigilResolveResult is the extension's response to a sigil resolve.
type SigilResolveResult struct {
	Display string         `json:"display"`
	Style   string         `json:"style,omitempty"`
	Hover   string         `json:"hover,omitempty"`
	Actions []string       `json:"actions,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// SigilActionEvent is dispatched when a user triggers an action on a sigil.
type SigilActionEvent struct {
	Prefix string `json:"prefix"`
	ID     string `json:"id"`
	Action string `json:"action"`
}

// SigilActionResult is the extension's response to a sigil action.
type SigilActionResult struct {
	Handled bool `json:"handled"`
}

// SessionMetadataSnapshot is the shape returned by SessionGetMetadata.
type SessionMetadataSnapshot struct {
	Name      string   `json:"name,omitempty"`
	Title     string   `json:"title,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"` // RFC3339
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// APIv22 extends the base API with v2.2 service helpers. Implementations
// that predate v2.2 should embed this with stubs that return
// ErrNotImplemented.
type APIv22 interface {
	// state
	StateGet(ctx context.Context) (value json.RawMessage, exists bool, err error)
	StateSet(ctx context.Context, v any) error
	StatePatch(ctx context.Context, patch json.RawMessage) error
	StateDelete(ctx context.Context) error

	// commands
	CommandsRegister(ctx context.Context, name, label, description, argHint string) error
	CommandsUnregister(ctx context.Context, name string) error
	OnCommandInvoke(fn func(CommandsInvokeEvent) CommandsInvokeResult)

	// ui
	UIStatus(ctx context.Context, text, style string) error
	UIClearStatus(ctx context.Context) error
	UIWidget(ctx context.Context, id, title string, lines []string, pos Position) error
	UIClearWidget(ctx context.Context, id string) error
	UINotify(ctx context.Context, level, text string, timeoutMs int) error
	UIDialog(ctx context.Context, title string, fields []DialogField, buttons []DialogButton) (dialogID string, err error)

	// sigils
	SigilsRegister(ctx context.Context, prefixes []string) error
	SigilsUnregister(ctx context.Context, prefixes []string) error
	OnSigilResolve(fn func(SigilResolveEvent) SigilResolveResult)
	OnSigilAction(fn func(SigilActionEvent) SigilActionResult)

	// session metadata
	SessionGetMetadata(ctx context.Context) (SessionMetadataSnapshot, error)
	SessionSetName(ctx context.Context, name string) error
	SessionSetTags(ctx context.Context, tags []string) error
}
