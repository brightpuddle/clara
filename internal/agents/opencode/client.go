package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

type statusError struct {
	Status int
	Body   string
}

func (e *statusError) Error() string {
	return "opencode api: status " + http.StatusText(e.Status) + ": " + e.Body
}

func isStatus(err error, codes ...int) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	for _, c := range codes {
		if se.Status == c {
			return true
		}
	}
	return false
}

// defaultStreamIdle is well above the service's heartbeat interval.
const defaultStreamIdle = 90 * time.Second

// client talks to the service and re-reads the registration file whenever the
// cached credentials stop working.
type client struct {
	stateFile string
	start     func(context.Context) error
	hc        *http.Client
	// idle is how long a stream may stay silent before it is abandoned. The
	// service sends heartbeats, so silence means a dead connection. Zero uses
	// defaultStreamIdle.
	idle time.Duration

	mu sync.Mutex
	ep *Endpoint
}

func (c *client) endpoint(ctx context.Context) (Endpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ep != nil {
		return *c.ep, nil
	}
	ep, err := Discover(c.stateFile)
	if errors.Is(err, ErrNotRunning) && c.start != nil {
		if serr := c.start(ctx); serr != nil {
			return Endpoint{}, serr
		}
		ep, err = Discover(c.stateFile)
	}
	if err != nil {
		return Endpoint{}, err
	}
	c.ep = &ep
	return ep, nil
}

func (c *client) forget() {
	c.mu.Lock()
	c.ep = nil
	c.mu.Unlock()
}

func (c *client) newRequest(
	ctx context.Context, method, path string, body any,
) (*http.Request, error) {
	ep, err := c.endpoint(ctx)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, "marshal request")
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, ep.URL+path, rd)
	if err != nil {
		return nil, errors.Wrap(err, "build request")
	}
	req.SetBasicAuth("opencode", ep.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// do performs a JSON call. A transport failure or 401 drops the cached
// endpoint and retries once against freshly discovered credentials.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = c.doOnce(ctx, method, path, body, out)
		if err == nil || ctx.Err() != nil {
			return err
		}
		var se *statusError
		if errors.As(err, &se) && se.Status != http.StatusUnauthorized {
			return err
		}
		c.forget()
	}
	return err
}

func (c *client) doOnce(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return errors.Wrapf(err, "%s %s", method, path)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &statusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errors.Wrapf(err, "decode %s", path)
	}
	return nil
}

// stream reads an SSE endpoint, calling fn for each data payload, until the
// stream ends, fn fails or ctx is cancelled.
func (c *client) stream(ctx context.Context, path string, fn func(data []byte) error) error {
	idle := c.idle
	if idle <= 0 {
		idle = defaultStreamIdle
	}
	sctx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	watchdog := time.AfterFunc(idle, cancelStream)
	defer watchdog.Stop()

	req, err := c.newRequest(sctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.hc.Do(req)
	if err != nil {
		c.forget()
		return errors.Wrapf(err, "GET %s", path)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		c.forget()
		return &statusError{Status: resp.StatusCode}
	}
	br := bufio.NewReaderSize(resp.Body, 64*1024)
	var data []byte
	for {
		line, err := br.ReadBytes('\n')
		watchdog.Reset(idle)
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			switch {
			case len(line) == 0:
				if len(data) > 0 {
					if ferr := fn(data); ferr != nil {
						return ferr
					}
					data = data[:0]
				}
			case bytes.HasPrefix(line, []byte("data:")):
				if len(data) > 0 {
					data = append(data, '\n')
				}
				data = append(data, bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" "))...)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if sctx.Err() != nil {
				return errors.Newf("event stream silent for %s", idle)
			}
			return errors.Wrap(err, "event stream ended")
		}
	}
}
