package llmutil

import "testing"

func TestResponseErrorText(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		message string
		want    string
	}{
		{name: "message preferred", code: "STREAM_ERROR", message: "connection lost", want: "connection lost"},
		{name: "falls back to code", code: "API_ERROR", message: "", want: "API_ERROR"},
		{name: "default fallback", code: "", message: "", want: "LLM error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResponseErrorText(tt.code, tt.message); got != tt.want {
				t.Fatalf("ResponseErrorText(%q, %q) = %q, want %q", tt.code, tt.message, got, tt.want)
			}
		})
	}
}

func TestResponseErrorDisplayText(t *testing.T) {
	if got := ResponseErrorDisplayText("STREAM_ERROR", "connection lost"); got != "Error: connection lost" {
		t.Fatalf("unexpected display text: %q", got)
	}
	if got := ResponseErrorDisplayText("", "Error: already formatted"); got != "Error: already formatted" {
		t.Fatalf("expected existing prefix to be preserved, got %q", got)
	}
}
