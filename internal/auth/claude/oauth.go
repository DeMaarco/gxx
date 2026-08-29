// Copyright 2026 DeMarco
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package claude implements Claude subscription OAuth (authorization code + PKCE).
// Endpoints and client parameters follow the Claude Code login flow and may change.
package claude

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"gxx/internal/config"
)

const (
	ClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AuthorizeURL = "https://claude.ai/oauth/authorize"
	TokenURL     = "https://console.anthropic.com/v1/oauth/token"
	RedirectURI  = "https://console.anthropic.com/oauth/code/callback"
	Scope        = "org:create_api_key user:profile user:inference"

	refreshSkew = 5 * time.Minute
	maxBody     = 1 << 20
)

// PKCE is a generated verifier/challenge pair for one login attempt.
type PKCE struct {
	Verifier  string
	Challenge string
}

// TokenResponse is the JSON body returned by the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Client talks to Anthropic's OAuth token endpoint.
type Client struct {
	HTTP        *http.Client
	TokenURL    string
	ClientID    string
	RedirectURI string
}

func NewClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		TokenURL:    TokenURL,
		ClientID:    ClientID,
		RedirectURI: RedirectURI,
	}
}

func (c *Client) tokenURL() string {
	if strings.TrimSpace(c.TokenURL) != "" {
		return c.TokenURL
	}
	return TokenURL
}

func (c *Client) clientID() string {
	if strings.TrimSpace(c.ClientID) != "" {
		return c.ClientID
	}
	return ClientID
}

func (c *Client) redirectURI() string {
	if strings.TrimSpace(c.RedirectURI) != "" {
		return c.RedirectURI
	}
	return RedirectURI
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// GeneratePKCE returns a S256 PKCE pair.
func GeneratePKCE() (PKCE, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

// AuthorizeURL builds the copy-paste Claude.ai authorization URL.
func AuthorizationURL(pkce PKCE) string {
	values := url.Values{}
	values.Set("code", "true")
	values.Set("client_id", ClientID)
	values.Set("response_type", "code")
	values.Set("redirect_uri", RedirectURI)
	values.Set("scope", Scope)
	values.Set("code_challenge", pkce.Challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("state", pkce.Verifier)
	return AuthorizeURL + "?" + values.Encode()
}

// ParsePastedCode accepts `code#state` or a bare authorization code.
func ParsePastedCode(raw string) (code, state string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("authorization code cannot be empty")
	}
	if strings.Contains(raw, "#") {
		code, state, _ = strings.Cut(raw, "#")
		code = strings.TrimSpace(code)
		state = strings.TrimSpace(state)
	} else {
		code = raw
	}
	if code == "" {
		return "", "", errors.New("authorization code cannot be empty")
	}
	if i := strings.IndexAny(code, "?&"); i >= 0 {
		return "", "", errors.New("authorization code looks like a URL; paste the code only")
	}
	return code, state, nil
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code, state, verifier string) (config.ClaudeTokens, error) {
	if state == "" {
		state = verifier
	}
	body := map[string]string{
		"code":          code,
		"state":         state,
		"grant_type":    "authorization_code",
		"client_id":     c.clientID(),
		"redirect_uri":  c.redirectURI(),
		"code_verifier": verifier,
	}
	return c.postToken(ctx, body)
}

// Refresh exchanges a refresh token for a new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (config.ClaudeTokens, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return config.ClaudeTokens{}, errors.New("Claude refresh token is empty")
	}
	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.clientID(),
	}
	return c.postToken(ctx, body)
}

func (c *Client) postToken(ctx context.Context, body map[string]string) (config.ClaudeTokens, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return config.ClaudeTokens{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(string(payload)))
	if err != nil {
		return config.ClaudeTokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return config.ClaudeTokens{}, fmt.Errorf("Claude token request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return config.ClaudeTokens{}, fmt.Errorf("read Claude token response: %w", err)
	}
	if len(data) > maxBody {
		return config.ClaudeTokens{}, errors.New("Claude token response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return config.ClaudeTokens{}, fmt.Errorf("Claude token endpoint: HTTP %d: %s", resp.StatusCode, tokenErrorMessage(data))
	}
	var parsed TokenResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return config.ClaudeTokens{}, fmt.Errorf("decode Claude token response: %w", err)
	}
	access := strings.TrimSpace(parsed.AccessToken)
	if access == "" {
		return config.ClaudeTokens{}, errors.New("Claude token response omitted access_token")
	}
	expires := time.Time{}
	if parsed.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return config.ClaudeTokens{
		AccessToken:  access,
		RefreshToken: strings.TrimSpace(parsed.RefreshToken),
		ExpiresAt:    expires,
	}, nil
}

func tokenErrorMessage(data []byte) string {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	if json.Unmarshal(data, &payload) == nil {
		for _, part := range []string{payload.ErrorDescription, payload.Message, payload.Error} {
			if strings.TrimSpace(part) != "" {
				return strings.TrimSpace(part)
			}
		}
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "unknown error"
	}
	if utf8Len := len(text); utf8Len > 200 {
		return text[:200]
	}
	return text
}

// NeedsRefresh reports whether the access token should be refreshed.
func NeedsRefresh(tokens config.ClaudeTokens, now time.Time) bool {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return false
	}
	if strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")) != "" {
		return false
	}
	if tokens.ExpiresAt.IsZero() || strings.TrimSpace(tokens.RefreshToken) == "" {
		return false
	}
	return !now.Before(tokens.ExpiresAt.Add(-refreshSkew))
}

// Source loads, refreshes, and persists Claude tokens.
type Source struct {
	mu     sync.Mutex
	client *Client
	now    func() time.Time
}

func NewSource(client *Client) *Source {
	if client == nil {
		client = NewClient()
	}
	return &Source{client: client, now: time.Now}
}

// AccessToken returns a usable access token, refreshing when needed.
func (s *Source) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		return token, nil
	}
	tokens, err := config.LoadClaudeTokens()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return "", errors.New("Claude is not logged in; run /login")
	}
	if !NeedsRefresh(tokens, s.now()) {
		return tokens.AccessToken, nil
	}
	refreshed, err := s.client.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh Claude token: %w", err)
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	if _, err := config.SaveClaudeTokens(refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

// Current returns the persisted tokens without refreshing.
func Current() (config.ClaudeTokens, error) {
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		return config.ClaudeTokens{AccessToken: token}, nil
	}
	return config.LoadClaudeTokens()
}
