package discordapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

// EmbedField is a single named field in a Discord embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Embed is a Discord rich embed.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

// Bot owns the Discord Gateway connection and routes events.
type Bot struct {
	cfg    Config
	sess   *discordgo.Session
	router *Router
	log    zerolog.Logger
}

// NewBot creates and opens the Discord Gateway connection.
func NewBot(cfg Config, router *Router, log zerolog.Logger) (*Bot, error) {
	if !cfg.Enabled() {
		return nil, fmt.Errorf("discord bot token not configured")
	}
	sess, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}
	sess.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	b := &Bot{cfg: cfg, sess: sess, router: router, log: log}
	sess.AddHandler(b.onInteractionCreate)
	sess.AddHandler(b.onMessageCreate)

	if err := sess.Open(); err != nil {
		return nil, fmt.Errorf("open discord gateway: %w", err)
	}
	log.Info().Msg("discord gateway connected")
	return b, nil
}

// Close shuts down the Gateway connection.
func (b *Bot) Close() { _ = b.sess.Close() }

// SendMessage posts a plain or embedded message to a channel.
func (b *Bot) SendMessage(channelID, content string, embed *Embed) (string, error) {
	msg := &discordgo.MessageSend{Content: content}
	if embed != nil {
		dge := &discordgo.MessageEmbed{
			Title:       embed.Title,
			Description: embed.Description,
			Color:       embed.Color,
		}
		for _, f := range embed.Fields {
			dge.Fields = append(dge.Fields, &discordgo.MessageEmbedField{
				Name:   f.Name,
				Value:  f.Value,
				Inline: f.Inline,
			})
		}
		msg.Embeds = []*discordgo.MessageEmbed{dge}
	}
	m, err := b.sess.ChannelMessageSendComplex(channelID, msg)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

// LevelColor maps a notification level string to a Discord embed color int.
func LevelColor(level string) int {
	switch level {
	case "warn":
		return 0xFFA500
	case "danger":
		return 0xFF0000
	default:
		return 0x5865F2 // Discord blurple
	}
}

// SendApproval posts an embed with Approve/Reject buttons.
// custom_id format: clara:{machine}:{requestID}:{action}
func (b *Bot) SendApproval(channelID, machine, requestID, title, description string) (string, error) {
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required for approval")
	}
	approveID := fmt.Sprintf("clara:%s:%s:approved", machine, requestID)
	rejectID := fmt.Sprintf("clara:%s:%s:rejected", machine, requestID)

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       title,
			Description: description,
			Color:       0xFFA500, // orange — pending
		}},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "✅ Approve",
						Style:    discordgo.SuccessButton,
						CustomID: approveID,
					},
					discordgo.Button{
						Label:    "❌ Reject",
						Style:    discordgo.DangerButton,
						CustomID: rejectID,
					},
				},
			},
		},
	}
	m, err := b.sess.ChannelMessageSendComplex(channelID, msg)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}
	customID := i.MessageComponentData().CustomID

	// Ack immediately — Discord requires response within 3 seconds.
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// Parse: clara:{machine}:{requestID}:{action}
	parts := strings.SplitN(customID, ":", 4)
	if len(parts) != 4 || parts[0] != "clara" {
		return
	}
	machine, requestID, action := parts[1], parts[2], parts[3]
	if action != "approved" && action != "rejected" {
		return
	}

	user := ""
	if i.Member != nil && i.Member.User != nil {
		user = i.Member.User.Username
	} else if i.User != nil {
		user = i.User.Username
	}

	b.log.Info().
		Str("machine", machine).
		Str("request_id", requestID).
		Str("action", action).
		Str("user", user).
		Msg("approval interaction received")

	d := ApprovalDecision{Decision: action, User: user}
	b.router.ResolveApproval(requestID, d)

	// Also push to the machine's SSE stream for EventStreamer subscribers.
	data, _ := json.Marshal(map[string]string{
		"request_id": requestID,
		"decision":   action,
		"user":       user,
	})
	b.router.Publish(machine, Event{Type: "approval_decision", Data: data})

	// Update embed to reflect the decision and remove buttons.
	color := 0x57F287 // green
	emoji := "✅"
	if action == "rejected" {
		color = 0xED4245 // red
		emoji = "❌"
	}
	if len(i.Message.Embeds) > 0 {
		orig := i.Message.Embeds[0]
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:      i.Message.ID,
			Channel: i.Message.ChannelID,
			Embeds: &[]*discordgo.MessageEmbed{{
				Title:       orig.Title,
				Description: orig.Description + fmt.Sprintf("\n\n%s **%s** by %s", emoji, capitalize(action), user),
				Color:       color,
			}},
			Components: &[]discordgo.MessageComponent{},
		})
	}
}

func (b *Bot) onMessageCreate(_ *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	data, _ := json.Marshal(map[string]string{
		"channel_id": m.ChannelID,
		"message_id": m.ID,
		"user":       m.Author.Username,
		"content":    m.Content,
	})
	// Broadcast to all subscribers — intents filter by channel_id.
	b.router.Publish("", Event{Type: "message_created", Data: data})
}
