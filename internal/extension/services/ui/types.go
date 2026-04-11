// Package ui implements the v2 "ui" service: transient UI intents
// (status, clear_status) that extensions push to the TUI.
package ui

// StatusPayload is the body of a ui.status host_call.
type StatusPayload struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"` // optional color hint, ANSI name
}

// StatusEntry is what the service hands to its Sink after validation.
// It is the host-side representation of a status line entry.
type StatusEntry struct {
	ExtensionID string
	Text        string
	Color       string
}

// Sink is the bridge between the service and whatever actually renders
// status entries. The manager injects a Sink that forwards to the TUI.
type Sink interface {
	SetStatus(entry StatusEntry)
	ClearStatus(extensionID string)
}
