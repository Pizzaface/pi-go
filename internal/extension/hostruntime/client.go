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

	// Send the host's handshake request. The v2 spec says the
	// extension initiates, but sending ours first is harmless (the
	// SDK ignores it) and allows future host-initiated patterns.
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

	// Read the first message from the extension. In the v2
	// extension-initiated flow this is the extension's handshake
	// REQUEST (has a method field). In a hypothetical host-initiated
	// flow it would be a RESPONSE to the request we just sent.
	raw, err := c.receiveRaw(ctx)
	if err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, err
	}

	var probe struct {
		Method string              `json:"method"`
		ID     int64               `json:"id"`
		Params json.RawMessage     `json:"params"`
		Result json.RawMessage     `json:"result"`
		Error  *hostproto.RPCError `json:"error"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("decoding handshake message: %w", err)
	}

	if probe.Method != "" {
		// Extension-initiated handshake: parse the extension's
		// request, accept it, and send back a response.
		return c.handleExtensionInitiatedHandshake(probe.ID, probe.Params, request)
	}

	// Host-initiated: the extension responded to our request.
	return c.handleHostInitiatedHandshake(probe.Result, probe.Error)
}

// handleExtensionInitiatedHandshake parses the extension's handshake
// request, builds an acceptance response, sends it, and returns the
// synthetic result. The hostRequest carries the host-side metadata
// (mode, services) for validation purposes.
func (c *Client) handleExtensionInitiatedHandshake(
	requestID int64,
	params json.RawMessage,
	_ hostproto.HandshakeRequest,
) (hostproto.HandshakeResponse, error) {
	var extReq hostproto.HandshakeRequest
	if params != nil {
		if err := json.Unmarshal(params, &extReq); err != nil {
			c.healthy.Store(false)
			return hostproto.HandshakeResponse{}, fmt.Errorf("decoding extension handshake request: %w", err)
		}
	}

	if err := hostproto.ValidateProtocolCompatibility(extReq.ProtocolVersion); err != nil {
		// Reject: incompatible protocol.
		resp := hostproto.HandshakeResponse{
			ProtocolVersion: hostproto.ProtocolVersion,
			Accepted:        false,
			Message:         err.Error(),
		}
		c.sendHandshakeResponse(requestID, resp)
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, err
	}

	// Accept. Grant all requested services (capability checks happen
	// at call-time in the services registry).
	grants := make([]hostproto.ServiceGrant, len(extReq.RequestedServices))
	for i, svc := range extReq.RequestedServices {
		grants[i] = hostproto.ServiceGrant{
			Service: svc.Service,
			Version: svc.Version,
			Methods: svc.Methods,
		}
	}
	resp := hostproto.HandshakeResponse{
		ProtocolVersion: hostproto.ProtocolVersion,
		Accepted:        true,
		GrantedServices: grants,
	}
	if err := c.sendHandshakeResponse(requestID, resp); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("sending handshake response: %w", err)
	}

	c.healthy.Store(true)
	return resp, nil
}

// handleHostInitiatedHandshake processes a response to the host's own
// handshake request (the in-process fake / legacy path).
func (c *Client) handleHostInitiatedHandshake(
	result json.RawMessage,
	rpcErr *hostproto.RPCError,
) (hostproto.HandshakeResponse, error) {
	if rpcErr != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("handshake rpc error %d: %s", rpcErr.Code, rpcErr.Message)
	}

	var resp hostproto.HandshakeResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("decoding handshake response: %w", err)
	}
	if err := hostproto.ValidateProtocolCompatibility(resp.ProtocolVersion); err != nil {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, err
	}
	if !resp.Accepted {
		c.healthy.Store(false)
		return hostproto.HandshakeResponse{}, fmt.Errorf("handshake rejected: %s", resp.Message)
	}

	c.healthy.Store(true)
	return resp, nil
}

// sendHandshakeResponse writes an RPCResponse for the given request ID.
func (c *Client) sendHandshakeResponse(requestID int64, resp hostproto.HandshakeResponse) error {
	result, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(hostproto.RPCResponse{
		JSONRPC: hostproto.JSONRPCVersion,
		ID:      requestID,
		Result:  result,
	})
}

// receiveRaw reads a single JSON message from the decoder with context
// cancellation support. Returns the raw bytes for the caller to probe
// the message type.
func (c *Client) receiveRaw(ctx context.Context) (json.RawMessage, error) {
	type result struct {
		data json.RawMessage
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		c.readMu.Lock()
		defer c.readMu.Unlock()
		var raw json.RawMessage
		err := c.decoder.Decode(&raw)
		ch <- result{data: raw, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("reading handshake message: %w", res.err)
		}
		return res.data, nil
	}
}

func (c *Client) send(request hostproto.RPCRequest) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.encoder.Encode(request); err != nil {
		return fmt.Errorf("sending rpc request: %w", err)
	}
	return nil
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
