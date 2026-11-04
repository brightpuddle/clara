package hub_test

import (
	"context"
	"errors"
	"testing"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/hub"
)

type fakeSource struct {
	events []agents.Event
	err    error
	calls  int
}

func (f *fakeSource) ReadEvents(_ context.Context, after int64, limit int) ([]agents.Event, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []agents.Event
	for _, e := range f.events {
		if e.HostSeq > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestPullDrainsInBatchesAndResumes(t *testing.T) {
	h, ctx := newHub(t), context.Background()
	src := &fakeSource{}
	for i := int64(1); i <= 5; i++ {
		src.events = append(src.events, hostEv("sophia", i, string(rune('a'+i))))
	}
	var got []agents.Event
	p := hub.NewPuller(h, "sophia", src, 2, func(e []agents.Event) { got = append(got, e...) })
	n, err := p.Pull(ctx)
	if err != nil || n != 5 || len(got) != 5 {
		t.Fatalf("n=%d got=%d err=%v", n, len(got), err)
	}
	// A second pull (e.g. after reconnect) must not duplicate anything.
	if n, err := p.Pull(ctx); err != nil || n != 0 {
		t.Fatalf("repull n=%d err=%v", n, err)
	}
	src.events = append(src.events, hostEv("sophia", 6, "z"))
	if n, _ := p.Pull(ctx); n != 1 {
		t.Fatalf("new event n=%d", n)
	}
}

func TestPullErrorLeavesCursor(t *testing.T) {
	h, ctx := newHub(t), context.Background()
	src := &fakeSource{err: errors.New("tunnel down")}
	p := hub.NewPuller(h, "sophia", src, 10, nil)
	if _, err := p.Pull(ctx); err == nil {
		t.Fatal("want error")
	}
	if c, _ := h.HostCursor(ctx, "sophia"); c != 0 {
		t.Fatalf("cursor = %d", c)
	}
}
