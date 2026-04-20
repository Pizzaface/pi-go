package host

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

// Subscriber is one extension's handler for a single event.
type Subscriber struct {
	ExtensionID string
	Call        func(ctx context.Context, payload json.RawMessage) (piapi.EventResult, error)
}

// DispatchResult is the aggregated outcome of dispatching an event to all
// its subscribers.
type DispatchResult struct {
	Cancelled bool
	Blocked   bool
	Reason    string
	Transform *piapi.ToolResult
	PerSub    []SubscriberResult
}

// SubscriberResult is one subscriber's outcome.
type SubscriberResult struct {
	ExtensionID string
	Result      piapi.EventResult
	Err         error
}

// Dispatcher fans an event out to all subscribers in parallel and
// aggregates the per-subscriber results into a DispatchResult.
type Dispatcher struct {
	mu   sync.RWMutex
	subs map[string][]Subscriber
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{subs: map[string][]Subscriber{}}
}

// Subscribe registers a subscriber for an event.
func (d *Dispatcher) Subscribe(event string, s Subscriber) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subs[event] = append(d.subs[event], s)
}

// Unsubscribe removes every subscription owned by the given extension.
func (d *Dispatcher) Unsubscribe(extensionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ev, list := range d.subs {
		out := list[:0]
		for _, s := range list {
			if s.ExtensionID != extensionID {
				out = append(out, s)
			}
		}
		d.subs[ev] = out
	}
}

// Dispatch fans an event out to every subscriber with a per-handler 30s
// timeout and aggregates the results.
func (d *Dispatcher) Dispatch(ctx context.Context, event string, payload json.RawMessage) DispatchResult {
	d.mu.RLock()
	subs := append([]Subscriber(nil), d.subs[event]...)
	d.mu.RUnlock()

	if len(subs) == 0 {
		return DispatchResult{}
	}

	results := make([]SubscriberResult, len(subs))
	var wg sync.WaitGroup
	for i, s := range subs {
		wg.Add(1)
		go func(i int, s Subscriber) {
			defer wg.Done()
			hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			r, err := s.Call(hctx, payload)
			results[i] = SubscriberResult{ExtensionID: s.ExtensionID, Result: r, Err: err}
		}(i, s)
	}
	wg.Wait()
	return aggregate(results)
}

// aggregate applies spec §5 rules: cancel wins overall, first block wins
// the reason. Transform composition is deferred to spec #3.
func aggregate(results []SubscriberResult) DispatchResult {
	agg := DispatchResult{PerSub: results}
	for _, r := range results {
		if r.Err != nil || r.Result.Control == nil {
			continue
		}
		ctrl := r.Result.Control
		if ctrl.Cancel {
			agg.Cancelled = true
			if agg.Reason == "" {
				agg.Reason = ctrl.Reason
			}
		}
		if ctrl.Block && !agg.Blocked {
			agg.Blocked = true
			agg.Reason = ctrl.Reason
		}
	}
	return agg
}
