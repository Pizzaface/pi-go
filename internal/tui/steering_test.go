package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMessageQueue_SteeringBasics(t *testing.T) {
	var q MessageQueue
	if q.HasSteering() {
		t.Error("expected no steering messages initially")
	}
	if q.SteeringCount() != 0 {
		t.Error("expected steering count 0")
	}

	q.QueueSteering("first", nil)
	q.QueueSteering("second", []string{"file.go"})

	if !q.HasSteering() {
		t.Error("expected steering messages")
	}
	if q.SteeringCount() != 2 {
		t.Errorf("expected 2, got %d", q.SteeringCount())
	}

	msgs := q.DrainSteering()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 drained messages, got %d", len(msgs))
	}
	if msgs[0].Text != "first" || msgs[0].Delivery != DeliverSteer {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Text != "second" || len(msgs[1].Mentions) != 1 {
		t.Errorf("unexpected second message: %+v", msgs[1])
	}

	if q.HasSteering() {
		t.Error("expected no steering messages after drain")
	}
}

func TestMessageQueue_FollowUpBasics(t *testing.T) {
	var q MessageQueue
	if q.HasFollowUp() {
		t.Error("expected no follow-ups initially")
	}

	q.QueueFollowUp("do this next", nil)
	q.QueueFollowUp("and then this", nil)

	if q.FollowUpCount() != 2 {
		t.Errorf("expected 2, got %d", q.FollowUpCount())
	}

	msg, ok := q.DrainOneFollowUp()
	if !ok {
		t.Fatal("expected a follow-up")
	}
	if msg.Text != "do this next" {
		t.Errorf("unexpected text: %q", msg.Text)
	}
	if msg.Delivery != DeliverFollowUp {
		t.Error("expected DeliverFollowUp delivery type")
	}
	if q.FollowUpCount() != 1 {
		t.Errorf("expected 1 remaining, got %d", q.FollowUpCount())
	}
}

func TestMessageQueue_Clear(t *testing.T) {
	var q MessageQueue
	q.QueueSteering("s1", nil)
	q.QueueFollowUp("f1", nil)

	q.Clear()

	if q.HasSteering() || q.HasFollowUp() {
		t.Error("expected empty after clear")
	}

	_, ok := q.DrainOneFollowUp()
	if ok {
		t.Error("expected no follow-up after clear")
	}
}

func TestMessageQueue_DrainOneFollowUp_Empty(t *testing.T) {
	var q MessageQueue
	_, ok := q.DrainOneFollowUp()
	if ok {
		t.Error("expected false for empty queue")
	}
}

func TestSteeringSubmit_QueuesAndShowsInChat(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.agentCh = make(chan agentMsg, 64)
	m.steeringNotify = make(chan struct{}, 1)

	msg := SteeringSubmitMsg{Text: "redirect now", Mentions: nil}
	m.handleSteeringSubmit(msg)

	if m.messageQueue.SteeringCount() != 1 {
		t.Error("expected 1 steering message queued")
	}

	// Check it appears in chat.
	found := false
	for _, cm := range m.chatModel.Messages {
		if cm.role == "user" && contains(cm.content, "redirect now") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected steering message visible in chat")
	}
}

func TestFollowUpSubmit_QueuesMessage(t *testing.T) {
	m := newTestModel(t)
	m.running = true

	msg := FollowUpSubmitMsg{Text: "after you're done", Mentions: nil}
	m.handleFollowUpSubmit(msg)

	if m.messageQueue.FollowUpCount() != 1 {
		t.Error("expected 1 follow-up message queued")
	}
}

func TestHandleKey_EnterWhileRunning_SubmitsSteering(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.agentCh = make(chan agentMsg, 64)
	m.steeringNotify = make(chan struct{}, 1)

	// Type some text.
	m.inputModel.Text = "steer this"
	m.inputModel.CursorPos = 10

	// Press Enter while running.
	cmd := m.inputModel.HandleKey(makeKey(tea.KeyEnter), true)
	if cmd == nil {
		t.Fatal("expected a command from steering submit")
	}
	msg := cmd()
	_, ok := msg.(SteeringSubmitMsg)
	if !ok {
		t.Errorf("expected SteeringSubmitMsg, got %T", msg)
	}
}

func TestHandleKey_AltEnterWhileRunning_SubmitsFollowUp(t *testing.T) {
	m := newTestModel(t)
	m.running = true

	m.inputModel.Text = "follow up later"
	m.inputModel.CursorPos = 15

	// Press Alt+Enter while running.
	altEnter := makeKey(tea.KeyEnter)
	altEnter.Mod = altEnter.Mod | tea.ModAlt
	cmd := m.inputModel.HandleKey(altEnter, true)
	if cmd == nil {
		t.Fatal("expected a command from follow-up submit")
	}
	msg := cmd()
	_, ok := msg.(FollowUpSubmitMsg)
	if !ok {
		t.Errorf("expected FollowUpSubmitMsg, got %T", msg)
	}
}

func TestHandleKey_EnterWhileIdle_SubmitsNormally(t *testing.T) {
	m := newTestModel(t)
	m.inputModel.Text = "normal prompt"
	m.inputModel.CursorPos = 13

	cmd := m.inputModel.HandleKey(makeKey(tea.KeyEnter), false)
	if cmd == nil {
		t.Fatal("expected a command from normal submit")
	}
	msg := cmd()
	_, ok := msg.(InputSubmitMsg)
	if !ok {
		t.Errorf("expected InputSubmitMsg, got %T", msg)
	}
}

func TestQueueIndicator_Empty(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	indicator := m.renderQueueIndicator()
	if indicator != "" {
		t.Errorf("expected empty indicator, got %q", indicator)
	}
}

func TestQueueIndicator_WithMessages(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.messageQueue.QueueSteering("s1", nil)
	m.messageQueue.QueueFollowUp("f1", nil)
	m.messageQueue.QueueFollowUp("f2", nil)

	indicator := m.renderQueueIndicator()
	if !contains(indicator, "1 steering") {
		t.Errorf("expected steering count in indicator, got %q", indicator)
	}
	if !contains(indicator, "2 follow-up") {
		t.Errorf("expected follow-up count in indicator, got %q", indicator)
	}
}

func TestInputView_ShowsSteeringHint_WhenRunningAndEmpty(t *testing.T) {
	im := &InputModel{}
	out := im.View(true)
	if !contains(out, "steer") {
		t.Errorf("expected steering hint, got %q", out)
	}
	if !contains(out, "follow up") {
		t.Errorf("expected follow-up hint, got %q", out)
	}
}

func TestInputView_ShowsNormalInput_WhenRunningWithText(t *testing.T) {
	im := &InputModel{Text: "some text", CursorPos: 9}
	out := im.View(true)
	if contains(out, "steer") {
		t.Errorf("should not show steering hint when text is present, got %q", out)
	}
}

func TestCancelAgent_ClearsQueue(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.agentCh = make(chan agentMsg, 64)
	m.steeringNotify = make(chan struct{}, 1)
	m.messageQueue.QueueSteering("s1", nil)
	m.messageQueue.QueueFollowUp("f1", nil)

	// Drain channel to prevent goroutine leak.
	ch := m.agentCh
	go func() { close(ch) }()

	m.cancelAgent()

	if m.messageQueue.HasSteering() || m.messageQueue.HasFollowUp() {
		t.Error("expected queue cleared after cancel")
	}
	if m.steeringNotify != nil {
		t.Error("expected steeringNotify cleared after cancel")
	}
}
