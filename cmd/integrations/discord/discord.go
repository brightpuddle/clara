package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/brightpuddle/clara/cmd/integrations/discord/internal/discordapi"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog/log"
)

const description = "Discord integration: send messages, notifications, and interactive requests."

type Discord struct {
	cfg     discordapi.Config
	bot     *discordapi.Bot
	router  *discordapi.Router
	eventCh chan contract.Event
}

func newDiscord() *Discord {
	return &Discord{
		router:  discordapi.NewRouter(),
		eventCh: make(chan contract.Event, 64),
	}
}

func (d *Discord) Configure(raw []byte) error {
	if len(raw) == 0 {
		return errors.New("discord: no configuration provided")
	}
	if err := json.Unmarshal(raw, &d.cfg); err != nil {
		return errors.Wrap(err, "discord: unmarshal config")
	}

	if d.cfg.Enabled() {
		bot, err := discordapi.NewBot(d.cfg, d.router, log.Logger)
		if err != nil {
			return fmt.Errorf("discord: init bot: %w", err)
		}
		d.bot = bot
	}

	go func() {
		events, cancel := d.router.Subscribe("local")
		defer cancel()
		for ev := range events {
			var params map[string]string
			if err := json.Unmarshal(ev.Data, &params); err == nil {
				dataBytes, _ := json.Marshal(params)
				d.eventCh <- contract.Event{
					Name: ev.Type,
					Data: dataBytes,
				}
			}
		}
	}()

	return nil
}

func (d *Discord) Description() (string, error) { return description, nil }

func (d *Discord) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"message.send",
			mcp.WithDescription("Send a plain or embedded message to a Discord channel."),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Target Discord channel ID.")),
			mcp.WithString("content", mcp.Description("Plain text message content.")),
		),
		mcp.NewTool(
			"notification.send",
			mcp.WithDescription("Send a titled embed notification to a Discord channel."),
			mcp.WithString(
				"channel_id",
				mcp.Required(),
				mcp.Description("Target Discord channel ID."),
			),
			mcp.WithString("title", mcp.Required(), mcp.Description("Notification title.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Notification body text.")),
			mcp.WithString("level", mcp.Description("Severity: info (default), warn, or danger.")),
		),
		mcp.NewTool(
			"interactive.request",
			mcp.WithDescription(
				"Post an interactive embed with buttons and block until decided. "+
					"Returns a JSON object with 'selection' and optional 'custom_text'.",
			),
			mcp.WithString(
				"channel_id",
				mcp.Required(),
				mcp.Description("Target Discord channel ID."),
			),
			mcp.WithString("title", mcp.Required(), mcp.Description("Short title for the card.")),
			mcp.WithString("description", mcp.Description("Detail shown in the embed body.")),
			mcp.WithNumber("timeout_s", mcp.Description("Seconds to wait for decision (default 300).")),
			mcp.WithArray("options",
				mcp.Description("The specific choices available to the user. Defaults to [\"Approve\", \"Reject\"] if omitted."),
				mcp.WithStringItems(),
			),
			mcp.WithBoolean("allow_text",
				mcp.Description("Whether to provide a UI affordance for custom text feedback. Defaults to false."),
			),
		),
		mcp.NewTool(
			"message_created",
			mcp.WithDescription(
				"Event source: fired when a message is posted in any Discord channel the bot can read. "+
					"Use as a trigger: clara.on(discord.message_created). "+
					"Event data: {channel_id, channel_name, channel_topic, guild_id, message_id, user, content, attachment_url, attachment_type}. "+
					"Filter by channel_id inside the handler.",
			),
		),
		mcp.NewTool(
			"channels.list",
			mcp.WithDescription("List all channels in the Discord server (guild)."),
			mcp.WithString("guild_id", mcp.Description("Optional Discord server (guild) ID. Defaults to the bot's primary server.")),
		),
		mcp.NewTool(
			"channel.create",
			mcp.WithDescription("Create (or reuse) a Discord text channel, e.g. for an active tmux/agent session."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Channel name (e.g. 'session-clara' or 'session-ims').")),
			mcp.WithString("topic", mcp.Description("Optional channel topic metadata (e.g. 'tmux:clara').")),
			mcp.WithString("category", mcp.Description("Optional category name to place the channel under (default 'Active Agent Sessions').")),
			mcp.WithString("guild_id", mcp.Description("Optional Discord server (guild) ID.")),
		),
		mcp.NewTool(
			"channel.delete",
			mcp.WithDescription("Delete a Discord channel by channel_id or name."),
			mcp.WithString("channel_id", mcp.Description("Discord channel ID to delete.")),
			mcp.WithString("name", mcp.Description("Discord channel name to delete if channel_id is omitted.")),
			mcp.WithString("guild_id", mcp.Description("Optional Discord server (guild) ID.")),
		),
	}
	return json.Marshal(tools)
}

