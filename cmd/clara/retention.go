package main

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// pruner is the part of the store the retention loop needs.
type pruner interface {
	Prune(ctx context.Context, cutoff time.Time, batch int) (int64, error)
}

const (
	retentionStartDelay = time.Minute
	retentionInterval   = 6 * time.Hour
	retentionBatch      = 5000
)

// runRetention prunes audit and run history older than keep until ctx ends. It
// waits a minute after start so it never competes with daemon startup.
func runRetention(
	ctx context.Context, p pruner, keep time.Duration, delay, every time.Duration, log zerolog.Logger,
) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		n, err := p.Prune(ctx, time.Now().Add(-keep), retentionBatch)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Warn().Err(err).Msg("retention prune failed")
		case n > 0:
			log.Info().Int64("rows", n).Dur("keep", keep).Msg("retention pruned history")
		}
		timer.Reset(every)
	}
}
