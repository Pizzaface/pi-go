package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

// InboundRouter routes inbound JSON-RPC methods other than the handshake to
// an extension-specific handler (typically *api.HostedAPIHandler.Handle).
type InboundRouter func(method string, params json.RawMessage) (any, error)

// LaunchHosted starts the hosted extension subprocess described by command,
// pipes stdin/stdout, wraps the process in an RPC connection, and services
// the initial handshake. The registration transitions to StateRunning only
// when the handshake succeeds; if the process exits before that, the state
// is set to StateErrored and the tail of stderr is attached.
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
	if reg.WorkDir != "" {
		cmd.Dir = reg.WorkDir
	}
	cmd.Env = buildChildEnv(reg, command, os.Environ())

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
	stderrBuf := newStderrRing(8 * 1024)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		manager.SetState(reg.ID, StateErrored, err)
		return fmt.Errorf("launch: start %s: %w", command[0], err)
	}

	handler := func(method string, params json.RawMessage) (any, error) {
		if method == hostproto.MethodHandshake {
			resp, herr := BuildHandshakeResponse(reg, manager, params)
			if herr == nil {
				manager.SetState(reg.ID, StateRunning, nil)
			}
			return resp, herr
		}
		if router == nil {
			return nil, fmt.Errorf("launch: no router for method %q", method)
		}
		return router(method, params)
	}

	conn := NewRPCConn(stdout, stdin, handler)
	reg.Conn = conn

	go watchProcessExit(cmd, reg, manager, conn, stderrBuf)
	return nil
}

// watchProcessExit waits for the subprocess to exit and reconciles state.
// If the process exits before reaching StateRunning, the registration is
// marked StateErrored and stderr is attached. If it exits after the
// handshake, any StateRunning registration flips to StateErrored so the
// UI reflects the crash; StateStopped (set by a clean shutdown) is left
// alone.
func watchProcessExit(cmd *exec.Cmd, reg *Registration, manager *Manager, conn *RPCConn, stderrBuf *stderrRing) {
	waitErr := cmd.Wait()
	cur := manager.Get(reg.ID)
	if cur == nil {
		conn.Close()
		return
	}
	switch cur.State {
	case StateStopped:
		// Clean shutdown already recorded.
	case StateRunning:
		// Crashed after handshake.
		manager.SetState(reg.ID, StateErrored, buildExitError("extension exited while running", waitErr, stderrBuf))
	default:
		// Exited before handshake — startup failure.
		manager.SetState(reg.ID, StateErrored, buildExitError("extension exited before handshake", waitErr, stderrBuf))
	}
	conn.Close()
}

func buildExitError(prefix string, waitErr error, stderrBuf *stderrRing) error {
	msg := prefix
	if waitErr != nil {
		msg = fmt.Sprintf("%s: %v", msg, waitErr)
	}
	if tail := stderrBuf.String(); tail != "" {
		msg = fmt.Sprintf("%s\nstderr:\n%s", msg, strings.TrimRight(tail, "\n"))
	}
	return errors.New(msg)
}

// buildChildEnv composes the child process environment. For hosted-go
// extensions invoked via `go`, GOWORK=off is injected (unless the caller
// already set GOWORK) so the child build is isolated from the host
// project's go.work workspace file. GOROOT and GOTOOLCHAIN are also
// stripped so the `go` binary on PATH uses the stdlib it was shipped with
// — otherwise a go-pi binary built under a newer toolchain leaks its
// GOROOT to an older child `go` and compilation aborts with
// "version go1.X does not match go tool version go1.Y".
func buildChildEnv(reg *Registration, command []string, parentEnv []string) []string {
	if reg == nil || reg.Mode != "hosted-go" || len(command) == 0 || !isGoInvocation(command[0]) {
		return append([]string(nil), parentEnv...)
	}
	env := make([]string, 0, len(parentEnv)+1)
	for _, e := range parentEnv {
		if strings.HasPrefix(e, "GOROOT=") || strings.HasPrefix(e, "GOTOOLCHAIN=") {
			continue
		}
		env = append(env, e)
	}
	if !envHasKey(env, "GOWORK") {
		env = append(env, "GOWORK=off")
	}
	return env
}

// isGoInvocation reports whether cmd is the Go toolchain entry point.
// Matches "go" and "go.exe", bare or path-qualified.
func isGoInvocation(cmd string) bool {
	base := cmd
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(base)
	return base == "go" || base == "go.exe"
}

// envHasKey reports whether env contains an entry for key (case-sensitive
// on Unix, case-insensitive on Windows — we keep it simple and treat
// uppercase key names as-is, which matches every caller today).
func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// stderrRing is a fixed-capacity ring buffer that keeps only the most
// recent max bytes written to it. Safe for concurrent writes.
type stderrRing struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func newStderrRing(max int) *stderrRing { return &stderrRing{max: max} }

func (r *stderrRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf.Write(p)
	if over := r.buf.Len() - r.max; over > 0 {
		tail := r.buf.Bytes()[over:]
		cp := make([]byte, len(tail))
		copy(cp, tail)
		r.buf.Reset()
		r.buf.Write(cp)
	}
	return len(p), nil
}

func (r *stderrRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// BuildHandshakeResponse constructs the host's reply to an inbound handshake
// request. It rejects extensions whose protocol_version differs from
// hostproto.ProtocolVersion and reports the granted services derived from
// the capability gate. Exported so tests (and future callers that wire the
// handshake through a custom RPC path) can drive the builder directly.
func BuildHandshakeResponse(reg *Registration, manager *Manager, params json.RawMessage) (any, error) {
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

// HandshakeErrorCode extracts the JSON-RPC error code from an error
// returned by BuildHandshakeResponse. Returns (0, "") for non-handshake
// errors.
func HandshakeErrorCode(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	if e, ok := err.(*hostprotoError); ok {
		return e.Code, e.Message
	}
	return 0, err.Error()
}
