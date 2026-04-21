package api

import (
	"context"

	"github.com/pizzaface/go-pi/internal/extension/uitypes"
	"github.com/pizzaface/go-pi/pkg/piapi"
)

// Re-export uitypes so callers use api.ExtensionWidget etc. without
// importing uitypes directly. Type aliases preserve assignability.
type ExtensionWidget = uitypes.ExtensionWidget
type Position = uitypes.Position
type DialogField = uitypes.DialogField
type DialogButton = uitypes.DialogButton
type DialogSpec = uitypes.DialogSpec
type DialogResolution = uitypes.DialogResolution
type SessionMetadata = uitypes.SessionMetadata

// SessionBridge is the seam between extension API implementations
// (compiled + hosted) and the running host (TUI or CLI). Every spec #5
// method on piapi.API / piapi.CommandContext routes through this
// interface so the extension-side code stays identical across hosts.
type SessionBridge interface {
	// Messaging.
	AppendEntry(extID, kind string, payload any) error
	SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error
	SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error
	SetSessionTitle(title string) error
	GetSessionTitle() string
	SetEntryLabel(entryID, label string) error

	// Session control (CommandContext only).
	WaitForIdle(ctx context.Context) error
	NewSession(opts piapi.NewSessionOptions) (piapi.NewSessionResult, error)
	Fork(entryID string) (piapi.ForkResult, error)
	NavigateBranch(targetID string) (piapi.NavigateResult, error)
	SwitchSession(sessionPath string) (piapi.SwitchResult, error)
	Reload(ctx context.Context) error

	// Streaming + logs.
	EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error
	AppendExtensionLog(extID, level, message string, fields map[string]any) error

	// UI.
	SetExtensionStatus(extID, text, style string) error
	ClearExtensionStatus(extID string) error
	SetExtensionWidget(extID string, w ExtensionWidget) error
	ClearExtensionWidget(extID, widgetID string) error
	EnqueueNotify(extID, level, text string, timeoutMs int) error
	ShowDialog(extID string, spec DialogSpec) (dialogID string, err error)

	// Session metadata.
	GetSessionMetadata() SessionMetadata
	SetSessionName(name string) error
	SetSessionTags(tags []string) error
}

// NoopBridge is the default bridge used when no host is wired (tests that
// don't care, early-boot paths). All mutating calls are no-ops; accessors
// return zero values.
type NoopBridge struct{}

func (NoopBridge) AppendEntry(string, string, any) error                                  { return nil }
func (NoopBridge) SendCustomMessage(string, piapi.CustomMessage, piapi.SendOptions) error { return nil }
func (NoopBridge) SendUserMessage(string, piapi.UserMessage, piapi.SendOptions) error     { return nil }
func (NoopBridge) SetSessionTitle(string) error                                           { return nil }
func (NoopBridge) GetSessionTitle() string                                                { return "" }
func (NoopBridge) SetEntryLabel(string, string) error                                     { return nil }
func (NoopBridge) WaitForIdle(context.Context) error                                      { return nil }
func (NoopBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, nil
}
func (NoopBridge) Fork(string) (piapi.ForkResult, error) { return piapi.ForkResult{}, nil }
func (NoopBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, nil
}
func (NoopBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, nil
}
func (NoopBridge) Reload(context.Context) error                                    { return nil }
func (NoopBridge) EmitToolUpdate(string, piapi.ToolResult) error                   { return nil }
func (NoopBridge) AppendExtensionLog(string, string, string, map[string]any) error { return nil }
func (NoopBridge) SetExtensionStatus(string, string, string) error                 { return nil }
func (NoopBridge) ClearExtensionStatus(string) error                               { return nil }
func (NoopBridge) SetExtensionWidget(string, ExtensionWidget) error                { return nil }
func (NoopBridge) ClearExtensionWidget(string, string) error                       { return nil }
func (NoopBridge) EnqueueNotify(string, string, string, int) error                 { return nil }
func (NoopBridge) ShowDialog(string, DialogSpec) (string, error)                   { return "", nil }
func (NoopBridge) GetSessionMetadata() SessionMetadata                             { return SessionMetadata{} }
func (NoopBridge) SetSessionName(string) error                                     { return nil }
func (NoopBridge) SetSessionTags([]string) error                                   { return nil }
