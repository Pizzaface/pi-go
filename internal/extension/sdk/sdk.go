// Package sdk provides helpers for authoring v2 pi-go extensions in Go.
// It handles the stdio JSON-RPC plumbing so extension authors can write
// handlers instead of wire-protocol code.
//
// Typical usage:
//
//	func main() {
//	    client := sdk.NewClient(os.Stdin, os.Stdout)
//	    err := client.Serve(context.Background(), sdk.ServeOptions{
//	        ExtensionID: "my-ext",
//	        RequestedServices: []hostproto.ServiceRequest{...},
//	        OnReady: func(ready sdk.HandshakeReady) error { ... },
//	    })
//	    if err != nil { log.Fatal(err) }
//	}
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// Client is the extension-side SDK client. One per extension process.
type Client struct {
	in      io.ReadCloser
	out     io.Writer
	encoder *json.Encoder
	decoder *json.Decoder

	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResult

	// readDone is closed when readLoop exits (EOF, decode error, or
	// ctx cancellation). Serve watches this so it can return when the
	// host closes stdin, without needing a signal. Closing is guarded
	// by readOnce so the channel is only closed once even if readLoop
	// is restarted in tests.
	readDone chan struct{}
	readOnce sync.Once
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// NewClient constructs a Client reading from in and writing to out.
// Typical callers pass os.Stdin / os.Stdout.
func NewClient(in io.ReadCloser, out io.Writer) *Client {
	return &Client{
		in:       in,
		out:      out,
		encoder:  json.NewEncoder(out),
		decoder:  json.NewDecoder(in),
		pending:  make(map[int64]chan rpcResult),
		readDone: make(chan struct{}),
	}
}

// sendRequest writes an RPCRequest and returns the assigned id.
func (c *Client) sendRequest(method string, params any) (int64, error) {
	id := c.nextID.Add(1)
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return 0, fmt.Errorf("sdk: marshal params: %w", err)
		}
		raw = data
	}
	req := hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  raw,
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.encoder.Encode(req); err != nil {
		return 0, fmt.Errorf("sdk: encode request: %w", err)
	}
	return id, nil
}

// waitFor registers a pending-response channel for the given id.
func (c *Client) waitFor(id int64) chan rpcResult {
	ch := make(chan rpcResult, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	return ch
}

// resolvePending delivers a response to the waiting channel, if any.
func (c *Client) resolvePending(id int64, result rpcResult) {
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if ok {
		ch <- result
	}
}

// readLoop pumps incoming messages from in. Responses are routed to
// pending waiters; requests are dispatched to registered handlers
// (added in Task 10). Exits on EOF, decode error, or context
// cancellation. On exit, signals readDone so Serve can return.
func (c *Client) readLoop(ctx context.Context) {
	defer c.readOnce.Do(func() { close(c.readDone) })
	for {
		if ctx.Err() != nil {
			return
		}
		var msg json.RawMessage
		if err := c.decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
		// Distinguish request vs response by probing for a method field.
		var probe struct {
			ID     int64               `json:"id"`
			Method string              `json:"method"`
			Result json.RawMessage     `json:"result"`
			Error  *hostproto.RPCError `json:"error"`
		}
		if err := json.Unmarshal(msg, &probe); err != nil {
			continue
		}
		if probe.Method == "" {
			// Response path.
			if probe.Error != nil {
				c.resolvePending(probe.ID, rpcResult{err: fmt.Errorf("rpc error %d: %s", probe.Error.Code, probe.Error.Message)})
			} else {
				c.resolvePending(probe.ID, rpcResult{result: probe.Result})
			}
			continue
		}
		// Request path: dispatched in Task 10.
		_ = probe
	}
}

// Close releases the stdio handles.
func (c *Client) Close() error {
	if c.in != nil {
		_ = c.in.Close()
	}
	return nil
}

// HostCall issues a single host_call RPC and waits for the response.
// This is the primary way extensions invoke host services.
func (c *Client) HostCall(ctx context.Context, service, method string, version int, payload any) (json.RawMessage, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("sdk: HostCall marshal payload: %w", err)
	}
	params := hostproto.HostCallParams{
		Service: service,
		Method:  method,
		Version: version,
		Payload: payloadBytes,
	}
	id, err := c.sendRequest(hostproto.MethodHostCall, params)
	if err != nil {
		return nil, err
	}
	waiter := c.waitFor(id)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-waiter:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	}
}

// ServeOptions configures a Serve invocation.
type ServeOptions struct {
	ExtensionID       string
	Mode              string // typically "hosted_stdio"
	RequestedServices []hostproto.ServiceRequest
	// OnReady is called once the handshake completes successfully. Use
	// it to issue startup host_calls (e.g. commands.register). Returning
	// an error terminates Serve.
	OnReady func(HandshakeReady) error
}

// HandshakeReady is passed to ServeOptions.OnReady with the negotiated
// handshake result.
type HandshakeReady struct {
	Client   *Client
	Response hostproto.HandshakeResponse
}

// Serve runs the extension lifecycle:
//  1. Start the stdio read loop
//  2. Send the handshake
//  3. Wait for the handshake response
//  4. Invoke OnReady (if set) for startup registrations
//  5. Block until the host closes stdin or ctx is canceled
//
// Serve returns nil on clean shutdown, or an error if the handshake
// fails, the read loop errors, or OnReady returns an error.
func (c *Client) Serve(ctx context.Context, opts ServeOptions) error {
	if opts.ExtensionID == "" {
		return fmt.Errorf("sdk: ExtensionID is required")
	}
	if opts.Mode == "" {
		opts.Mode = "hosted_stdio"
	}

	go c.readLoop(ctx)

	handshakeReq := hostproto.HandshakeRequest{
		ProtocolVersion:   hostproto.ProtocolVersion,
		ExtensionID:       opts.ExtensionID,
		Mode:              opts.Mode,
		RequestedServices: opts.RequestedServices,
	}
	id, err := c.sendRequest(hostproto.MethodHandshake, handshakeReq)
	if err != nil {
		return err
	}
	waiter := c.waitFor(id)

	var response hostproto.HandshakeResponse
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-waiter:
		if res.err != nil {
			return fmt.Errorf("sdk: handshake failed: %w", res.err)
		}
		if err := json.Unmarshal(res.result, &response); err != nil {
			return fmt.Errorf("sdk: decode handshake response: %w", err)
		}
	}

	if !response.Accepted {
		return fmt.Errorf("sdk: handshake rejected: %s", response.Message)
	}
	if err := hostproto.ValidateProtocolCompatibility(response.ProtocolVersion); err != nil {
		return fmt.Errorf("sdk: incompatible host protocol: %w", err)
	}

	if opts.OnReady != nil {
		if err := opts.OnReady(HandshakeReady{Client: c, Response: response}); err != nil {
			return err
		}
	}

	// Block until either the context is canceled (SIGINT/SIGTERM) or
	// the read loop exits. The read loop exits when the host closes
	// stdin, which is how the host signals graceful shutdown — stdin
	// EOF works cross-platform, whereas os.Interrupt doesn't on
	// Windows.
	select {
	case <-ctx.Done():
	case <-c.readDone:
	}
	return nil
}
