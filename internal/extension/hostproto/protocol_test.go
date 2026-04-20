package hostproto

import (
	"encoding/json"
	"testing"
)

func TestHandshakeRequest_RoundTrip(t *testing.T) {
	req := HandshakeRequest{
		ProtocolVersion:  ProtocolVersion,
		ExtensionID:      "hello",
		ExtensionVersion: "0.1.0",
		RequestedServices: []RequestedService{
			{Service: "tools", Version: 1, Methods: []string{"register"}},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var round HandshakeRequest
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round.ProtocolVersion != "2.2" {
		t.Fatalf("protocol_version lost in round trip: %q", round.ProtocolVersion)
	}
	if len(round.RequestedServices) != 1 {
		t.Fatalf("services lost")
	}
}

func TestSpec5Methods(t *testing.T) {
	cases := map[string]string{
		"session.append_entry":        ServiceSession + "." + MethodSessionAppendEntry,
		"session.send_custom_message": ServiceSession + "." + MethodSessionSendCustomMessage,
		"session.send_user_message":   ServiceSession + "." + MethodSessionSendUserMessage,
		"session.set_title":           ServiceSession + "." + MethodSessionSetTitle,
		"session.get_title":           ServiceSession + "." + MethodSessionGetTitle,
		"session.set_entry_label":     ServiceSession + "." + MethodSessionSetEntryLabel,
		"session_control.wait_idle":   ServiceSessionControl + "." + MethodSessionControlWaitIdle,
		"session_control.new":         ServiceSessionControl + "." + MethodSessionControlNew,
		"session_control.fork":        ServiceSessionControl + "." + MethodSessionControlFork,
		"session_control.navigate":    ServiceSessionControl + "." + MethodSessionControlNavigate,
		"session_control.switch":      ServiceSessionControl + "." + MethodSessionControlSwitch,
		"session_control.reload":      ServiceSessionControl + "." + MethodSessionControlReload,
		"tool_stream.update":          ServiceToolStream + "." + MethodToolStreamUpdate,
		"log.append":                  ServiceLog + "." + MethodLogAppend,
	}
	for want, got := range cases {
		if want != got {
			t.Errorf("method key %q got %q", want, got)
		}
	}
}

func TestLogParamsRoundtrip(t *testing.T) {
	in := LogParams{
		Level:   "info",
		Message: "hello",
		Fields:  map[string]any{"k": "v"},
		Ts:      "2026-04-17T00:00:00Z",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out LogParams
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Level != in.Level || out.Message != in.Message || out.Fields["k"] != "v" || out.Ts != in.Ts {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}
