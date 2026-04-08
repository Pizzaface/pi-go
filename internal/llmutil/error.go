package llmutil

import "strings"

// ResponseErrorText returns the most useful human-readable error text from an
// LLM response error code/message pair.
func ResponseErrorText(code, message string) string {
	message = strings.TrimSpace(message)
	code = strings.TrimSpace(code)

	switch {
	case message != "":
		return message
	case code != "":
		return code
	default:
		return "LLM error"
	}
}

// ResponseErrorDisplayText returns a user-facing error string suitable for
// rendering in chat UIs.
func ResponseErrorDisplayText(code, message string) string {
	text := ResponseErrorText(code, message)
	if strings.HasPrefix(strings.ToLower(text), "error:") {
		return text
	}
	return "Error: " + text
}
