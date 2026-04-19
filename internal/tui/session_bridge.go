package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

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
	// prog is accessed atomically so AttachProgram and bridge methods can
	// race safely. We store *programSender because atomic.Pointer requires
	// a concrete pointer type; the interface value lives behind the pointer.
	prog    atomic.Pointer[programSender]
	logFile *extensionLogFile

	mu          sync.Mutex
	latestTitle string
	idleWaiters []chan struct{}
	isIdle      bool
}

func newTUISessionBridge(prog programSender, logPath string) *tuiSessionBridge {
	b := &tuiSessionBridge{
		isIdle:  true,
		logFile: newExtensionLogFile(logPath),
	}
	if prog != nil {
		b.prog.Store(&prog)
	}
	return b
}

// NewSessionBridge constructs a TUI session bridge with a nil program
// pointer. The caller must pass the result to tui.Config.Bridge; tui.Run
// calls AttachProgram once the bubbletea Program is created.
// Callers outside this package receive the bridge as api.SessionBridge;
// tui.Run recovers the concrete type internally via type assertion.
func NewSessionBridge(logPath string) api.SessionBridge {
	return newTUISessionBridge(nil, logPath)
}

// AttachProgram wires a bubbletea Program into a bridge that was constructed
// with a nil prog. Must be called before any extension can invoke bridge
// methods that dispatch through prog. All bridge send methods guard against
// nil prog, so calls that arrive before attachment return errBridgeNotReady
// rather than panicking.
func (b *tuiSessionBridge) AttachProgram(p programSender) {
	if p == nil {
		return
	}
	b.prog.Store(&p)
}

func (b *tuiSessionBridge) AppendEntry(extID, kind string, payload any) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	(*p).Send(ExtensionEntryMsg{ExtensionID: extID, Kind: kind, Payload: payload})
	return nil
}

func (b *tuiSessionBridge) SendCustomMessage(extID string, msg piapi.CustomMessage, opts piapi.SendOptions) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	(*p).Send(ExtensionSendCustomMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SendUserMessage(extID string, msg piapi.UserMessage, opts piapi.SendOptions) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	(*p).Send(ExtensionSendUserMsg{ExtensionID: extID, Message: msg, Options: opts})
	return nil
}

func (b *tuiSessionBridge) SetSessionTitle(title string) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	b.mu.Lock()
	b.latestTitle = title
	b.mu.Unlock()
	(*p).Send(ExtensionSetTitleMsg{Title: title})
	return nil
}

func (b *tuiSessionBridge) GetSessionTitle() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latestTitle
}

func (b *tuiSessionBridge) SetEntryLabel(entryID, label string) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	(*p).Send(ExtensionSetLabelMsg{EntryID: entryID, Label: label})
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
	p := b.prog.Load()
	if p == nil {
		return piapi.NewSessionResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionNewSessionReply, 1)
	(*p).Send(ExtensionNewSessionReq{Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) Fork(entryID string) (piapi.ForkResult, error) {
	p := b.prog.Load()
	if p == nil {
		return piapi.ForkResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionForkReply, 1)
	(*p).Send(ExtensionForkReq{EntryID: entryID, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) NavigateBranch(targetID string) (piapi.NavigateResult, error) {
	p := b.prog.Load()
	if p == nil {
		return piapi.NavigateResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionNavigateReply, 1)
	(*p).Send(ExtensionNavigateReq{TargetID: targetID, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) SwitchSession(path string) (piapi.SwitchResult, error) {
	p := b.prog.Load()
	if p == nil {
		return piapi.SwitchResult{}, errBridgeNotReady
	}
	done := make(chan ExtensionSwitchReply, 1)
	(*p).Send(ExtensionSwitchReq{SessionPath: path, Done: done})
	r := <-done
	return r.Result, r.Err
}

func (b *tuiSessionBridge) Reload(_ context.Context) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	done := make(chan error, 1)
	(*p).Send(ExtensionReloadReq{Done: done})
	return <-done
}
func (b *tuiSessionBridge) EmitToolUpdate(toolCallID string, partial piapi.ToolResult) error {
	p := b.prog.Load()
	if p == nil {
		return errBridgeNotReady
	}
	(*p).Send(ExtensionToolStreamMsg{ToolCallID: toolCallID, Partial: partial})
	return nil
}
func (b *tuiSessionBridge) AppendExtensionLog(extID, level, message string, fields map[string]any) error {
	ts := time.Now()
	if p := b.prog.Load(); p != nil {
		(*p).Send(ExtensionLogMsg{ExtensionID: extID, Level: level, Message: message, Fields: fields, Ts: ts})
	}
	return b.logFile.Write(extID, level, message, fields, ts)
}

var _ api.SessionBridge = (*tuiSessionBridge)(nil)
