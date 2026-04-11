package commands

import (
	"encoding/json"
	"strings"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/extension/services"
)

// Service implements the "commands" v2 service.
type Service struct {
	sink Sink
}

// New constructs the commands service.
func New(sink Sink) *Service {
	return &Service{sink: sink}
}

// Name returns the service name.
func (s *Service) Name() string { return "commands" }

// Version returns the current service version.
func (s *Service) Version() int { return 1 }

// Methods returns the supported methods.
func (s *Service) Methods() []string { return []string{"register", "unregister"} }

// Dispatch handles a single host_call.
func (s *Service) Dispatch(call services.Call) (json.RawMessage, error) {
	switch call.Method {
	case "register":
		return s.handleRegister(call)
	case "unregister":
		return s.handleUnregister(call)
	}
	return nil, services.NewRPCError(hostproto.ErrCodeMethodNotFound, "commands: unknown method "+call.Method)
}

func (s *Service) handleRegister(call services.Call) (json.RawMessage, error) {
	var payload RegisterPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.register: invalid payload: "+err.Error())
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.register: name is required")
	}
	kind := strings.TrimSpace(payload.Kind)
	if kind == "" {
		kind = "prompt"
	}
	if kind != "prompt" && kind != "callback" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.register: kind must be 'prompt' or 'callback'")
	}
	if err := s.sink.RegisterCommand(Registration{
		ExtensionID: call.ExtensionID,
		Name:        name,
		Description: payload.Description,
		Prompt:      payload.Prompt,
		Kind:        kind,
	}); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeServiceError, err.Error())
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func (s *Service) handleUnregister(call services.Call) (json.RawMessage, error) {
	var payload UnregisterPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.unregister: invalid payload: "+err.Error())
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "commands.unregister: name is required")
	}
	if err := s.sink.UnregisterCommand(UnregisterInput{
		ExtensionID: call.ExtensionID,
		Name:        name,
	}); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeServiceError, err.Error())
	}
	return json.RawMessage(`{"ok":true}`), nil
}
