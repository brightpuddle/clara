package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A stream that connects and then goes silent (no events, no heartbeats) must be
// abandoned so the mirror reconnects instead of waiting forever.
func TestStreamAbandonsSilentConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"server.connected\"}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	state := filepath.Join(t.TempDir(), "service.json")
	body := `{"url":"` + srv.URL + `","pid":` + itoa(os.Getpid()) + `,"password":"x"}`
	if err := os.WriteFile(state, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &client{stateFile: state, hc: &http.Client{}, idle: 100 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		done <- c.stream(context.Background(), "/api/event", func([]byte) error { return nil })
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error for a stalled stream")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not give up on a silent connection")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
