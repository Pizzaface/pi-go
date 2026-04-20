package piext

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/pizzaface/go-pi/pkg/piapi"
)

const protocolVersion = "2.1"
const handshakeTimeout = 5 * time.Second

// Run is the entrypoint for a hosted-Go extension. It performs the
// handshake, instantiates a piapi.API backed by stdio JSON-RPC, and
// invokes the user's register callback. Blocks until the host sends
// pi.extension/shutdown.
func Run(metadata piapi.Metadata, register piapi.Register) error {
	return runInternal(stdinReadCloser{}, stdoutWriteCloser{}, metadata, register)
}

func runInternal(in io.ReadCloser, out io.WriteCloser, metadata piapi.Metadata, register piapi.Register) error {
	if err := metadata.Validate(); err != nil {
		return err
	}
	transport := newTransport(in, out)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancel()

	requested := make([]map[string]any, 0, len(metadata.RequestedCapabilities))
	seen := map[string]map[string]any{}
	for _, c := range metadata.RequestedCapabilities {
		svc, method := splitCap(c)
		entry, ok := seen[svc]
		if !ok {
			entry = map[string]any{"service": svc, "version": 1, "methods": []string{}}
			seen[svc] = entry
			requested = append(requested, entry)
		}
		entry["methods"] = append(entry["methods"].([]string), method)
	}

	var hsResp handshakeResponse
	err := transport.Call(ctx, "pi.extension/handshake", map[string]any{
		"protocol_version":   protocolVersion,
		"extension_id":       metadata.Name,
		"extension_version":  metadata.Version,
		"requested_services": requested,
	}, &hsResp)
	if err != nil {
		return err
	}
	if hsResp.ProtocolVersion != protocolVersion {
		return &handshakeError{got: hsResp.ProtocolVersion}
	}

	api := newRPCAPI(transport, metadata, hsResp.GrantedServices)
	SetLogWriter(transportLogWriter{api: api})
	defer SetLogWriter(nil)
	if err := register(api); err != nil {
		return err
	}

	shutdownCh := make(chan struct{})
	transport.HandleRequest("pi.extension/shutdown", func(_ context.Context, _ json.RawMessage) (any, error) {
		close(shutdownCh)
		return map[string]any{}, nil
	})

	<-shutdownCh
	return nil
}

type handshakeResponse struct {
	ProtocolVersion    string           `json:"protocol_version"`
	GrantedServices    []GrantedService `json:"granted_services"`
	HostServices       []GrantedService `json:"host_services"`
	DispatchableEvents []DispatchEvent  `json:"dispatchable_events"`
}

// GrantedService is the post-handshake view of what the host granted us.
type GrantedService struct {
	Service string   `json:"service"`
	Version int      `json:"version"`
	Methods []string `json:"methods"`
}

// DispatchEvent is an event the host is willing to dispatch to us.
type DispatchEvent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type handshakeError struct{ got string }

func (e *handshakeError) Error() string {
	return "piext: protocol version mismatch: host returned " + e.got
}

func splitCap(cap string) (service, method string) {
	for i := 0; i < len(cap); i++ {
		if cap[i] == '.' {
			return cap[:i], cap[i+1:]
		}
	}
	return cap, ""
}

// logWriterBox wraps an io.Writer so that atomic.Value always stores the
// same concrete type regardless of which writer is active.
type logWriterBox struct{ w io.Writer }

// currentLogWriter holds a *logWriterBox set by Run() once the transport is
// active. A nil load means no transport yet — fall back to stderr.
var currentLogWriter atomic.Value // stores *logWriterBox

// Log returns an io.Writer for hosted-extension logging. When a transport
// is active (set by Run()), each newline-terminated write becomes a
// log.append notification. Pre-handshake (transport nil) falls back to
// stderr so early startup messages aren't lost.
func Log() io.Writer {
	if v := currentLogWriter.Load(); v != nil {
		return v.(*logWriterBox).w
	}
	return os.Stderr
}

// SetLogWriter is called by Run() to swap Log() to a writer backed by
// the active transport. Pass nil to restore stderr behavior.
func SetLogWriter(w io.Writer) {
	if w == nil {
		currentLogWriter.Store(&logWriterBox{w: os.Stderr})
		return
	}
	currentLogWriter.Store(&logWriterBox{w: w})
}
