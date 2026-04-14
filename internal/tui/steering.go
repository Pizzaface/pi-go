package tui

import "sync"

// MessageDelivery specifies when a queued message should be delivered.
type MessageDelivery int

const (
	// DeliverSteer delivers after the current tool calls finish, before the
	// next LLM call — lets the user redirect the agent mid-stream.
	DeliverSteer MessageDelivery = iota

	// DeliverFollowUp delivers only after the agent finishes all work —
	// effectively queues another prompt.
	DeliverFollowUp
)

// QueuedMessage is a user message waiting to be injected into the agent loop.
type QueuedMessage struct {
	Text     string
	Mentions []string
	Delivery MessageDelivery
}

// MessageQueue holds steering and follow-up messages submitted while the
// agent is running. It is safe for concurrent use.
type MessageQueue struct {
	mu        sync.Mutex
	steering  []QueuedMessage
	followUps []QueuedMessage
}

// QueueSteering adds a message that will be injected between tool rounds.
func (q *MessageQueue) QueueSteering(text string, mentions []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = append(q.steering, QueuedMessage{
		Text:     text,
		Mentions: mentions,
		Delivery: DeliverSteer,
	})
}

// QueueFollowUp adds a message that will be sent after the agent finishes.
func (q *MessageQueue) QueueFollowUp(text string, mentions []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.followUps = append(q.followUps, QueuedMessage{
		Text:     text,
		Mentions: mentions,
		Delivery: DeliverFollowUp,
	})
}

// DrainSteering returns and removes all pending steering messages.
func (q *MessageQueue) DrainSteering() []QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	msgs := q.steering
	q.steering = nil
	return msgs
}

// DrainOneFollowUp returns and removes the next follow-up message, if any.
func (q *MessageQueue) DrainOneFollowUp() (QueuedMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.followUps) == 0 {
		return QueuedMessage{}, false
	}
	msg := q.followUps[0]
	q.followUps = q.followUps[1:]
	return msg, true
}

// SteeringCount returns the number of pending steering messages.
func (q *MessageQueue) SteeringCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steering)
}

// FollowUpCount returns the number of pending follow-up messages.
func (q *MessageQueue) FollowUpCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.followUps)
}

// Clear removes all queued messages.
func (q *MessageQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = nil
	q.followUps = nil
}

// ClearSteering removes only pending steering messages, preserving follow-ups.
func (q *MessageQueue) ClearSteering() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = nil
}

// HasSteering returns true if there are pending steering messages.
func (q *MessageQueue) HasSteering() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steering) > 0
}

// HasFollowUp returns true if there are pending follow-up messages.
func (q *MessageQueue) HasFollowUp() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.followUps) > 0
}
