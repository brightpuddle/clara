package registry

import (
	"bufio"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"sync"

	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// ConnTransport implements the MCP client transport interface on top of an
// existing bidirectional stream connection.
type ConnTransport struct {
	conn   io.ReadWriteCloser
	reader *bufio.Reader

	mu        sync.RWMutex
	responses map[string]chan *mcptransport.JSONRPCResponse

	writeMu sync.Mutex

	startOnce sync.Once
	startErr  error

	done       chan error
	closed     chan struct{}
	finishOnce sync.Once
	closeOnce  sync.Once

	notifyMu       sync.RWMutex
	onNotification func(mcp.JSONRPCNotification)

	requestMu sync.RWMutex
	onRequest mcptransport.RequestHandler

	connectionLostMu sync.RWMutex
	onConnectionLost func(error)

	ctx   context.Context
	ctxMu sync.RWMutex
}

func NewConnTransport(conn io.ReadWriteCloser) *ConnTransport {
	return &ConnTransport{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		responses: make(map[string]chan *mcptransport.JSONRPCResponse),
		done:      make(chan error, 1),
		closed:    make(chan struct{}),
		ctx:       context.Background(),
	}
}

func (c *ConnTransport) Start(ctx context.Context) error {
	c.startOnce.Do(func() {
		c.ctxMu.Lock()
		c.ctx = ctx
		c.ctxMu.Unlock()
		go c.readMessages()
	})
	return c.startErr
}

func (c *ConnTransport) SendRequest(
	ctx context.Context,
	request mcptransport.JSONRPCRequest,
) (*mcptransport.JSONRPCResponse, error) {
	select {
	case <-c.closed:
		return nil, mcptransport.ErrTransportClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	payload = append(payload, '\n')

	idKey := request.ID.String()
	respCh := make(chan *mcptransport.JSONRPCResponse, 1)
	c.mu.Lock()
	c.responses[idKey] = respCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.responses, idKey)
		c.mu.Unlock()
	}()

	if err := c.writeFrame(payload); err != nil {
		return nil, err
	}

	select {
	case <-c.closed:
		select {
		case resp := <-respCh:
			return resp, nil
		default:
		}
		return nil, mcptransport.ErrTransportClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

func (c *ConnTransport) SendNotification(
	ctx context.Context,
	notification mcp.JSONRPCNotification,
) error {
	select {
	case <-c.closed:
		return mcptransport.ErrTransportClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	payload = append(payload, '\n')
	return c.writeFrame(payload)
}

func (c *ConnTransport) SetNotificationHandler(handler func(mcp.JSONRPCNotification)) {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	c.onNotification = handler
}

func (c *ConnTransport) SetRequestHandler(handler mcptransport.RequestHandler) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	c.onRequest = handler
}

func (c *ConnTransport) SetConnectionLostHandler(handler func(error)) {
	c.connectionLostMu.Lock()
	defer c.connectionLostMu.Unlock()
	c.onConnectionLost = handler
}

func (c *ConnTransport) Close() error {
	var closeErr error
	c.finish(nil)
	c.closeOnce.Do(func() {
		closeErr = c.conn.Close()
	})
	return closeErr
}

func (c *ConnTransport) GetSessionId() string {
	return ""
}

func (c *ConnTransport) Done() <-chan error {
	return c.done
}

func (c *ConnTransport) readMessages() {
	for {
		select {
		case <-c.closed:
			return
		default:
		}

		line, err := c.reader.ReadString('\n')
		if err != nil {
			if stderrors.Is(err, io.EOF) {
				c.finish(nil)
			} else {
				c.finish(err)
			}
			return
		}

		var base struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      *mcp.RequestId `json:"id,omitempty"`
			Method  string         `json:"method,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &base); err != nil {
			continue
		}

		if base.Method != "" && base.ID == nil {
			var notification mcp.JSONRPCNotification
			if err := json.Unmarshal([]byte(line), &notification); err != nil {
				continue
			}
			c.notifyMu.RLock()
			handler := c.onNotification
			c.notifyMu.RUnlock()
			if handler != nil {
				handler(notification)
			}
			continue
		}

		if base.Method != "" && base.ID != nil {
			var request mcptransport.JSONRPCRequest
			if err := json.Unmarshal([]byte(line), &request); err == nil {
				c.handleIncomingRequest(request)
				continue
			}
		}

		var response mcptransport.JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			continue
		}

		idKey := response.ID.String()
		c.mu.RLock()
		respCh, ok := c.responses[idKey]
		c.mu.RUnlock()
		if ok {
			respCh <- &response
		}
	}
}

func (c *ConnTransport) handleIncomingRequest(request mcptransport.JSONRPCRequest) {
	c.requestMu.RLock()
	handler := c.onRequest
	c.requestMu.RUnlock()
	if handler == nil {
		c.sendResponse(*mcptransport.NewJSONRPCErrorResponse(
			request.ID,
			mcp.METHOD_NOT_FOUND,
			"no request handler configured",
			nil,
		))
		return
	}

	go func() {
		c.ctxMu.RLock()
		ctx := c.ctx
		c.ctxMu.RUnlock()

		response, err := handler(ctx, request)
		if err != nil {
			c.sendResponse(*mcptransport.NewJSONRPCErrorResponse(
				request.ID,
				mcp.INTERNAL_ERROR,
				err.Error(),
				nil,
			))
			return
		}
		if response != nil {
			c.sendResponse(*response)
		}
	}()
}

func (c *ConnTransport) sendResponse(response mcptransport.JSONRPCResponse) {
	payload, err := json.Marshal(response)
	if err != nil {
		return
	}
	payload = append(payload, '\n')
	_ = c.writeFrame(payload)
}

func (c *ConnTransport) writeFrame(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.conn.Write(payload); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

func (c *ConnTransport) finish(err error) {
	c.finishOnce.Do(func() {
		close(c.closed)
		c.done <- err
		close(c.done)
		if err != nil {
			c.connectionLostMu.RLock()
			handler := c.onConnectionLost
			c.connectionLostMu.RUnlock()
			if handler != nil {
				handler(err)
			}
		}
	})
}
