package hostproto

import "testing"

func TestProtocolVersion_2_2(t *testing.T) {
	if ProtocolVersion != "2.2" {
		t.Fatalf("ProtocolVersion = %q, want %q", ProtocolVersion, "2.2")
	}
}

func TestNewServiceConstants(t *testing.T) {
	cases := map[string]string{
		"state":    ServiceState,
		"commands": ServiceCommands,
		"ui":       ServiceUI,
		"sigils":   ServiceSigils,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("service %q: got %q", want, got)
		}
	}
}
