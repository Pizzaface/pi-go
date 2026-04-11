// Package commands implements the v2 "commands" service: slash command
// registration and unregistration. Command invocation (ext_call
// on_invoke) is a Plan 2 concern.
package commands

// RegisterPayload is the body of a commands.register host_call.
type RegisterPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Kind        string `json:"kind,omitempty"` // "prompt" (default) or "callback"
}

// UnregisterPayload is the body of a commands.unregister host_call.
type UnregisterPayload struct {
	Name string `json:"name"`
}

// Registration is the host-side representation of a registered command,
// handed to the Sink after validation.
type Registration struct {
	ExtensionID string
	Name        string
	Description string
	Prompt      string
	Kind        string
}

// UnregisterInput is what Sink.UnregisterCommand receives.
type UnregisterInput struct {
	ExtensionID string
	Name        string
}

// Sink is the bridge between the service and the manager's existing
// command registry.
type Sink interface {
	RegisterCommand(reg Registration) error
	UnregisterCommand(input UnregisterInput) error
}
