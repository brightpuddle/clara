package discordapi

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

const commandArgOption = "args"

type slashCommand struct {
	name string
	desc string
	// arg is the description of the optional free-text argument, empty for none.
	arg string
}

// slashCommands is the full set of native commands Clara owns. Anything else
// registered under this application (for example by another program sharing the
// bot token) is removed at startup.
var slashCommands = []slashCommand{
	{"help", "Show Clara commands for this channel", ""},
	{"status", "System health, triggers and satellite status", ""},
	{"sync", "Sync agent and tmux sessions into channels", ""},
	{"recent", "Latest activity per project", ""},
	{"sessions", "List active sessions", ""},
	{"spawn", "Start an agent session", "<name> [agy|opencode] [dir] [model=<id>]"},
	{"resume", "Adopt an existing agent conversation", "[agy|opencode] [session_id]"},
	{"platform", "View or switch the primary agent platform", "webex | discord"},
	{"send", "Send text to a tmux session", "<name> <text>"},
	{"capture", "Capture the last lines of a tmux session", "<name>"},
	{"runs", "Recent workflow runs", ""},
	{"triggers", "List workflow triggers", ""},
	{"run", "Run a trigger", "<trigger id>"},
	{"code", "Run a headless opencode task", "<prompt>"},
	{"new", "Start with fresh context", ""},
	{"clear", "Reinitialize the agent with fresh context", ""},
	{"model", "Show or change the agent model", "[model id]"},
	{"stop", "Interrupt the running turn", ""},
	{"kill", "Close this session and delete the channel", ""},
}

func applicationCommands() []*discordgo.ApplicationCommand {
	out := make([]*discordgo.ApplicationCommand, 0, len(slashCommands))
	for _, c := range slashCommands {
		cmd := &discordgo.ApplicationCommand{
			Name:        c.name,
			Description: c.desc,
			Type:        discordgo.ChatApplicationCommand,
		}
		if c.arg != "" {
			cmd.Options = []*discordgo.ApplicationCommandOption{{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        commandArgOption,
				Description: c.arg,
			}}
		}
		out = append(out, cmd)
	}
	return out
}

// commandText renders an invocation as the text a user would have typed, so the
// router handles native and typed commands identically.
func commandText(data discordgo.ApplicationCommandInteractionData) string {
	text := "/" + data.Name
	for _, o := range data.Options {
		if o.Name == commandArgOption {
			if v := strings.TrimSpace(o.StringValue()); v != "" {
				text += " " + v
			}
		}
	}
	return text
}

func (b *Bot) registerCommands(s *discordgo.Session, r *discordgo.Ready) {
	appID := ""
	if r.Application != nil {
		appID = r.Application.ID
	}
	if appID == "" && s.State.User != nil {
		appID = s.State.User.ID
	}
	// Global commands are cleared so stale registrations disappear; guild
	// commands apply immediately, unlike global ones.
	if _, err := s.ApplicationCommandBulkOverwrite(
		appID, "", []*discordgo.ApplicationCommand{},
	); err != nil {
		b.log.Error().Err(err).Msg("failed to clear global slash commands")
	}
	for _, g := range r.Guilds {
		_, err := s.ApplicationCommandBulkOverwrite(appID, g.ID, applicationCommands())
		if err != nil {
			b.log.Error().Err(err).Str("guild_id", g.ID).Msg("failed to register slash commands")
			continue
		}
		b.log.Info().
			Str("guild_id", g.ID).
			Int("count", len(slashCommands)).
			Msg("slash commands registered")
	}
}

func (b *Bot) onSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	text := commandText(i.ApplicationCommandData())
	user := ""
	if i.Member != nil && i.Member.User != nil {
		user = i.Member.User.Username
	} else if i.User != nil {
		user = i.User.Username
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "▶ `" + text + "`",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		b.log.Error().Err(err).Msg("failed to ack slash command")
		return
	}
	// The "cli-" prefix tells the router there is no real message to react to.
	b.publishMessage(i.ChannelID, i.GuildID, "cli-slash-"+i.ID, user, text, "", "")
}
