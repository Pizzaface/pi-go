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
	if round.ProtocolVersion != "2.1" {
		t.Fatalf("protocol_version lost in round trip: %q", round.ProtocolVersion)
	}
	if len(round.RequestedServices) != 1 {
		t.Fatalf("services lost")
	}
}
