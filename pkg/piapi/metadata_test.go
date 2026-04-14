package piapi

import "testing"

func TestMetadata_Validate(t *testing.T) {
	cases := []struct {
		name    string
		meta    Metadata
		wantErr bool
	}{
		{"valid minimal", Metadata{Name: "hello", Version: "0.1.0"}, false},
		{"empty name", Metadata{Version: "0.1.0"}, true},
		{"invalid name chars", Metadata{Name: "has spaces", Version: "0.1.0"}, true},
		{"empty version", Metadata{Name: "hello"}, true},
		{"dotted capability", Metadata{Name: "h", Version: "0.1.0", RequestedCapabilities: []string{"tools.register"}}, false},
		{"malformed capability", Metadata{Name: "h", Version: "0.1.0", RequestedCapabilities: []string{"no_dot"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.meta.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
