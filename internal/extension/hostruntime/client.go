package hostruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dimetron/pi-go/internal/extension/hostproto"
)

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
