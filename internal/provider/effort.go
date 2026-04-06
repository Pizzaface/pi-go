package provider

import (
	"strings"

	"github.com/openai/openai-go/v3/shared"
)

// EffortLevel represents a provider-agnostic reasoning/thinking effort level.
// It maps to each provider's native effort mechanism.
type EffortLevel int

const (
	// EffortNone disables extended thinking/reasoning.
	EffortNone EffortLevel = iota
	// EffortLow uses minimal thinking/reasoning budget.
	EffortLow
	// EffortMedium uses moderate thinking/reasoning budget (default).
	EffortMedium
	// EffortHigh uses maximum thinking/reasoning budget.
	EffortHigh
)

// String returns the human-readable name for the effort level.
func (e EffortLevel) String() string {
	switch e {
	case EffortNone:
		return "none"
	case EffortLow:
		return "low"
	case EffortMedium:
		return "medium"
	case EffortHigh:
		return "high"
	default:
		return "medium"
	}
}

// ParseEffortLevel parses a string into an EffortLevel.
// Accepts "none", "low", "medium", "high" (case-insensitive).
// Returns EffortMedium for unrecognized values.
func ParseEffortLevel(s string) EffortLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "off", "disabled":
		return EffortNone
	case "low", "min", "minimal":
		return EffortLow
	case "medium", "med", "default":
		return EffortMedium
	case "high", "max":
		return EffortHigh
	default:
		return EffortMedium
	}
}

// AllEffortLevels returns all valid effort levels in order.
func AllEffortLevels() []EffortLevel {
	return []EffortLevel{EffortNone, EffortLow, EffortMedium, EffortHigh}
}

// NextEffortLevel cycles to the next effort level, wrapping around.
func (e EffortLevel) Next() EffortLevel {
	return (e + 1) % (EffortHigh + 1)
}

// --- Provider-specific mappings ---

// AnthropicThinkingBudget returns the Anthropic thinking budget in tokens
// for the given effort level. Returns 0 for EffortNone (disabled).
func (e EffortLevel) AnthropicThinkingBudget() int64 {
	switch e {
	case EffortLow:
		return 2048
	case EffortMedium:
		return 4096
	case EffortHigh:
		return 8192
	default:
		return 0
	}
}

// OpenAIReasoningEffort returns the OpenAI reasoning_effort value
// for the given effort level. Returns empty for EffortNone.
func (e EffortLevel) OpenAIReasoningEffort() shared.ReasoningEffort {
	switch e {
	case EffortLow:
		return shared.ReasoningEffortLow
	case EffortMedium:
		return shared.ReasoningEffortMedium
	case EffortHigh:
		return shared.ReasoningEffortHigh
	default:
		return ""
	}
}

// OllamaThinkingValue returns the Ollama think value string
// for the given effort level. Returns "" for EffortNone.
func (e EffortLevel) OllamaThinkingValue() string {
	switch e {
	case EffortLow:
		return "low"
	case EffortMedium:
		return "medium"
	case EffortHigh:
		return "high"
	default:
		return ""
	}
}
