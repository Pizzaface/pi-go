package tui

import (
	"sync"
	"testing"

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
	b := newTUISessionBridge(prog)

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
	b := newTUISessionBridge(prog)

	_ = b.SetSessionTitle("alpha")
	if got := b.GetSessionTitle(); got != "alpha" {
		t.Fatalf("title = %q; want alpha", got)
	}
}

func TestTUISessionBridge_SteerSendUserMessageDispatches(t *testing.T) {
	prog, captured := newCapturingProgram(t)
	b := newTUISessionBridge(prog)
	_ = b.SendUserMessage("ext", piapi.UserMessage{
		Content: []piapi.ContentPart{{Type: "text", Text: "abort"}},
	}, piapi.SendOptions{DeliverAs: "steer", TriggerTurn: true})
	msgs := captured()
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d; want 1", len(msgs))
	}
}
