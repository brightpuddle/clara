package webexapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// refreshBuffer is how far before expiry we proactively refresh the access token.
const refreshBuffer = 5 * time.Minute

// storedTokens is the on-disk representation of the OAuth token pair.
type storedTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// TokenManager manages an OAuth access/refresh token pair.
// It loads tokens from disk on startup, refreshes them automatically when
// they approach expiry, and persists the new pair after every refresh.
type TokenManager struct {
	mu           sync.Mutex
	tokens       storedTokens
	clientID     string
	clientSecret string
	storePath    string
}

// NewTokenManager creates a TokenManager and loads any previously stored tokens
// from disk. An error is only returned for genuinely unexpected I/O failures;
// a missing token file is silently ignored.
func NewTokenManager(clientID, clientSecret string) (*TokenManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("webex: determine home dir: %w", err)
	}
	tm := &TokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
		storePath:    filepath.Join(home, ".local", "share", "clara", "webex_tokens.json"),
	}
	_ = tm.load() // not an error if the file doesn't exist yet
	return tm, nil
}

// HasToken returns true if a refresh token is present (OAuth flow has been completed).
func (tm *TokenManager) HasToken() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.tokens.RefreshToken != ""
}

// AccessToken returns a valid access token, transparently refreshing if needed.
// Returns an error if no refresh token is available (OAuth flow not completed).
func (tm *TokenManager) AccessToken() (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.tokens.RefreshToken == "" {
		return "", fmt.Errorf(
			"webex: not authorized — complete the OAuth flow by visiting %s",
			"your Webex integration authorization URL",
		)
	}

	if time.Now().After(tm.tokens.ExpiresAt.Add(-refreshBuffer)) {
		if err := tm.doRefresh(); err != nil {
			return "", fmt.Errorf("webex: token refresh: %w", err)
		}
	}

	return tm.tokens.AccessToken, nil
}

// Store persists a new token pair received from a code-exchange or refresh.
// expiresIn is the lifetime in seconds reported by the token endpoint.
func (tm *TokenManager) Store(accessToken, refreshToken string, expiresIn int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tokens = storedTokens{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
	}
	return tm.save()
}

// doRefresh calls the Webex token endpoint to exchange the refresh token.
// Caller must hold tm.mu.
func (tm *TokenManager) doRefresh() error {
	tr, err := exchangeRefreshToken(tm.clientID, tm.clientSecret, tm.tokens.RefreshToken)
	if err != nil {
		return err
	}
	tm.tokens = storedTokens{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}
	return tm.save()
}

func (tm *TokenManager) load() error {
	data, err := os.ReadFile(tm.storePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &tm.tokens)
}

func (tm *TokenManager) save() error {
	if err := os.MkdirAll(filepath.Dir(tm.storePath), 0o700); err != nil {
		return fmt.Errorf("webex: create token store dir: %w", err)
	}
	data, err := json.Marshal(tm.tokens)
	if err != nil {
		return fmt.Errorf("webex: marshal tokens: %w", err)
	}
	return os.WriteFile(tm.storePath, data, 0o600)
}
