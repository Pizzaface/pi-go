package api

import (
	"context"

	"github.com/dimetron/pi-go/pkg/piapi"
)

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
}

// NoopBridge is the default bridge used when no host is wired (tests that
// don't care, early-boot paths). All mutating calls are no-ops; accessors
// return zero values.
type NoopBridge struct{}

func (NoopBridge) AppendEntry(string, string, any) error                           { return nil }
func (NoopBridge) SendCustomMessage(string, piapi.CustomMessage, piapi.SendOptions) error { return nil }
func (NoopBridge) SendUserMessage(string, piapi.UserMessage, piapi.SendOptions) error     { return nil }
func (NoopBridge) SetSessionTitle(string) error                                    { return nil }
func (NoopBridge) GetSessionTitle() string                                         { return "" }
func (NoopBridge) SetEntryLabel(string, string) error                              { return nil }
func (NoopBridge) WaitForIdle(context.Context) error                               { return nil }
func (NoopBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, nil
}
func (NoopBridge) Fork(string) (piapi.ForkResult, error)             { return piapi.ForkResult{}, nil }
func (NoopBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, nil
}
func (NoopBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, nil
}
func (NoopBridge) Reload(context.Context) error                               { return nil }
func (NoopBridge) EmitToolUpdate(string, piapi.ToolResult) error              { return nil }
func (NoopBridge) AppendExtensionLog(string, string, string, map[string]any) error { return nil }
