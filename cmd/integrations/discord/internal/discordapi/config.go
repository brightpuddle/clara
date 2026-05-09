package discordapi

// Config holds Discord gateway settings.
// Values are populated from the central Eve config (see internal/config).
type Config struct {
	Token  string `json:"token"`  // Bot token from the Discord Developer Portal
	Secret string `json:"secret"` // Shared bearer secret for Clara → Eve relay auth
}

// Enabled returns true if a bot token is configured.
func (c Config) Enabled() bool { return c.Token != "" }
