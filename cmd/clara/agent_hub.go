package main

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	agenthub "github.com/brightpuddle/clara/internal/agents/hub"
	"github.com/brightpuddle/clara/internal/agents/manager"
	agentbuiltin "github.com/brightpuddle/clara/internal/builtin/agent"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
)

const agentPullInterval = 2 * time.Second

type localSource struct{ mgr *manager.Manager }

func (s localSource) ReadEvents(
	ctx context.Context, after int64, limit int,
) ([]agents.Event, error) {
	return s.mgr.ReadEvents(ctx, "", after, limit)
}

// startAgentHub aggregates the local log and every configured remote host into
// the hub log. Remote events are republished on the bus; local ones already are.
func startAgentHub(
	ctx context.Context,
	mgr *manager.Manager,
	reg *registry.Registry,
	bus *supervisor.EventBus,
	db *store.Store,
	log zerolog.Logger,
) {
	h, err := agenthub.New(db.DB())
	if err != nil {
		log.Error().Err(err).Msg("agent hub disabled")
		return
	}
	pullers := []*agenthub.Puller{
		agenthub.NewPuller(h, mgr.Host(), localSource{mgr}, 0, nil),
	}
	for _, r := range cfg.Agents.RemoteHosts {
		src := agenthub.NewToolSource(reg.Call, r.ToolPrefix)
		pullers = append(pullers, agenthub.NewPuller(h, r.Name, src, 0,
			func(evs []agents.Event) {
				for _, ev := range evs {
					bus.PublishCloud(agentbuiltin.CloudEvent(ev))
				}
			}))
	}
	for _, p := range pullers {
		go p.Run(ctx, agentPullInterval, log)
	}
}
