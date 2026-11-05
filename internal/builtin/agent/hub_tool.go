package agent

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/brightpuddle/clara/internal/agents/hub"
	"github.com/brightpuddle/clara/internal/registry"
)

// RegisterHub adds agent.hub.read, the aggregated view across all hosts keyed
// by the global seq. A nil hub registers the spec only (type generation).
// synced reports whether every host has finished its first pull; clients use
// it to avoid treating a still-running backfill as new activity.
func RegisterHub(reg *registry.Registry, h *hub.Hub, synced func() bool) {
	reg.RegisterWithSpec(
		mcp.NewTool("agent.hub.read",
			mcp.WithDescription(
				"Read aggregated events from all hosts after a global seq cursor."),
			mcp.WithNumber("after", mcp.Description("Return events with seq greater than this.")),
			mcp.WithNumber("limit", mcp.Description("Maximum events (default and cap 1000)."))),
		func(ctx context.Context, args map[string]any) (any, error) {
			if h == nil {
				return nil, errDisabled
			}
			after := argInt(args, "after")
			evs, err := h.Read(ctx, after, int(argInt(args, "limit")))
			if err != nil {
				return nil, err
			}
			next := after
			if n := len(evs); n > 0 {
				next = evs[n-1].Seq
			}
			return map[string]any{
				"events": evs, "next": next, "synced": synced != nil && synced(),
			}, nil
		})
}
