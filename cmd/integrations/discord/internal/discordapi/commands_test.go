package discordapi

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCommandText(t *testing.T) {
	data := discordgo.ApplicationCommandInteractionData{
		Name: "spawn",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: commandArgOption, Type: discordgo.ApplicationCommandOptionString, Value: " foo opencode "},
		},
	}
	if got := commandText(data); got != "/spawn foo opencode" {
		t.Fatalf("got %q", got)
	}
	if got := commandText(discordgo.ApplicationCommandInteractionData{Name: "help"}); got != "/help" {
		t.Fatalf("got %q", got)
	}
}

func TestApplicationCommandsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range applicationCommands() {
		if seen[c.Name] || len(c.Description) > 100 || len(c.Name) > 32 {
			t.Fatalf("bad command %q", c.Name)
		}
		seen[c.Name] = true
	}
	if !seen["help"] {
		t.Fatal("missing /help")
	}
}
