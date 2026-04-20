package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/pizzaface/go-pi/internal/extension/hostproto"
)

// InboundHandler handles requests coming from the extension.
type InboundHandler func(method string, params json.RawMessage) (any, error)

// RPCConn is a per-extension JSON-RPC 2.0 connection over line-delimited
// stdio. Inbound traffic (requests + notifications from the extension) is
// routed to the supplied handler; outbound Call requests are matched by id.
type RPCConn struct {
	writer  io.Writer
	writeMu sync.Mutex

	handler InboundHandler

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan rpcResult
	closed   bool
	closeErr error

	doneCh chan struct{}

	closeCbsMu sync.Mutex
	closeCbs   []func()

	// fakeCaller, when non-nil, redirects Call to an in-process stand-in.
	// Exposed for tests; not for production use.
	fakeCaller RPCCaller
}

// RPCCaller is the minimum surface an adapter uses: the same shape as
// (*RPCConn).Call. Exposed as an interface so tests can inject a fake.
type RPCCaller interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// NewRPCConnFromCaller wraps an RPCCaller in an RPCConn-compatible shell so
// tests can pretend an extension is connected without running a subprocess.
// Exposed for tests; not for production use.
func NewRPCConnFromCaller(c RPCCaller) *RPCConn {
	return &RPCConn{
		pending:    map[int64]chan rpcResult{},
		doneCh:     make(chan struct{}),
		fakeCaller: c,
	}
}

// OnClose registers a callback fired when the connection is closed. If the
// connection is already closed, the callback fires immediately (in a
// goroutine). Callbacks never hold any lock while running.
func (c *RPCConn) OnClose(fn func()) {
	if fn == nil {
		return
	}
	c.mu.Lock()
	alreadyClosed := c.closed
	c.mu.Unlock()
	if alreadyClosed {
		go fn()
		return
	}
	c.closeCbsMu.Lock()
	c.closeCbs = append(c.closeCbs, fn)
	c.closeCbsMu.Unlock()
}

type rpcResult struct {
	result json.RawMessage
	err    *rpcError
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewRPCConn starts a goroutine that reads JSON-RPC messages from r and
// dispatches them. The handler is invoked for inbound requests and
// notifications; return a value for a request to produce a response, or
// (nil, err) to produce an error response.
func NewRPCConn(r io.Reader, w io.Writer, handler InboundHandler) *RPCConn {
	c := &RPCConn{
		writer:  w,
		handler: handler,
		pending: map[int64]chan rpcResult{},
		doneCh:  make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// Call sends a request and waits for the matching response. The result is
// unmarshalled into the value pointed to by result (may be nil).
func (c *RPCConn) Call(ctx context.Context, method string, params any, result any) error {
	if c.fakeCaller != nil {
		return c.fakeCaller.Call(ctx, method, params, result)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("rpc: connection closed")
	}
	id := c.nextID + 1
	c.nextID = id
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.writeMsg(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: mustMarshal(params)}); err != nil {
		return err
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return fmt.Errorf("rpc %d: %s", res.err.Code, res.err.Message)
		}
		if result != nil && len(res.result) > 0 {
			return json.Unmarshal(res.result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.doneCh:
		return errors.New("rpc: connection closed")
	}
}

// Notify sends a notification (no response expected).
func (c *RPCConn) Notify(method string, params any) error {
	return c.writeMsg(rpcMessage{JSONRPC: "2.0", Method: method, Params: mustMarshal(params)})
}

// Close releases all pending callers.
func (c *RPCConn) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		ch <- rpcResult{err: &rpcError{Code: hostproto.ErrCodeHandlerTimeout, Message: "connection closed"}}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.doneCh)

	c.closeCbsMu.Lock()
	cbs := append([]func(){}, c.closeCbs...)
	c.closeCbs = nil
	c.closeCbsMu.Unlock()
	for _, f := range cbs {
		go f()
	}
}

func (c *RPCConn) writeMsg(m rpcMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errors.New("rpc: connection closed")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		return err
	}
	return nil
}

func (c *RPCConn) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m rpcMessage
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		c.dispatch(m)
	}
	c.Close()
}

func (c *RPCConn) dispatch(m rpcMessage) {
	if m.Method != "" {
		// Request or notification from the extension.
		result, err := c.handler(m.Method, m.Params)
		if m.ID == nil {
			return
		}
		if err != nil {
			_ = c.writeMsg(rpcMessage{
				JSONRPC: "2.0",
				ID:      m.ID,
				Error:   &rpcError{Code: hostproto.ErrCodeServiceUnsupported, Message: err.Error()},
			})
			return
		}
		_ = c.writeMsg(rpcMessage{
			JSONRPC: "2.0",
			ID:      m.ID,
			Result:  mustMarshal(result),
		})
		return
	}
	if m.ID != nil {
		c.mu.Lock()
		ch, ok := c.pending[*m.ID]
		c.mu.Unlock()
		if !ok {
			return
		}
		ch <- rpcResult{result: m.Result, err: m.Error}
	}
}

func mustMarshal(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(fmt.Sprintf("%q", err.Error()))
	}
	return data
}
