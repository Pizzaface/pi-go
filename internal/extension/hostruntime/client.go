package hostruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

// Dispatcher routes an incoming host_call from an extension to the
// services registry. The hostruntime package defines the interface to
// avoid an import cycle with the services package.
type Dispatcher interface {
	Dispatch(extensionID string, params hostproto.HostCallParams) (json.RawMessage, error)
}

// rpcCoder is satisfied by *services.RPCError. Used to extract a
// JSON-RPC code from an error without importing the services package.
type rpcCoder interface {
	error
	RPCCode() int
}

type Client struct {
	readMu  sync.Mutex
	writeMu sync.Mutex
	encoder *json.Encoder
	decoder *json.Decoder

	stdin  io.Closer
	stdout io.Closer

	process *Process
	nextID  atomic.Int64
	healthy atomic.Bool
}

func NewClient(reader io.Reader, writer io.Writer) *Client {
	return &Client{
		encoder: json.NewEncoder(writer),
		decoder: json.NewDecoder(reader),
	}
}

func NewClientFromProcess(process *Process) *Client {
	c := &Client{
		encoder: json.NewEncoder(process.Stdin()),
		decoder: json.NewDecoder(process.Stdout()),
		stdin:   process.Stdin(),
		stdout:  process.Stdout(),
		process: process,
	}
	c.healthy.Store(true)
	go func() {
		_ = process.Wait()
		c.healthy.Store(false)
	}()
	return c
}

func (c *Client) IsHealthy() bool {
	return c.healthy.Load()
}

func (c *Client) Handshake(ctx context.Context, request hostproto.HandshakeRequest) (hostproto.HandshakeResponse, error) {
	if strings.TrimSpace(request.ProtocolVersion) == "" {
		request.ProtocolVersion = hostproto.ProtocolVersion
	}

	id := c.nextID.Add(1)
	params, err := json.Marshal(request)
	if err != nil {
		return hostproto.HandshakeResponse{}, fmt.Errorf("encoding handshake request: %w", err)
	}

	if err := c.send(hostproto.RPCRequest{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Method:  hostproto.MethodHandshake,
		Params:  params,
	}); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, err
	}

	response, err := c.receive(ctx)
	if err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, err
	}
	if response.Error != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("handshake rpc error %d: %s", response.Error.Code, response.Error.Message)
	}

	var result hostproto.HandshakeResponse
	if err := json.Unmarshal(response.Result, &result); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("decoding handshake response: %w", err)
	}
	if err := hostproto.ValidateProtocolCompatibility(result.ProtocolVersion); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, err
	}
	if !result.Accepted {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("handshake rejected: %s", result.Message)
	}

	c.healthy.Store(true)
	return result, nil
}

func (c *Client) send(request hostproto.RPCRequest) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.encoder.Encode(request); err != nil {
		return fmt.Errorf("sending rpc request: %w", err)
	}
	return nil
}

func (c *Client) receive(ctx context.Context) (hostproto.RPCResponse, error) {
	type result struct {
		response hostproto.RPCResponse
		err      error
	}
	resultCh := make(chan result, 1)

	go func() {
		c.readMu.Lock()
		defer c.readMu.Unlock()
		var response hostproto.RPCResponse
		err := c.decoder.Decode(&response)
		resultCh <- result{response: response, err: err}
	}()

	select {
	case <-ctx.Done():
		return hostproto.RPCResponse{}, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return hostproto.RPCResponse{}, fmt.Errorf("reading rpc response: %w", res.err)
		}
		return res.response, nil
	}
}

func (c *Client) Shutdown(ctx context.Context) error {
	if c.process != nil {
		return c.process.Shutdown(ctx)
	}
	return c.Close()
}

func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	c.healthy.Store(false)
	return nil
}

func DefaultHandshakeTimeout() time.Duration {
	return 5 * time.Second
}

// ServeInbound reads ext-initiated requests from the client's stdout
// (the extension's stdout, read by the host) and dispatches them via
// the registry. Runs until EOF or ctx is canceled. Returns nil on
// clean EOF or ctx.Err() on cancellation; other errors propagate.
//
// All dispatch failures are serialized as JSON-RPC error responses;
// only framing/transport errors abort the loop.
func (c *Client) ServeInbound(ctx context.Context, extensionID string, dispatcher Dispatcher) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var req hostproto.RPCRequest
		c.readMu.Lock()
		err := c.decoder.Decode(&req)
		c.readMu.Unlock()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("hostruntime: decode inbound request: %w", err)
		}
		switch req.Method {
		case hostproto.MethodHostCall:
			c.handleHostCall(extensionID, req, dispatcher)
		default:
			c.writeError(req.ID, hostproto.ErrCodeMethodNotFound, "unknown method: "+req.Method)
		}
	}
}

func (c *Client) handleHostCall(extensionID string, req hostproto.RPCRequest, dispatcher Dispatcher) {
	var params hostproto.HostCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		c.writeError(req.ID, hostproto.ErrCodeInvalidParams, "invalid host_call params: "+err.Error())
		return
	}
	result, err := dispatcher.Dispatch(extensionID, params)
	if err != nil {
		code, msg := extractRPCError(err)
		c.writeError(req.ID, code, msg)
		return
	}
	c.writeResult(req.ID, result)
}

func (c *Client) writeResult(id int64, result json.RawMessage) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Result:  result,
	})
}

func (c *Client) writeError(id int64, code int, message string) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      id,
		Error: &hostproto.RPCError{
			Code:    code,
			Message: message,
		},
	})
}

// extractRPCError pulls a JSON-RPC code and message out of an error.
// It handles *services.RPCError via the rpcCoder interface, and falls
// back to ErrCodeServiceError for anything else.
func extractRPCError(err error) (int, string) {
	if err == nil {
		return hostproto.ErrCodeServiceError, ""
	}
	if coder, ok := err.(rpcCoder); ok {
		return coder.RPCCode(), coder.Error()
	}
	return hostproto.ErrCodeServiceError, err.Error()
}
