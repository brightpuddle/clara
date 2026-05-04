// Package notify provides the built-in notify tools for Clara intents.
//
// Two tools are registered under the "notify" namespace:
//
//   - notify.send — fire-and-forget delivery of a message to the configured backend.
//   - notify.ask  — interactive: delivers a question and returns the answer.
//     With the dummy backend (default) it returns "acknowledged" immediately.
//     With a real backend (webex, discord) it pauses the script via PauseError
//     and resumes when the backend webhook delivers the user's reply.
//
// The active backend is selected by the notify.backend config key.
package notify

import (
	"context"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

const namespaceDescription = "Built-in notifications: send messages and ask questions via configurable backends."

// Register adds the notify.send and notify.ask tools to reg.
func Register(
	_ context.Context,
	cfg config.NotifyConfig,
	reg *registry.Registry,
	log zerolog.Logger,
) error {
	backend := cfg.Backend
	if backend == "" {
		backend = "dummy"
	}

	log.Debug().Str("backend", backend).Msg("registering notify builtin")

	reg.RegisterNamespaceDescription("notify", namespaceDescription)

	sendSpec := mcp.NewTool("notify.send",
		mcp.WithDescription("Send a fire-and-forget notification message."),
		mcp.WithString("message",
			mcp.Required(),
			mcp.Description("The message to deliver."),
		),
	)

	askSpec := mcp.NewTool("notify.ask",
		mcp.WithDescription(
			"Deliver a question and return the user's answer. "+
				"With the dummy backend, returns \"acknowledged\" immediately.",
		),
		mcp.WithString("question",
			mcp.Required(),
			mcp.Description("The question to ask."),
		),
	)

	var sendFn, askFn func(ctx context.Context, args map[string]any) (any, error)

	switch backend {
	case "dummy", "":
		sendFn = dummySend(log)
		askFn = dummyAsk(log)
	case "discord":
		if cfg.Discord.ChannelID == "" {
			return errors.New("notify: discord backend requires notify.discord.channel_id")
		}
		sendFn = discordSend(reg, cfg.Discord.ChannelID, log)
		askFn = discordAsk(reg, cfg.Discord.ChannelID, log)
	default:
		return errors.Newf("notify: unsupported backend %q", backend)
	}

	reg.RegisterWithSpec(sendSpec, sendFn)
	reg.RegisterWithSpec(askSpec, askFn)

	return nil
}

// dummySend logs the message and returns immediately.
func dummySend(log zerolog.Logger) func(ctx context.Context, args map[string]any) (any, error) {
	return func(_ context.Context, args map[string]any) (any, error) {
		message, _ := args["message"].(string)
		if message == "" {
			return nil, errors.New("notify.send: message is required")
		}
		log.Info().Str("backend", "dummy").Str("message", message).Msg("notify.send")
		return "notification sent", nil
	}
}

// dummyAsk logs the question and returns "acknowledged" immediately so scripts
// are never blocked when no real backend is configured.
func dummyAsk(log zerolog.Logger) func(ctx context.Context, args map[string]any) (any, error) {
	return func(_ context.Context, args map[string]any) (any, error) {
		question, _ := args["question"].(string)
		if question == "" {
			return nil, errors.New("notify.ask: question is required")
		}
		log.Info().Str("backend", "dummy").Str("question", question).Msg("notify.ask")
		return "acknowledged", nil
	}
}

// discordSend sends a notification via the discord.notification.send tool.
// The discord plugin must be loaded; the lookup is lazy (happens at call time).
func discordSend(
	reg *registry.Registry,
	channelID string,
	log zerolog.Logger,
) func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		message, _ := args["message"].(string)
		if message == "" {
			return nil, errors.New("notify.send: message is required")
		}
		result, err := reg.Call(ctx, "discord.notification.send", map[string]any{
			"channel_id": channelID,
			"title":      "Clara",
			"body":       message,
			"level":      "info",
		})
		if err != nil {
			log.Error().Err(err).Str("backend", "discord").Msg("notify.send failed")
			return nil, errors.Wrap(err, "notify.send: discord")
		}
		return result, nil
	}
}

// discordAsk posts an approval request via discord.approval.request and blocks
// until the user clicks a button. Returns "approved" or "rejected".
func discordAsk(
	reg *registry.Registry,
	channelID string,
	log zerolog.Logger,
) func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		question, _ := args["question"].(string)
		if question == "" {
			return nil, errors.New("notify.ask: question is required")
		}
		result, err := reg.Call(ctx, "discord.approval.request", map[string]any{
			"channel_id":  channelID,
			"title":       "Clara needs your input",
			"description": question,
		})
		if err != nil {
			log.Error().Err(err).Str("backend", "discord").Msg("notify.ask failed")
			return nil, errors.Wrap(err, "notify.ask: discord")
		}
		return result, nil
	}
}
