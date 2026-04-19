package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dimetron/pi-go/pkg/piapi"
)

type capturedMsgs struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *capturedMsgs) Send(m tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
}

func newCapturingProgram(t *testing.T) (programSender, func() []tea.Msg) {
	t.Helper()
	c := &capturedMsgs{}
	return c, func() []tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		return append([]tea.Msg(nil), c.msgs...)
	}
}

func TestTUISessionBridge_AppendEntryDispatches(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")

	if err := b.AppendEntry("ext", "info", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}

	msgs := captured()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d; want 1", len(msgs))
	}
	m, ok := msgs[0].(ExtensionEntryMsg)
	if !ok {
		t.Fatalf("msg = %T; want ExtensionEntryMsg", msgs[0])
	}
	if m.ExtensionID != "ext" || m.Kind != "info" {
		t.Fatalf("bad msg: %+v", m)
	}
}

func TestTUISessionBridge_TitleRoundtrip(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")

	_ = b.SetSessionTitle("alpha")
	if got := b.GetSessionTitle(); got != "alpha" {
		t.Fatalf("title = %q; want alpha", got)
	}
}

func TestTUISessionBridge_SteerSendUserMessageDispatches(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")
	_ = b.SendUserMessage("ext", piapi.UserMessage{
		Content: []piapi.ContentPart{{Type: "text", Text: "abort"}},
	}, piapi.SendOptions{DeliverAs: "steer", TriggerTurn: true})
	msgs := captured()
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d; want 1", len(msgs))
	}
}

func TestTUISessionBridge_WaitForIdleReturnsWhenIdle(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")
	// bridge starts idle by default
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle on idle bridge returned error: %v", err)
	}
}

func TestTUISessionBridge_WaitForIdleBlocksUntilMark(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")
	b.markBusy()

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- b.WaitForIdle(ctx)
	}()

	// Should not be done yet
	select {
	case err := <-done:
		t.Fatalf("WaitForIdle returned early with: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	b.markIdle()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitForIdle returned error after markIdle: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle did not return after markIdle")
	}
}

func TestTUISessionBridge_WaitForIdleRemovesWaiterOnCancel(t *testing.T) {
	prog, _ := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")
	b.markBusy()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := b.WaitForIdle(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForIdle error = %v; want context.DeadlineExceeded", err)
	}
	if count := b.idleWaiterCount(); count != 0 {
		t.Fatalf("idleWaiterCount = %d; want 0 after ctx cancel", count)
	}
}

func TestTUISessionBridge_NilProgReturnsError(t *testing.T) {
	b := newTUISessionBridge(nil, "")

	_, err := b.NewSession(piapi.NewSessionOptions{})
	if !errors.Is(err, errBridgeNotReady) {
		t.Errorf("NewSession: got %v; want errBridgeNotReady", err)
	}

	_, err = b.Fork("entry-1")
	if !errors.Is(err, errBridgeNotReady) {
		t.Errorf("Fork: got %v; want errBridgeNotReady", err)
	}

	_, err = b.NavigateBranch("target-1")
	if !errors.Is(err, errBridgeNotReady) {
		t.Errorf("NavigateBranch: got %v; want errBridgeNotReady", err)
	}

	_, err = b.SwitchSession("/some/path")
	if !errors.Is(err, errBridgeNotReady) {
		t.Errorf("SwitchSession: got %v; want errBridgeNotReady", err)
	}

	err = b.Reload(context.Background())
	if !errors.Is(err, errBridgeNotReady) {
		t.Errorf("Reload: got %v; want errBridgeNotReady", err)
	}
}

func TestTUISessionBridge_ForkSendsReq(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog, "")

	// Fork sends ExtensionForkReq and blocks on Done channel.
	// We run it in a goroutine and reply manually.
	done := make(chan piapi.ForkResult, 1)
	go func() {
		result, _ := b.Fork("entry-abc")
		done <- result
	}()

	// Wait for the message to be sent.
	var req ExtensionForkReq
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		msgs := captured()
		for _, m := range msgs {
			if r, ok := m.(ExtensionForkReq); ok {
				req = r
				goto found
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("ExtensionForkReq not dispatched within 1s")
found:
	if req.EntryID != "entry-abc" {
		t.Fatalf("EntryID = %q; want entry-abc", req.EntryID)
	}
	req.Done <- ExtensionForkReply{Result: piapi.ForkResult{BranchID: "branch-1"}}

	select {
	case r := <-done:
		if r.BranchID != "branch-1" {
			t.Fatalf("BranchID = %q; want branch-1", r.BranchID)
		}
	case <-time.After(time.Second):
		t.Fatal("Fork did not return after reply")
	}
}
