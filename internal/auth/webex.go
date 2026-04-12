package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
)

const (
	WebexAuthURL     = "https://webexapis.com/v1/authorize"
	WebexTokenURL    = "https://webexapis.com/v1/access_token"
	WebexRedirectURI = "http://localhost:48766/webex/callback"
	WebexScope       = "spark:all spark:kms"
	WebexKVKey       = "webex_tokens"
)

type WebexTokens struct {
	AccessToken           string    `json:"access_token"`
	ExpiresIn             int       `json:"expires_in"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresIn int       `json:"refresh_token_expires_in"`
	Expiry                time.Time `json:"expiry"`
	ClientID              string    `json:"client_id"`
	ClientSecret          string    `json:"client_secret"`
}

func AuthorizeWebex(ctx context.Context, clientID, clientSecret string, db *store.Store, log zerolog.Logger) error {
	state := fmt.Sprintf("%d", time.Now().UnixNano())
	authURL := fmt.Sprintf("%s?client_id=%s&response_type=code&redirect_uri=%s&scope=%s&state=%s",
		WebexAuthURL, clientID, url.QueryEscape(WebexRedirectURI), url.QueryEscape(WebexScope), state)

	fmt.Printf("Authorize Clara at Webex:\n\n%s\n\n", authURL)
	fmt.Println("Waiting for callback on http://localhost:48766/webex/callback ...")

	codeCh := make(chan string)
	errCh := make(chan error)

	server := &http.Server{Addr: ":48766"}
	http.HandleFunc("/webex/callback", func(w http.ResponseWriter, r *http.Request) {
		log.Debug().Str("url", r.URL.String()).Msg("webex oauth callback received")
		if r.URL.Query().Get("state") != state {
			errCh <- errors.New("invalid oauth state")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- errors.New("missing authorization code")
			return
		}
		fmt.Fprintf(w, "Clara authorized! You can close this window.")
		codeCh <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	var code string
	select {
	case <-ctx.Done():
		server.Shutdown(context.Background())
		return ctx.Err()
	case err := <-errCh:
		server.Shutdown(context.Background())
		return err
	case code = <-codeCh:
		server.Shutdown(context.Background())
	}

	tokens, err := exchangeCode(ctx, code, clientID, clientSecret)
	if err != nil {
		return err
	}
	tokens.ClientID = clientID
	tokens.ClientSecret = clientSecret

	if err := db.SetKV(ctx, WebexKVKey, tokens); err != nil {
		return errors.Wrap(err, "save webex tokens")
	}

	fmt.Println("Webex authorization successful! Tokens saved to database.")
	return nil
}

func RefreshToken(ctx context.Context, tokens *WebexTokens) (*WebexTokens, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", tokens.ClientID)
	data.Set("client_secret", tokens.ClientSecret)
	data.Set("refresh_token", tokens.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, "POST", WebexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := json.Marshal(resp.Body)
		return nil, fmt.Errorf("webex token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var newTokens WebexTokens
	if err := json.NewDecoder(resp.Body).Decode(&newTokens); err != nil {
		return nil, err
	}
	newTokens.Expiry = time.Now().Add(time.Duration(newTokens.ExpiresIn) * time.Second)
	newTokens.ClientID = tokens.ClientID
	newTokens.ClientSecret = tokens.ClientSecret

	return &newTokens, nil
}

func exchangeCode(ctx context.Context, code, clientID, clientSecret string) (*WebexTokens, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", WebexRedirectURI)

	req, err := http.NewRequestWithContext(ctx, "POST", WebexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := json.Marshal(resp.Body)
		return nil, fmt.Errorf("webex token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokens WebexTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	tokens.Expiry = time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	return &tokens, nil
}
