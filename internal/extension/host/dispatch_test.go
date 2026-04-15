package host

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dimetron/pi-go/pkg/piapi"
)

func TestDispatcher_NoSubscribers(t *testing.T) {
	d := NewDispatcher()
	res := d.Dispatch(context.Background(), "session_start", nil)
	if res.Cancelled || res.Blocked {
		t.Fatalf("empty dispatch should be zero-valued; got %+v", res)
	}
}

func TestDispatcher_CancelAggregation(t *testing.T) {
	d := NewDispatcher()
	d.Subscribe("x", Subscriber{
		ExtensionID: "a",
		Call: func(context.Context, json.RawMessage) (piapi.EventResult, error) {
			return piapi.EventResult{}, nil
		},
	})
	d.Subscribe("x", Subscriber{
		ExtensionID: "b",
		Call: func(context.Context, json.RawMessage) (piapi.EventResult, error) {
			return piapi.EventResult{Control: &piapi.EventControl{Cancel: true, Reason: "b says no"}}, nil
		},
	})
	res := d.Dispatch(context.Background(), "x", nil)
	if !res.Cancelled {
		t.Fatalf("expected cancelled; got %+v", res)
	}
	if res.Reason != "b says no" {
		t.Fatalf("expected reason from cancelling sub; got %q", res.Reason)
	}
}

func TestDispatcher_BlockFirstWins(t *testing.T) {
	d := NewDispatcher()
	d.Subscribe("x", Subscriber{
		ExtensionID: "first",
		Call: func(context.Context, json.RawMessage) (piapi.EventResult, error) {
			return piapi.EventResult{Control: &piapi.EventControl{Block: true, Reason: "first"}}, nil
		},
	})
	d.Subscribe("x", Subscriber{
		ExtensionID: "second",
		Call: func(context.Context, json.RawMessage) (piapi.EventResult, error) {
			return piapi.EventResult{Control: &piapi.EventControl{Block: true, Reason: "second"}}, nil
		},
	})
	res := d.Dispatch(context.Background(), "x", nil)
	if !res.Blocked {
		t.Fatal("expected blocked")
	}
	if res.Reason != "first" && res.Reason != "second" {
		t.Fatalf("expected reason from one of the blockers; got %q", res.Reason)
	}
}

func TestDispatcher_Unsubscribe(t *testing.T) {
	d := NewDispatcher()
	d.Subscribe("x", Subscriber{
		ExtensionID: "a",
		Call: func(context.Context, json.RawMessage) (piapi.EventResult, error) {
			return piapi.EventResult{Control: &piapi.EventControl{Cancel: true, Reason: "a"}}, nil
		},
	})
	d.Unsubscribe("a")
	res := d.Dispatch(context.Background(), "x", nil)
	if res.Cancelled {
		t.Fatal("unsubscribed handler should not run")
	}
}
