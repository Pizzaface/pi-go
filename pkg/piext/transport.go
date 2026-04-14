package piext

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Transport is the line-delimited JSON-RPC client used by piext.Run.
type Transport struct {
	in         io.ReadCloser
	out        io.WriteCloser
	scanner    *bufio.Scanner
	writeMu    sync.Mutex
	nextID     atomic.Uint64
	pending    sync.Map // id -> chan *rawResponse
	handlersMu sync.RWMutex
	handlers   map[string]RequestHandler
	closed     atomic.Bool
}

// RequestHandler is invoked when the host sends a request to us.
// Used for extension_event dispatch from host → extension.
type RequestHandler func(ctx context.Context, params json.RawMessage) (any, error)

type rawRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.Number    `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rawResponse struct {
	Result json.RawMessage
	Error  *rpcError
}

func newTransport(in io.ReadCloser, out io.WriteCloser) *Transport {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	t := &Transport{
		in:       in,
		out:      out,
		scanner:  scanner,
		handlers: make(map[string]RequestHandler),
	}
	go t.readLoop()
	return t
}

// Connect starts a Transport over the process's stdin/stdout.
func Connect() *Transport {
	return newTransport(stdinReadCloser{}, stdoutWriteCloser{})
}

func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = t.in.Close()
	_ = t.out.Close()
	return nil
}

// Call sends a request and blocks until the response arrives or ctx cancels.
func (t *Transport) Call(ctx context.Context, method string, params, result any) error {
	id := t.nextID.Add(1)
	ch := make(chan *rawResponse, 1)
	t.pending.Store(id, ch)
	defer t.pending.Delete(id)

	if err := t.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	}
}

// Notify sends a notification (no id, no response).
func (t *Transport) Notify(method string, params any) error {
	return t.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

// HandleRequest registers a handler for inbound requests.
func (t *Transport) HandleRequest(method string, h RequestHandler) {
	t.handlersMu.Lock()
	t.handlers[method] = h
	t.handlersMu.Unlock()
}

func (t *Transport) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err = t.out.Write(append(b, '\n'))
	return err
}

func (t *Transport) readLoop() {
	for t.scanner.Scan() {
		line := t.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rawRequest
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Method != "" {
			t.handleInbound(msg)
		} else if msg.ID != nil {
			id, err := msg.ID.Int64()
			if err != nil {
				continue
			}
			ch, ok := t.pending.Load(uint64(id))
			if !ok {
				continue
			}
			ch.(chan *rawResponse) <- &rawResponse{Result: msg.Result, Error: msg.Error}
		}
	}
}

func (t *Transport) handleInbound(msg rawRequest) {
	t.handlersMu.RLock()
	h := t.handlers[msg.Method]
	t.handlersMu.RUnlock()
	if msg.ID == nil {
		if h != nil {
			_, _ = h(context.Background(), msg.Params)
		}
		return
	}
	go func() {
		var result any
		var err error
		if h != nil {
			result, err = h(context.Background(), msg.Params)
		} else {
			err = fmt.Errorf("unknown method %q", msg.Method)
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
		if err != nil {
			resp["error"] = rpcError{Code: -32601, Message: err.Error()}
		} else {
			resp["result"] = result
		}
		_ = t.writeJSON(resp)
	}()
}
