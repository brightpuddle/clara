package hub_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/agents/hub"
	"github.com/brightpuddle/clara/internal/store"
)

func newHub(t *testing.T) *hub.Hub {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "clara.db"), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h, err := hub.New(st.DB())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func hostEv(host string, hostSeq int64, id string) agents.Event {
	return agents.Event{
		ID: id, Host: host, HostSeq: hostSeq, Session: "s1",
		TS: time.Unix(1, 0).UTC(), Type: agents.TypeTurnStarted,
		Payload: json.RawMessage(`{"turn_id":"t"}`),
	}
}

func TestIngestAssignsGlobalSeq(t *testing.T) {
	h, ctx := newHub(t), context.Background()
	out, err := h.Ingest(ctx, []agents.Event{
		hostEv("eve", 1, "a"), hostEv("sophia", 1, "b"), hostEv("eve", 2, "c"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d events", len(out))
	}
	for i, e := range out {
		if e.Seq != int64(i+1) {
			t.Errorf("event %d seq = %d", i, e.Seq)
		}
	}
}

func TestIngestDedupesOnHostAndHostSeq(t *testing.T) {
	h, ctx := newHub(t), context.Background()
	if _, err := h.Ingest(ctx, []agents.Event{hostEv("sophia", 1, "a")}); err != nil {
		t.Fatal(err)
	}
	out, err := h.Ingest(ctx, []agents.Event{hostEv("sophia", 1, "a"), hostEv("sophia", 2, "b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].HostSeq != 2 || out[0].Seq != 2 {
		t.Fatalf("want only new event with seq 2, got %+v", out)
	}
}

func TestHostCursor(t *testing.T) {
	h, ctx := newHub(t), context.Background()
	if n, _ := h.HostCursor(ctx, "sophia"); n != 0 {
		t.Fatalf("empty cursor = %d", n)
	}
	h.Ingest(ctx, []agents.Event{hostEv("sophia", 1, "a"), hostEv("sophia", 5, "b")})
	if n, _ := h.HostCursor(ctx, "sophia"); n != 5 {
		t.Fatalf("cursor = %d", n)
	}
}

func TestReadAfterSeq(t *testing.T) {
	h, ctx := newHub(t), context.Background()
	h.Ingest(ctx, []agents.Event{hostEv("eve", 1, "a"), hostEv("sophia", 1, "b")})
	out, err := h.Read(ctx, 1, 10)
	if err != nil || len(out) != 1 || out[0].Host != "sophia" {
		t.Fatalf("got %+v, %v", out, err)
	}
}