func (d *Discord) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "message.send":
		return d.callMessageSend(args)
	case "notification.send":
		return d.callNotificationSend(args)
	case "interactive.request":
		return d.callInteractiveRequest(args)
	case "channels.list":
		return d.callChannelsList(args)
	case "channel.create":
		return d.callChannelCreate(args)
	case "channel.delete":
		return d.callChannelDelete(args)
	case "message_created":
		return json.Marshal(map[string]string{"error": "message_created is an event source, not a callable tool"})
	default:
		return nil, errors.Newf("discord: unknown tool %q", name)
	}
}

// --- Tool implementations ---

type messageSendArgs struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

func (d *Discord) callMessageSend(args []byte) ([]byte, error) {
	if d.bot == nil {
		return nil, errors.New("discord bot account not configured")
	}
	var a messageSendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord message.send: unmarshal")
	}
	if a.ChannelID == "" {
		return nil, errors.New("channel_id required")
	}

	msgID, err := d.bot.SendMessage(a.ChannelID, a.Content, nil)
	if err != nil {
		return nil, errors.Wrap(err, "discord message.send")
	}
	return json.Marshal(map[string]string{"message_id": msgID})
}

type notificationSendArgs struct {
	ChannelID string `json:"channel_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Level     string `json:"level"`
}

func (d *Discord) callNotificationSend(args []byte) ([]byte, error) {
	if d.bot == nil {
		return nil, errors.New("discord bot account not configured")
	}
	var a notificationSendArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord notification.send: unmarshal")
	}
	if a.ChannelID == "" {
		return nil, errors.New("discord notification.send: channel_id is required")
	}
	if a.Level == "" {
		a.Level = "info"
	}

	embed := &discordapi.Embed{
		Title:       a.Title,
		Description: a.Body,
		Color:       discordapi.LevelColor(a.Level),
	}

	msgID, err := d.bot.SendMessage(a.ChannelID, "", embed)
	if err != nil {
		return nil, errors.Wrap(err, "discord notification.send")
	}
	return json.Marshal(map[string]string{"message_id": msgID})
}

type interactiveRequestArgs struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ChannelID   string   `json:"channel_id"`
	TimeoutS    float64  `json:"timeout_s"`
	Options     []string `json:"options"`
	AllowText   bool     `json:"allow_text"`
}

func (d *Discord) callInteractiveRequest(args []byte) ([]byte, error) {
	if d.bot == nil {
		return nil, errors.New("discord bot account not configured")
	}
	var a interactiveRequestArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord interactive.request: unmarshal")
	}
	if a.ChannelID == "" {
		return nil, errors.New("discord interactive.request: channel_id is required")
	}
	timeoutSec := int(a.TimeoutS)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	requestID := uuid.New().String()

	d.router.RegisterInteractive(requestID)

	_, err := d.bot.SendInteractive(a.ChannelID, "local", requestID, a.Title, a.Description, a.Options, a.AllowText)
	if err != nil {
		return nil, errors.Wrap(err, "discord interactive.request: send")
	}

	waitCh := d.router.GetInteractiveChan(requestID)
	if waitCh == nil {
		return nil, errors.New("interactive request not found")
	}

	decision, ok := d.router.WaitInteractive(requestID, waitCh, time.Duration(timeoutSec)*time.Second)
	if !ok {
		return json.Marshal(map[string]string{"selection": "timeout"})
	}

	res := map[string]string{
		"selection": decision.Selection,
	}
	if decision.CustomText != "" {
		res["custom_text"] = decision.CustomText
	}
	return json.Marshal(res)
}

func (d *Discord) callChannelsList(args []byte) ([]byte, error) {
	if d.bot == nil {
		return nil, errors.New("discord bot account not configured")
	}
	var a struct {
		GuildID string `json:"guild_id"`
	}
	_ = json.Unmarshal(args, &a)
	chs, err := d.bot.ListChannels(a.GuildID)
	if err != nil {
		return nil, errors.Wrap(err, "discord channels.list")
	}
	return json.Marshal(chs)
}

func (d *Discord) callChannelCreate(args []byte) ([]byte, error) {
	if d.bot == nil {
		return nil, errors.New("discord bot account not configured")
	}
	var a struct {
		GuildID  string `json:"guild_id"`
		Name     string `json:"name"`
		Topic    string `json:"topic"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord channel.create: unmarshal")
	}
	if a.Name == "" {
		return nil, errors.New("name is required")
	}
	ch, err := d.bot.CreateChannel(a.GuildID, a.Name, a.Topic, a.Category, false)
	if err != nil {
		return nil, errors.Wrap(err, "discord channel.create")
	}
	return json.Marshal(ch)
}

func (d *Discord) callChannelDelete(args []byte) ([]byte, error) {
	if d.bot == nil {
		return nil, errors.New("discord bot account not configured")
	}
	var a struct {
		GuildID   string `json:"guild_id"`
		ChannelID string `json:"channel_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "discord channel.delete: unmarshal")
	}
	deletedID, err := d.bot.DeleteChannel(a.GuildID, a.ChannelID, a.Name)
	if err != nil {
		return nil, errors.Wrap(err, "discord channel.delete")
	}
	return json.Marshal(map[string]string{"deleted_channel_id": deletedID})
}

func (d *Discord) StreamEvents() (<-chan contract.Event, error) {
	return d.eventCh, nil
}
