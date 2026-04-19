package tui

import (
	"context"
	"errors"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/pkg/piapi"
)

// programSender is the Send interface of *tea.Program, extracted so
// tests can inject a recording fake.
type programSender interface {
	Send(msg tea.Msg)
}

// tuiSessionBridge implements api.SessionBridge for an interactive TUI.
// All mutations are dispatched through prog.Send so they're serialized
// on the model's Update goroutine.
type tuiSessionBridge struct {
	prog programSender

	mu          sync.Mutex
	latestTitle string
	idleWaiters []chan struct{}
	isIdle      bool
}

func newTUISessionBridge(prog programSender) *tuiSessionBridge {
	return &tuiSessionBridge{prog: prog, isIdle: true}
}

func (b *tuiSessionBridge) AppendEntry(extID, kind string, payload any) error {
	b.prog.Send(ExtensionEntryMsg{ExtensionID: extID, Kind: kind, Payload: payload})
	return nil
}

func (b *tuiSessionBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error {
	b.prog.Send(ExtensionSendCustomMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error {
	b.prog.Send(ExtensionSendUserMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SetSessionTitle(title string) error {
	b.mu.Lock()
	b.latestTitle = title
	b.mu.Unlock()
	b.prog.Send(ExtensionSetTitleMsg{Title: title})
	return nil
}

func (b *tuiSessionBridge) GetSessionTitle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latestTitle
}

func (b *tuiSessionBridge) SetEntryLabel(entryID, label string) error {
	b.prog.Send(ExtensionSetLabelMsg{EntryID: entryID, Label: label})
	return nil
}

// Stubs filled in Task 9 / Task 10 / Task 11.
func (b *tuiSessionBridge) WaitForIdle(context.Context) error { return errors.New("not yet wired") }
func (b *tuiSessionBridge) NewSession(piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	return piapi.NewSessionResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) Fork(string) (piapi.ForkResult, error) {
	return piapi.ForkResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) NavigateBranch(string) (piapi.NavigateResult, error) {
	return piapi.NavigateResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) SwitchSession(string) (piapi.SwitchResult, error) {
	return piapi.SwitchResult{}, errors.New("not yet wired")
}
func (b *tuiSessionBridge) Reload(context.Context) error                                    { return errors.New("not yet wired") }
func (b *tuiSessionBridge) EmitToolUpdate(string, piapi.ToolResult) error                   { return nil }
func (b *tuiSessionBridge) AppendExtensionLog(string, string, string, map[string]any) error { return nil }

var _ api.SessionBridge = (*tuiSessionBridge)(nil)
