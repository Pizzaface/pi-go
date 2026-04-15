package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// InboundRouter routes inbound JSON-RPC methods other than the handshake to
// an extension-specific handler (typically *api.HostedAPIHandler.Handle).
type InboundRouter func(method string, params json.RawMessage) (any, error)

// LaunchHosted starts the hosted extension subprocess described by command,
// pipes stdin/stdout, wraps the process in an RPC connection, services the
// initial handshake, and transitions the registration to StateRunning.
//
// router handles every inbound JSON-RPC method other than
// hostproto.MethodHandshake — typically (*api.HostedAPIHandler).Handle.
// Pass nil to reject every non-handshake method.
func LaunchHosted(ctx context.Context, reg *Registration, manager *Manager, command []string, router InboundRouter) error {
	if reg == nil {
		return fmt.Errorf("launch: registration is required")
	}
	if manager == nil {
		return fmt.Errorf("launch: manager is required")
	}
	if len(command) == 0 {
		return fmt.Errorf("launch: command is required")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		manager.SetState(reg.ID, StateErrored, err)
		return fmt.Errorf("launch: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		manager.SetState(reg.ID, StateErrored, err)
		return fmt.Errorf("launch: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		manager.SetState(reg.ID, StateErrored, err)
		return fmt.Errorf("launch: start %s: %w", command[0], err)
	}

	handler := func(method string, params json.RawMessage) (any, error) {
		if method == hostproto.MethodHandshake {
			return buildHandshakeResponse(reg, manager, params)
		}
		if router == nil {
			return nil, fmt.Errorf("launch: no router for method %q", method)
		}
		return router(method, params)
	}

	conn := NewRPCConn(stdout, stdin, handler)
	reg.Conn = conn
	manager.SetState(reg.ID, StateRunning, nil)
	return nil
}

// buildHandshakeResponse constructs the host's reply to an inbound handshake
// request. It rejects extensions whose protocol_version differs from
// hostproto.ProtocolVersion and reports the granted services derived from
// the capability gate.
func buildHandshakeResponse(reg *Registration, manager *Manager, params json.RawMessage) (any, error) {
	var req hostproto.HandshakeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &hostprotoError{
			Code:    hostproto.ErrCodeHandshakeFailed,
			Message: fmt.Sprintf("handshake: invalid params: %v", err),
		}
	}
	if req.ProtocolVersion != hostproto.ProtocolVersion {
		return nil, &hostprotoError{
			Code:    hostproto.ErrCodeHandshakeFailed,
			Message: fmt.Sprintf("handshake: unsupported protocol_version %q (host speaks %s)", req.ProtocolVersion, hostproto.ProtocolVersion),
		}
	}

	grants := manager.Gate().Grants(reg.ID, reg.Trust)
	granted := groupGrantsByService(req.RequestedServices, grants, reg.Trust)
	return hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		GrantedServices: granted,
		HostServices:    nil,
		DispatchableEvents: []hostproto.DispatchableEvent{
			{Name: "session_start", Version: 1},
		},
	}, nil
}

// groupGrantsByService intersects the requested services with the
// capability gate's grants. Compiled-in extensions get every requested
// method back; hosted extensions get only methods whose "service.method"
// capability appears in the grants list.
func groupGrantsByService(requested []hostproto.RequestedService, grants []string, trust TrustClass) []hostproto.GrantedService {
	if len(requested) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	starAll := false
	for _, g := range grants {
		if g == StarAll {
			starAll = true
			continue
		}
		allowed[g] = true
	}
	out := make([]hostproto.GrantedService, 0, len(requested))
	for _, svc := range requested {
		methods := make([]string, 0, len(svc.Methods))
		for _, m := range svc.Methods {
			cap := svc.Service + "." + m
			if starAll || trust == TrustCompiledIn || allowed[cap] {
				methods = append(methods, m)
			}
		}
		gs := hostproto.GrantedService{
			Service: svc.Service,
			Version: svc.Version,
			Methods: methods,
		}
		if len(methods) == 0 {
			gs.DeniedReason = "no capabilities granted"
		}
		out = append(out, gs)
	}
	return out
}

// hostprotoError is a typed error so the RPC layer can extract the code.
// (The current rpc.go uses ErrCodeServiceUnsupported for handler errors;
// the handshake-failed code propagates only by message text for now —
// the protocol-downgrade test in Task 45 asserts on the message.)
type hostprotoError struct {
	Code    int
	Message string
}

func (e *hostprotoError) Error() string { return e.Message }
