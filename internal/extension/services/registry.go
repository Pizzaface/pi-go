package services

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// CapabilityGate decides whether an extension is allowed to call a
// particular service method. Implementations typically consult
// approvals.json through the existing extension.Permissions type.
type CapabilityGate interface {
	Allowed(extensionID, service, method string) bool
}

// Registry is the host-side dispatch table for v2 extension services.
// A Registry is safe for concurrent Dispatch calls once all Register
// calls are complete.
type Registry struct {
	mu       sync.RWMutex
	services map[string]Service
	gate     CapabilityGate
}

// NewRegistry constructs a Registry with the given capability gate.
// Passing nil is allowed in tests and disables the gate entirely.
func NewRegistry(gate CapabilityGate) *Registry {
	return &Registry{
		services: make(map[string]Service),
		gate:     gate,
	}
}

// Register adds a service. Duplicate names return an error; services
// are added during startup before any Dispatch call runs.
func (r *Registry) Register(svc Service) error {
	if svc == nil {
		return fmt.Errorf("services: Register called with nil")
	}
	name := svc.Name()
	if name == "" {
		return fmt.Errorf("services: service name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.services[name]; exists {
		return fmt.Errorf("services: duplicate service %q", name)
	}
	r.services[name] = svc
	return nil
}

// Dispatch looks up the service, enforces version and capability gates,
// and forwards the call. All failures are returned as *RPCError so the
// caller can serialize them directly into a JSON-RPC response.
func (r *Registry) Dispatch(extensionID string, params hostproto.HostCallParams, sess *SessionContext) (json.RawMessage, error) {
	r.mu.RLock()
	svc, ok := r.services[params.Service]
	r.mu.RUnlock()
	if !ok {
		return nil, NewRPCError(hostproto.ErrCodeServiceUnsupported, fmt.Sprintf("unknown service %q", params.Service))
	}
	if params.Version > svc.Version() {
		return nil, NewRPCError(hostproto.ErrCodeServiceUnsupported, fmt.Sprintf("service %q version %d unavailable (host supports %d)", params.Service, params.Version, svc.Version()))
	}
	if !methodSupported(svc, params.Method) {
		return nil, NewRPCError(hostproto.ErrCodeMethodNotFound, fmt.Sprintf("service %q has no method %q", params.Service, params.Method))
	}
	if r.gate != nil && !r.gate.Allowed(extensionID, params.Service, params.Method) {
		return nil, NewRPCError(hostproto.ErrCodeCapabilityDenied, fmt.Sprintf("extension %q is not granted %s.%s", extensionID, params.Service, params.Method))
	}
	call := Call{
		ExtensionID: extensionID,
		Method:      params.Method,
		Version:     params.Version,
		Payload:     params.Payload,
		Session:     sess,
	}
	result, err := svc.Dispatch(call)
	if err != nil {
		return nil, ToRPCError(err)
	}
	return result, nil
}

// HostServices returns the service catalog sent to extensions in the
// handshake response. Sorted by service name for determinism.
func (r *Registry) HostServices() []hostproto.HostServiceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]hostproto.HostServiceInfo, 0, len(r.services))
	for _, svc := range r.services {
		out = append(out, hostproto.HostServiceInfo{
			Service: svc.Name(),
			Version: svc.Version(),
			Methods: append([]string(nil), svc.Methods()...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

func methodSupported(svc Service, method string) bool {
	return slices.Contains(svc.Methods(), method)
}
