package ui

import (
	"encoding/json"
	"strings"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
	"github.com/dimetron/pi-go/internal/extension/services"
)

// Service implements the "ui" v2 service.
type Service struct {
	sink Sink
}

// New constructs the ui service with the given sink.
func New(sink Sink) *Service {
	return &Service{sink: sink}
}

// Name returns the service name.
func (s *Service) Name() string { return "ui" }

// Version returns the current service version.
func (s *Service) Version() int { return 1 }

// Methods returns the supported methods.
func (s *Service) Methods() []string { return []string{"status", "clear_status"} }

// Dispatch handles a single host_call.
func (s *Service) Dispatch(call services.Call) (json.RawMessage, error) {
	switch call.Method {
	case "status":
		return s.handleStatus(call)
	case "clear_status":
		return s.handleClearStatus(call)
	}
	return nil, services.NewRPCError(hostproto.ErrCodeMethodNotFound, "ui: unknown method "+call.Method)
}

func (s *Service) handleStatus(call services.Call) (json.RawMessage, error) {
	var payload StatusPayload
	if err := json.Unmarshal(call.Payload, &payload); err != nil {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "ui.status: invalid payload: "+err.Error())
	}
	if strings.TrimSpace(payload.Text) == "" {
		return nil, services.NewRPCError(hostproto.ErrCodeInvalidParams, "ui.status: text is required")
	}
	s.sink.SetStatus(StatusEntry{
		ExtensionID: call.ExtensionID,
		Text:        payload.Text,
		Color:       payload.Color,
	})
	return json.RawMessage(`{"ok":true}`), nil
}

func (s *Service) handleClearStatus(call services.Call) (json.RawMessage, error) {
	s.sink.ClearStatus(call.ExtensionID)
	return json.RawMessage(`{"ok":true}`), nil
}
