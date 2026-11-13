package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type fakePruner struct {
	mu      sync.Mutex
	cutoffs []time.Time
}

func (f *fakePruner) Prune(_ context.Context, cutoff time.Time, _ int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	return 3, nil
}

func (f *fakePruner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cutoffs)
}

func TestRunRetention_PrunesPeriodicallyAndStopsWithContext(t *testing.T) {
	p := &fakePruner{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRetention(ctx, p, 24*time.Hour, 5*time.Millisecond, 10*time.Millisecond, zerolog.Nop())
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for p.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.count() < 3 {
		t.Fatalf("pruned %d times, want >= 3", p.count())
	}
	if age := time.Since(p.cutoffs[0]); age < 23*time.Hour || age > 25*time.Hour {
		t.Errorf("cutoff is %v old, want ~24h", age)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runRetention did not stop")
	}
}
