package webexapi

// Config holds Webex integration settings.
// Values are populated from the central Eve config (see internal/config).
type Config struct {
	ClientID      string // OAuth Integration client ID
	ClientSecret  string // OAuth Integration client secret
	BotToken      string // Bot account permanent access token
	WebhookSecret string // HMAC-SHA1 key for X-Spark-Signature verification
	BaseURL       string // Public Eve base URL, no trailing slash
	Secret        string // Shared bearer secret for Clara → Eve relay auth
}

// OAuthRedirectURI is the redirect URI registered in the Webex Integration settings.
func (c Config) OAuthRedirectURI() string { return c.BaseURL + "/auth/webex" }

// WebhookCallbackURL is the targetUrl registered with the Webex webhooks API.
func (c Config) WebhookCallbackURL() string { return c.BaseURL + "/api/webex/callback" }

// UserEnabled returns true when OAuth credentials are configured.
func (c Config) UserEnabled() bool { return c.ClientID != "" && c.ClientSecret != "" }

// BotEnabled returns true when a bot token is configured.
func (c Config) BotEnabled() bool { return c.BotToken != "" }

// Enabled returns true when at least one Webex credential is configured.
func (c Config) Enabled() bool { return c.UserEnabled() || c.BotEnabled() }
