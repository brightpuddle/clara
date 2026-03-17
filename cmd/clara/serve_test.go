package main

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestRunDaemonServices_StartsServersBeforeSupervisor(t *testing.T) {
	startedServers := false
	supervisorSawStartedServers := false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runDaemonServices(ctx, daemonServiceHooks{
			startServers: func(context.Context) error {
				startedServers = true
				return nil
			},
			stopServers: func() {},
			startControl: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
			startAttach: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
			startSupervisor: func(ctx context.Context) error {
				supervisorSawStartedServers = startedServers
				cancel()
				<-ctx.Done()
				return nil
			},
		}, zerolog.Nop())
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runDaemonServices returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for daemon services to exit")
	}

	if !supervisorSawStartedServers {
		t.Fatal("expected MCP servers to start before supervisor")
	}
}
