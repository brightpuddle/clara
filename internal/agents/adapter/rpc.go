package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/cockroachdb/errors"
)

// JSON-RPC error codes used by the client.
const (
	codeMethodNotFound = -32601
	codeInternal       = -32603
)

// ErrClosed is reported for calls that were pending when the connection ended.
var ErrClosed = errors.New("acp connection closed")

// RPCError is a JSON-RPC error object returned by the peer.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// RPC is the request surface quirks may use.
type RPC interface {
	Call(ctx context.Context, method string, params, out any) error
}

// wireMsg is any JSON-RPC 2.0 message: request, notification or response.
type wireMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// requestHandler answers a request from the peer. A nil result with nil error
// is sent as an empty object.
type requestHandler func(method string, params json.RawMessage) (any, error)

// notifyHandler receives notifications from the peer, on the read goroutine.
type notifyHandler func(method string, params json.RawMessage)

// replyFunc receives the response to a request on the read goroutine, so work
// done there is ordered with notifications that arrived before the response.
type replyFunc func(result json.RawMessage, err error)

// rpcConn is a newline-delimited JSON-RPC 2.0 connection. One goroutine reads;
// handlers and reply hooks run on it and must not block on the connection.
type rpcConn struct {
	rw       io.ReadWriteCloser
	onReq    requestHandler
	onNotify notifyHandler
	onClose  func(err error)

	wmu sync.Mutex // serialises writes

	mu      sync.Mutex
	nextID  int64
	pending map[int64]replyFunc
	dead    bool

	done chan struct{}
}

// newRPCConn starts the read loop. onClose runs on the read goroutine once the
// loop ends and every pending request has been failed with ErrClosed.
func newRPCConn(
	rw io.ReadWriteCloser,
	onReq requestHandler,
	onNotify notifyHandler,
	onClose func(err error),
) *rpcConn {
	c := &rpcConn{
		rw:       rw,
		onReq:    onReq,
		onNotify: onNotify,
		onClose:  onClose,
		pending:  map[int64]replyFunc{},
		done:     make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Done is closed after the read loop has ended and cleanup has run.
func (c *rpcConn) Done() <-chan struct{} { return c.done }

// Close closes the underlying transport, which ends the read loop.
func (c *rpcConn) Close() error { return c.rw.Close() }

func (c *rpcConn) readLoop() {
	var loopErr error
	sc := bufio.NewReaderSize(c.rw, 64*1024)
	for {
		line, err := sc.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) &&
				!errors.Is(err, io.ErrClosedPipe) {
				loopErr = err
			}
			break
		}
	}

	c.mu.Lock()
	c.dead = true
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, fn := range pending {
		fn(nil, ErrClosed)
	}
	if c.onClose != nil {
		c.onClose(loopErr)
	}
	close(c.done)
}

func (c *rpcConn) dispatch(line []byte) {
	var m wireMsg
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	switch {
	case m.Method != "" && len(m.ID) > 0:
		c.answer(m)
	case m.Method != "":
		if c.onNotify != nil {
			c.onNotify(m.Method, m.Params)
		}
	case len(m.ID) > 0:
		var id int64
		if err := json.Unmarshal(m.ID, &id); err != nil {
			return
		}
		c.mu.Lock()
		fn := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if fn == nil {
			return
		}
		if m.Error != nil {
			fn(nil, m.Error)
			return
		}
		fn(m.Result, nil)
	}
}

func (c *rpcConn) answer(m wireMsg) {
	var (
		res any
		err error
	)
	if c.onReq == nil {
		err = &RPCError{Code: codeMethodNotFound, Message: "method not found: " + m.Method}
	} else {
		res, err = c.onReq(m.Method, m.Params)
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if err != nil {
		var re *RPCError
		if !errors.As(err, &re) {
			re = &RPCError{Code: codeInternal, Message: err.Error()}
		}
		reply["error"] = re
	} else {
		if res == nil {
			res = struct{}{}
		}
		reply["result"] = res
	}
	_ = c.write(reply)
}

func (c *rpcConn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return errors.Wrap(err, "marshal rpc message")
	}
	b = append(b, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.rw.Write(b); err != nil {
		return errors.Wrap(err, "write rpc message")
	}
	return nil
}

// Go sends a request and returns once it is written. onReply runs on the read
// goroutine when the response (or ErrClosed) arrives.
func (c *rpcConn) Go(method string, params any, onReply replyFunc) error {
	c.mu.Lock()
	if c.dead {
		c.mu.Unlock()
		return ErrClosed
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = onReply
	c.mu.Unlock()

	err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		c.mu.Lock()
		if c.pending != nil {
			delete(c.pending, id)
		}
		c.mu.Unlock()
		return err
	}
	return nil
}

// Call sends a request and waits for the response, decoding it into out.
func (c *rpcConn) Call(ctx context.Context, method string, params, out any) error {
	return c.call(ctx, method, params, out, nil)
}

// call is Call with a hook that runs on the read goroutine before the caller is
// released, used to arm per-session state before later notifications arrive.
func (c *rpcConn) call(
	ctx context.Context,
	method string,
	params, out any,
	hook func(result json.RawMessage),
) error {
	type reply struct {
		res json.RawMessage
		err error
	}
	ch := make(chan reply, 1)
	err := c.Go(method, params, func(res json.RawMessage, err error) {
		if err == nil && hook != nil {
			hook(res)
		}
		ch <- reply{res, err}
	})
	if err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return errors.Wrapf(r.err, "%s", method)
		}
		if out != nil && len(r.res) > 0 {
			if err := json.Unmarshal(r.res, out); err != nil {
				return errors.Wrapf(err, "decode %s result", method)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Notify sends a notification (no response expected).
func (c *rpcConn) Notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
