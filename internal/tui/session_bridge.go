package tui

import (
	"context"
	"errors"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/internal/extension/api"
	"github.com/dimetron/pi-go/pkg/piapi"
)

var errBridgeNotReady = errors.New("tui session bridge not attached to a running program")

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
	if b.prog == nil {
		return errBridgeNotReady
	}
	b.prog.Send(ExtensionEntryMsg{ExtensionID: extID, Kind: kind, Payload: payload})
	return nil
}

func (b *tuiSessionBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error {
	if b.prog == nil {
		return errBridgeNotReady
	}
	b.prog.Send(ExtensionSendCustomMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error {
	if b.prog == nil {
		return errBridgeNotReady
	}
	b.prog.Send(ExtensionSendUserMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SetSessionTitle(title string) error {
	if b.prog == nil {
		return errBridgeNotReady
	}
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
	if b.prog == nil {
		return errBridgeNotReady
	}
	b.prog.Send(ExtensionSetLabelMsg{EntryID: entryID, Label: label})
	return nil
}

func (b *tuiSessionBridge) WaitForIdle(ctx context.Context) error {
	b.mu.Lock()
	if b.isIdle {
		b.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	b.idleWaiters = append(b.idleWaiters, ch)
	b.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		b.mu.Lock()
		for i, w := range b.idleWaiters {
			if w == ch {
				b.idleWaiters = append(b.idleWaiters[:i], b.idleWaiters[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		return ctx.Err()
	}
}

func (b *tuiSessionBridge) idleWaiterCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.idleWaiters)
}

func (b *tuiSessionBridge) markBusy() {
	b.mu.Lock()
	b.isIdle = false
	b.mu.Unlock()
}

func (b *tuiSessionBridge) markIdle() {
	b.mu.Lock()
	b.isIdle = true
	waiters := b.idleWaiters
	b.idleWaiters = nil
	b.mu.Unlock()
	for _, w := range waiters {
		close(w)
	}
}

func (b *tuiSessionBridge) NewSession(_ piapi.NewSessionOptions) (piapi.NewSessionResult, error) {
	if b.prog == nil {
		return piapi.NewSessionResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionNewSessionReply, 1)
	b.prog.Send(ExtensionNewSessionReq{Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) Fork(entryID string) (piapi.ForkResult, error) {
	if b.prog == nil {
		return piapi.ForkResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionForkReply, 1)
	b.prog.Send(ExtensionForkReq{EntryID: entryID, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) NavigateBranch(targetID string) (piapi.NavigateResult, error) {
	if b.prog == nil {
		return piapi.NavigateResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionNavigateReply, 1)
	b.prog.Send(ExtensionNavigateReq{TargetID: targetID, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) SwitchSession(path string) (piapi.SwitchResult, error) {
	if b.prog == nil {
		return piapi.SwitchResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionSwitchReply, 1)
	b.prog.Send(ExtensionSwitchReq{SessionPath: path, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) Reload(_ context.Context) error {
	if b.prog == nil {
		return errBridgeNotReady
	}
	done := make(chan error, 1)
	b.prog.Send(ExtensionReloadReq{Done: done})
	return <-done
}
func (b *tuiSessionBridge) EmitToolUpdate(string, piapi.ToolResult) error                   { return nil }
func (b *tuiSessionBridge) AppendExtensionLog(string, string, string, map[string]any) error { return nil }

var _ api.SessionBridge = (*tuiSessionBridge)(nil)
