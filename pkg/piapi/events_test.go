package piapi

import (
	"encoding/json"
	"testing"
)

func TestSessionStartEvent_Name(t *testing.T) {
	evt := SessionStartEvent{Reason: "startup"}
	if evt.EventName() != EventSessionStart {
		t.Fatalf("EventName() = %q, want %q", evt.EventName(), EventSessionStart)
	}
}

func TestEventResult_MarshalControl(t *testing.T) {
	cases := []struct {
		name    string
		result  EventResult
		wantKey string
	}{
		{"nil", EventResult{}, ""},
		{"cancel", EventResult{Control: &EventControl{Cancel: true}}, "cancel"},
		{"block", EventResult{Control: &EventControl{Block: true, Reason: "nope"}}, "block"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.result)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantKey != "" && !contains(string(b), tc.wantKey) {
				t.Fatalf("Marshal(%+v) = %s; expected %q key", tc.result, b, tc.wantKey)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
