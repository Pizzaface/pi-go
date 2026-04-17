// Package testing contains test helpers for extension API implementations.
package testing

import (
	"context"
	"sync"

	"github.com/dimetron/pi-go/pkg/piapi"
)

// Call is a single recorded call on FakeBridge.
type Call struct {
	Method string
	Args   map[string]any
}

// FakeBridge records every method call and lets tests pre-seed return
// values or errors. Safe for concurrent use.
type FakeBridge struct {
	mu        sync.Mutex
	Calls     []Call
	Title     string
	NewResult piapi.NewSessionResult
	ForkRes   piapi.ForkResult
	NavRes    piapi.NavigateResult
	SwitchRes piapi.SwitchResult
	Err       error // returned from every mutating method when non-nil
}

func (f *FakeBridge) record(m string, a map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Method: m, Args: a})
	return f.Err
}

func (f *FakeBridge) AppendEntry(extID, kind string, payload any) error {
	return f.record("AppendEntry", map[string]any{"ext": extID, "kind": kind, "payload": payload})
}
func (f *FakeBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error {
	return f.record("SendCustomMessage", map[string]any{"ext": extID, "msg": msg, "opts": opts})
}
func (f *FakeBridge) SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error {
	return f.record("SendUserMessage", map[string]any{"ext": extID, "msg": msg, "opts": opts})
}
func (f *FakeBridge) SetSessionTitle(title string) error {
	f.mu.Lock()
	f.Title = title
	f.mu.Unlock()
	return f.record("SetSessionTitle", map[string]any{"title": title})
}
func (f *FakeBridge) GetSessionTitle() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	title := f.Title
	f.Calls = append(f.Calls, Call{Method: "GetSessionTitle", Args: nil})
	return title
}
func (f *FakeBridge) SetEntryLabel(entryID, label string) error {
	return f.record("SetEntryLabel", map[string]any{"entry": entryID, "label": label})
}
func (f *FakeBridge) WaitForIdle(_ context.Context) error {
	return f.record("WaitForIdle", nil)
}
func (f *FakeBridge) NewSession(opts piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return f.NewResult, f.record("NewSession", map[string]any{"opts": opts})
}
func (f *FakeBridge) Fork(entryID string) (piapi.ForkResult, error) {
	return f.ForkRes, f.record("Fork", map[string]any{"entry": entryID})
}
func (f *FakeBridge) NavigateBranch(targetID string) (piapi.NavigateResult, error) {
	return f.NavRes, f.record("NavigateBranch", map[string]any{"target": targetID})
}
func (f *FakeBridge) SwitchSession(path string) (piapi.SwitchResult, error) {
	return f.SwitchRes, f.record("SwitchSession", map[string]any{"path": path})
}
func (f *FakeBridge) Reload(_ context.Context) error {
	return f.record("Reload", nil)
}
func (f *FakeBridge) EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error {
	return f.record("EmitToolUpdate", map[string]any{"id": toolCallID, "partial": partial})
}
func (f *FakeBridge) AppendExtensionLog(extID, level, msg string, fields map[string]any) error {
	return f.record("AppendExtensionLog", map[string]any{"ext": extID, "level": level, "msg": msg, "fields": fields})
}
