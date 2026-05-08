package webexapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	tokenEndpoint    = "https://webexapis.com/v1/access_token"
	webhooksEndpoint = "https://webexapis.com/v1/webhooks"
)

// TokenResponse is the payload returned by the Webex token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// ExchangeCode exchanges an OAuth authorization code for an access+refresh token pair.
func ExchangeCode(clientID, clientSecret, code, redirectURI string) (*TokenResponse, error) {
	return tokenRequest(url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	})
}

// exchangeRefreshToken is the internal helper used by TokenManager.
func exchangeRefreshToken(clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	return tokenRequest(url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
	})
}

func tokenRequest(params url.Values) (*TokenResponse, error) {
	resp, err := http.PostForm(tokenEndpoint, params)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request: status %d: %s", resp.StatusCode, body)
	}
	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("token request: unmarshal: %w", err)
	}
	return &tr, nil
}

// WebhookInfo describes a registered Webex webhook.
type WebhookInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TargetURL string `json:"targetUrl"`
	Resource  string `json:"resource"`
	Event     string `json:"event"`
	Status    string `json:"status"`
}

// EnsureWebhook creates or updates a webhook for a specific resource.
func EnsureWebhook(accessToken, targetURL, secret, name, resource string) error {
	existing, err := listWebhooks(accessToken)
	if err != nil {
		return err
	}
	for _, wh := range existing {
		if wh.Name == name {
			// If resource changed, we should ideally delete and recreate, 
			// but we use unique names per resource usually.
			return updateWebhook(accessToken, wh.ID, targetURL, secret)
		}
	}
	return createWebhook(accessToken, targetURL, secret, name, resource)
}

func listWebhooks(accessToken string) ([]WebhookInfo, error) {
	req, err := http.NewRequest(http.MethodGet, webhooksEndpoint+"?max=100", nil)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list webhooks: status %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Items []WebhookInfo `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("list webhooks: unmarshal: %w", err)
	}
	return result.Items, nil
}

func createWebhook(accessToken, targetURL, secret, name, resource string) error {
	payload := map[string]string{
		"name":      name,
		"targetUrl": targetURL,
		"resource":  resource,
		"event":     "created",
		"secret":    secret,
	}
	return webhookWrite(http.MethodPost, webhooksEndpoint, accessToken, payload)
}

func updateWebhook(accessToken, id, targetURL, secret string) error {
	payload := map[string]string{
		"targetUrl": targetURL,
		"secret":    secret,
		"status":    "active",
	}
	return webhookWrite(http.MethodPut, webhooksEndpoint+"/"+id, accessToken, payload)
}

func webhookWrite(method, url, accessToken string, payload map[string]string) error {
	data, _ := json.Marshal(payload)
	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("webhook write: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook write: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("webhook write: status %d: %s", resp.StatusCode, body)
	}
	return nil
}