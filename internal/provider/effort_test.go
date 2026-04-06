package provider

import (
	"testing"

	"github.com/openai/openai-go/v3/shared"
)

func TestEffortLevelString(t *testing.T) {
	tests := []struct {
		level EffortLevel
		want  string
	}{
		{EffortNone, "none"},
		{EffortLow, "low"},
		{EffortMedium, "medium"},
		{EffortHigh, "high"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("EffortLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseEffortLevel(t *testing.T) {
	tests := []struct {
		input string
		want  EffortLevel
	}{
		{"none", EffortNone},
		{"off", EffortNone},
		{"disabled", EffortNone},
		{"low", EffortLow},
		{"min", EffortLow},
		{"minimal", EffortLow},
		{"medium", EffortMedium},
		{"med", EffortMedium},
		{"default", EffortMedium},
		{"high", EffortHigh},
		{"max", EffortHigh},
		{"HIGH", EffortHigh},
		{"  Medium  ", EffortMedium},
		{"unknown", EffortMedium}, // defaults to medium
		{"", EffortMedium},
	}
	for _, tt := range tests {
		if got := ParseEffortLevel(tt.input); got != tt.want {
			t.Errorf("ParseEffortLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestEffortLevelNext(t *testing.T) {
	tests := []struct {
		level EffortLevel
		want  EffortLevel
	}{
		{EffortNone, EffortLow},
		{EffortLow, EffortMedium},
		{EffortMedium, EffortHigh},
		{EffortHigh, EffortNone}, // wraps around
	}
	for _, tt := range tests {
		if got := tt.level.Next(); got != tt.want {
			t.Errorf("EffortLevel(%v).Next() = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestAnthropicThinkingBudget(t *testing.T) {
	tests := []struct {
		level EffortLevel
		want  int64
	}{
		{EffortNone, 0},
		{EffortLow, 2048},
		{EffortMedium, 4096},
		{EffortHigh, 8192},
	}
	for _, tt := range tests {
		if got := tt.level.AnthropicThinkingBudget(); got != tt.want {
			t.Errorf("EffortLevel(%v).AnthropicThinkingBudget() = %d, want %d", tt.level, got, tt.want)
		}
	}
}

func TestOpenAIReasoningEffort(t *testing.T) {
	tests := []struct {
		level EffortLevel
		want  shared.ReasoningEffort
	}{
		{EffortNone, ""},
		{EffortLow, shared.ReasoningEffortLow},
		{EffortMedium, shared.ReasoningEffortMedium},
		{EffortHigh, shared.ReasoningEffortHigh},
	}
	for _, tt := range tests {
		if got := tt.level.OpenAIReasoningEffort(); got != tt.want {
			t.Errorf("EffortLevel(%v).OpenAIReasoningEffort() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestOllamaThinkingValue(t *testing.T) {
	tests := []struct {
		level EffortLevel
		want  string
	}{
		{EffortNone, ""},
		{EffortLow, "low"},
		{EffortMedium, "medium"},
		{EffortHigh, "high"},
	}
	for _, tt := range tests {
		if got := tt.level.OllamaThinkingValue(); got != tt.want {
			t.Errorf("EffortLevel(%v).OllamaThinkingValue() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestAllEffortLevels(t *testing.T) {
	levels := AllEffortLevels()
	if len(levels) != 4 {
		t.Fatalf("AllEffortLevels() returned %d levels, want 4", len(levels))
	}
	if levels[0] != EffortNone || levels[1] != EffortLow || levels[2] != EffortMedium || levels[3] != EffortHigh {
		t.Errorf("AllEffortLevels() = %v, want [none, low, medium, high]", levels)
	}
}
