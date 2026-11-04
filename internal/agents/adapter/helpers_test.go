package adapter_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents/adapter"
)

const timeout = 5 * time.Second

type msg = map[string]any

// peer plays the agent side of an ACP connection.
type peer struct {
	t    *testing.T
	in   chan msg  // messages the client wrote
	out  io.Writer // to the client
	done chan struct{}
}

type pipeConn struct {
	r io.Reader
	w io.WriteCloser
	c io.Closer
}

func (p *pipeConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeConn) Close() error                { _ = p.w.Close(); return p.c.Close() }

func newPeer(t *testing.T) (adapter.Dialer, *peer) {
	t.Helper()
	toAgentR, toAgentW := io.Pipe()
	toClientR, toClientW := io.Pipe()
	p := &peer{t: t, in: make(chan msg, 64), out: toClientW, done: make(chan struct{})}
	go func() {
		defer close(p.in)
		br := bufio.NewReader(toAgentR)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				var m msg
				if json.Unmarshal(line, &m) == nil {
					p.in <- m
				}
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = toClientW.Close(); _ = toAgentW.Close() })
	dial := func(context.Context, string) (io.ReadWriteCloser, error) {
		// Closing the client's write side ends the agent's reads, and the
		// agent's write side ends the client's reads.
		return &pipeConn{r: toClientR, w: toAgentW, c: toClientW}, nil
	}
	return dial, p
}

func (p *peer) recv() msg {
	p.t.Helper()
	select {
	case m, ok := <-p.in:
		if !ok {
			p.t.Fatal("client closed the connection")
		}
		return m
	case <-time.After(timeout):
		p.t.Fatal("timed out waiting for client message")
	}
	return nil
}

func (p *peer) expect(method string) msg {
	p.t.Helper()
	m := p.recv()
	if m["method"] != method {
		p.t.Fatalf("got %v, want method %s", m, method)
	}
	return m
}

func (p *peer) send(m msg) {
	p.t.Helper()
	m["jsonrpc"] = "2.0"
	b, _ := json.Marshal(m)
	if _, err := p.out.Write(append(b, '\n')); err != nil {
		p.t.Fatalf("send: %v", err)
	}
}

func (p *peer) reply(req msg, result any) {
	p.t.Helper()
	p.send(msg{"id": req["id"], "result": result})
}

func (p *peer) update(sid string, u msg) {
	p.t.Helper()
	p.send(msg{"method": "session/update", "params": msg{"sessionId": sid, "update": u}})
}

func params(m msg) msg { return m["params"].(msg) }

// handshake answers initialize and session/new for a session called "s1".
func (p *peer) handshake() {
	p.t.Helper()
	p.reply(p.expect("initialize"), msg{"protocolVersion": 1})
	p.reply(p.expect("session/new"), msg{"sessionId": "s1"})
}

type opened struct {
	s   adapter.AgentSession
	err error
}

// open runs Open in the background while script plays the agent.
func open(
	t *testing.T,
	ad *adapter.ACP,
	spec adapter.OpenSpec,
	script func(),
) (adapter.AgentSession, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ch := make(chan opened, 1)
	go func() {
		s, err := ad.Open(ctx, spec)
		ch <- opened{s, err}
	}()
	script()
	select {
	case r := <-ch:
		if r.s != nil {
			t.Cleanup(func() {
				cctx, ccancel := context.WithTimeout(context.Background(), timeout)
				defer ccancel()
				_ = r.s.Close(cctx)
			})
		}
		return r.s, r.err
	case <-time.After(timeout):
		t.Fatal("Open did not return")
	}
	return nil, nil
}

func newACP(dial adapter.Dialer, q adapter.Quirks) *adapter.ACP {
	return adapter.NewACP("test", dial, q, zerolog.Nop())
}

func nextEvent(t *testing.T, s adapter.AgentSession) adapter.AgentEvent {
	t.Helper()
	select {
	case ev, ok := <-s.Events():
		if !ok {
			t.Fatal("events closed")
		}
		return ev
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
	}
	return nil
}

func eventsUntilTurnEnd(t *testing.T, s adapter.AgentSession) []adapter.AgentEvent {
	t.Helper()
	var evs []adapter.AgentEvent
	for {
		ev := nextEvent(t, s)
		evs = append(evs, ev)
		if _, ok := ev.(adapter.TurnEnded); ok {
			return evs
		}
	}
}

func openSimple(t *testing.T, q adapter.Quirks) (adapter.AgentSession, *peer) {
	t.Helper()
	dial, p := newPeer(t)
	s, err := open(t, newACP(dial, q), adapter.OpenSpec{Dir: "/tmp"}, p.handshake)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

// replay plays a recorded session (testdata/acp/*.jsonl) as the agent. Each
// recorded client message is matched by method against what the client sends,
// then the agent messages that followed it are replayed with ids remapped.
// Requests whose recorded id is in skip are dropped along with their response.
func replay(t *testing.T, p *peer, file string, skip ...int) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Dir string `json:"dir"`
		Msg msg    `json:"msg"`
	}
	skipped := map[int]bool{}
	for _, id := range skip {
		skipped[id] = true
	}
	var script []entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if id, ok := e.Msg["id"].(float64); ok && skipped[int(id)] {
			continue
		}
		script = append(script, e)
	}
	_ = f.Close()

	go func() {
		ids := map[int]any{}
		for _, e := range script {
			if e.Dir == "out" {
				var got msg
				select {
				case m, ok := <-p.in:
					if !ok {
						return
					}
					got = m
				case <-time.After(timeout):
					t.Errorf("replay: timed out waiting for %v", e.Msg["method"])
					return
				}
				if got["method"] != e.Msg["method"] {
					t.Errorf("replay: client sent %v, recording has %v",
						got["method"], e.Msg["method"])
					return
				}
				if id, ok := e.Msg["id"].(float64); ok {
					ids[int(id)] = got["id"]
				}
				continue
			}
			if _, isReq := e.Msg["method"]; !isReq {
				if id, ok := e.Msg["id"].(float64); ok {
					e.Msg["id"] = ids[int(id)]
				}
			}
			e.Msg["jsonrpc"] = "2.0"
			b, _ := json.Marshal(e.Msg)
			if _, err := p.out.Write(append(b, '\n')); err != nil {
				return
			}
		}
	}()
}
