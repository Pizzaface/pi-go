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
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// NewClient constructs a Client reading from in and writing to out.
// Typical callers pass os.Stdin / os.Stdout.
func NewClient(in io.ReadCloser, out io.Writer) *Client {
	return &Client{
		in:      in,
		out:     out,
		encoder: json.NewEncoder(out),
		decoder: json.NewDecoder(in),
		pending: make(map[int64]chan rpcResult),
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
// (added in Task 10). Exits on EOF or context cancellation.
func (c *Client) readLoop(ctx context.Context) {
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
