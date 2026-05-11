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

// SendInteractive posts an embed with multiple choice buttons and an optional text input modal.
func (b *Bot) SendInteractive(channelID, machine, requestID, title, description string, options []string, allowText bool) (string, error) {
	if channelID == "" {
		return "", fmt.Errorf("channel_id is required for interactive request")
	}
	if len(options) == 0 {
		options = []string{"Approve", "Reject"}
	}

	var components []discordgo.MessageComponent
	var currentRow []discordgo.MessageComponent

	addButton := func(label string, customID string, style discordgo.ButtonStyle) {
		if len(currentRow) == 5 {
			components = append(components, discordgo.ActionsRow{Components: currentRow})
			currentRow = []discordgo.MessageComponent{}
		}
		currentRow = append(currentRow, discordgo.Button{
			Label:    label,
			Style:    style,
			CustomID: customID,
		})
	}

	for _, opt := range options {
		safeOpt := opt
		if len(safeOpt) > 30 {
			safeOpt = safeOpt[:30]
		}
		customID := fmt.Sprintf("clara:%s:%s:b:%s", machine, requestID, safeOpt)
		addButton(opt, customID, discordgo.PrimaryButton)
	}

	if allowText {
		customID := fmt.Sprintf("clara:%s:%s:modalbtn", machine, requestID)
		addButton("📝 Feedback / Other...", customID, discordgo.SecondaryButton)
	}

	if len(currentRow) > 0 {
		components = append(components, discordgo.ActionsRow{Components: currentRow})
	}

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       title,
			Description: description,
			Color:       0xFFA500, // orange — pending
		}},
		Components: components,
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
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		customID := i.MessageComponentData().CustomID
		parts := strings.SplitN(customID, ":", 5)
		if len(parts) < 4 || parts[0] != "clara" {
			return
		}
		machine := parts[1]
		requestID := parts[2]
		actionType := parts[3] // "b" or "modalbtn"

		user := ""
		if i.Member != nil && i.Member.User != nil {
			user = i.Member.User.Username
		} else if i.User != nil {
			user = i.User.Username
		}

		if actionType == "modalbtn" {
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseModal,
				Data: &discordgo.InteractionResponseData{
					CustomID: fmt.Sprintf("clara:modal:%s:%s", machine, requestID),
					Title:    "Custom Feedback",
					Components: []discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "feedback_text",
									Label:       "Your response",
									Style:       discordgo.TextInputParagraph,
									Placeholder: "Type your feedback here...",
									Required:    true,
								},
							},
						},
					},
				},
			})
			if err != nil {
				b.log.Error().Err(err).Msg("failed to send modal")
			}
			return
		}

		// It's a standard button "b"
		decisionVal := "approved" // default fallback
		if len(parts) == 5 {
			decisionVal = parts[4]
		}

		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("Selection made by %s: %s", user, decisionVal),
				Components: []discordgo.MessageComponent{}, // clear buttons
			},
		})
		if err != nil {
			b.log.Error().Err(err).Msg("failed to ack interaction")
		}

		b.router.ResolveInteractive(requestID, InteractiveDecision{
			Selection: decisionVal,
			User:      user,
		})

		// Also push to the machine's SSE stream
		data, _ := json.Marshal(map[string]string{
			"request_id": requestID,
			"selection":  decisionVal,
			"user":       user,
		})
		b.router.Publish(machine, Event{Type: "interactive_decision", Data: data})

	case discordgo.InteractionModalSubmit:
		data := i.ModalSubmitData()
		customID := data.CustomID
		parts := strings.SplitN(customID, ":", 4)
		if len(parts) != 4 || parts[0] != "clara" || parts[1] != "modal" {
			return
		}
		machine := parts[2]
		requestID := parts[3]

		customText := ""
		for _, comp := range data.Components {
			if row, ok := comp.(*discordgo.ActionsRow); ok && len(row.Components) > 0 {
				if textInput, ok := row.Components[0].(*discordgo.TextInput); ok && textInput.CustomID == "feedback_text" {
					customText = textInput.Value
				}
			}
		}

		user := ""
		if i.Member != nil && i.Member.User != nil {
			user = i.Member.User.Username
		} else if i.User != nil {
			user = i.User.Username
		}

		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Feedback received from %s.", user),
			},
		})
		if err != nil {
			b.log.Error().Err(err).Msg("failed to ack modal")
		}

		b.router.ResolveInteractive(requestID, InteractiveDecision{
			Selection:  "custom",
			CustomText: customText,
			User:       user,
		})

		// Push to SSE stream
		sseData, _ := json.Marshal(map[string]string{
			"request_id":  requestID,
			"selection":   "custom",
			"custom_text": customText,
			"user":        user,
		})
		b.router.Publish(machine, Event{Type: "interactive_decision", Data: sseData})
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
