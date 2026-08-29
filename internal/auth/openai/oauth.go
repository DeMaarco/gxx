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

// Package openai implements ChatGPT Codex OAuth (PKCE browser + device code).
// Endpoints and client parameters follow the Codex login flow and may change.
package openai

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
	"strings"
	"sync"
	"time"

	"gxx/internal/config"
)

const (
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
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// DeviceCode is a pending device-authorization session.
type DeviceCode struct {
	UserCode     string
	DeviceAuthID string
	Interval     time.Duration
	Verification string
	ExpiresIn    time.Duration
}

// DevicePoll is the authorization_code + PKCE pair returned after approval.
type DevicePoll struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	CodeChallenge     string `json:"code_challenge"`
}

// Client talks to OpenAI's Codex OAuth endpoints.
type Client struct {
	HTTP              *http.Client
	TokenURL          string
	ClientID          string
	Issuer            string
	DeviceUserCodeURL string
	DevicePollURL     string
	PollInterval      time.Duration
	PollTimeout       time.Duration
	Now               func() time.Time
}

func NewClient() *Client {
	return &Client{
		HTTP:         &http.Client{Timeout: 30 * time.Second},
		TokenURL:     TokenURL,
		ClientID:     ClientID,
		Issuer:       Issuer,
		PollInterval: 5 * time.Second,
		PollTimeout:  15 * time.Minute,
		Now:          time.Now,
	}
}

func (c *Client) tokenURL() string {
	if c != nil && strings.TrimSpace(c.TokenURL) != "" {
		return c.TokenURL
	}
	return TokenURL
}

func (c *Client) clientID() string {
	if c != nil && strings.TrimSpace(c.ClientID) != "" {
		return c.ClientID
	}
	return ClientID
}

func (c *Client) issuer() string {
	if c != nil && strings.TrimSpace(c.Issuer) != "" {
		return strings.TrimRight(c.Issuer, "/")
	}
	return Issuer
}

func (c *Client) deviceUserCodeURL() string {
	if c != nil && strings.TrimSpace(c.DeviceUserCodeURL) != "" {
		return c.DeviceUserCodeURL
	}
	return c.issuer() + deviceUserCodePath
}

func (c *Client) devicePollURL() string {
	if c != nil && strings.TrimSpace(c.DevicePollURL) != "" {
		return c.DevicePollURL
	}
	return c.issuer() + devicePollPath
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) pollInterval() time.Duration {
	if c != nil && c.PollInterval > 0 {
		return c.PollInterval
	}
	return 5 * time.Second
}

func (c *Client) pollTimeout() time.Duration {
	if c != nil && c.PollTimeout > 0 {
		return c.PollTimeout
	}
	return 15 * time.Minute
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

// AuthorizationURL builds the ChatGPT authorization URL for a localhost callback.
func AuthorizationURL(pkce PKCE, redirectURI, state string) string {
	// Keep parameter order stable (state early) so a wrapped terminal
	// hyperlink is less likely to drop it. Encode spaces as %20 to match
	// the Codex authorize URL.
	pairs := [][2]string{
		{"response_type", "code"},
		{"client_id", ClientID},
		{"redirect_uri", redirectURI},
		{"scope", Scope},
		{"code_challenge", pkce.Challenge},
		{"code_challenge_method", "S256"},
		{"id_token_add_organizations", "true"},
		{"codex_cli_simplified_flow", "true"},
		{"state", strings.TrimSpace(state)},
		{"originator", Originator},
	}
	var parts []string
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		parts = append(parts, queryEscape(pair[0])+"="+queryEscape(pair[1]))
	}
	return AuthorizeURL + "?" + strings.Join(parts, "&")
}

func queryEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (config.OpenAITokens, error) {
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", redirectURI)
	body.Set("client_id", c.clientID())
	body.Set("code_verifier", verifier)
	return c.postToken(ctx, body)
}

// Refresh exchanges a refresh token for a new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (config.OpenAITokens, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return config.OpenAITokens{}, errors.New("OpenAI refresh token is empty")
	}
	body := url.Values{}
	body.Set("grant_type", "refresh_token")
	body.Set("refresh_token", refreshToken)
	body.Set("client_id", c.clientID())
	return c.postToken(ctx, body)
}

func (c *Client) postToken(ctx context.Context, body url.Values) (config.OpenAITokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(body.Encode()))
	if err != nil {
		return config.OpenAITokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return config.OpenAITokens{}, fmt.Errorf("OpenAI token request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return config.OpenAITokens{}, fmt.Errorf("read OpenAI token response: %w", err)
	}
	if len(data) > maxBody {
		return config.OpenAITokens{}, errors.New("OpenAI token response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return config.OpenAITokens{}, fmt.Errorf("OpenAI token endpoint: HTTP %d: %s", resp.StatusCode, tokenErrorMessage(data))
	}
	var parsed TokenResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return config.OpenAITokens{}, fmt.Errorf("decode OpenAI token response: %w", err)
	}
	access := strings.TrimSpace(parsed.AccessToken)
	if access == "" {
		return config.OpenAITokens{}, errors.New("OpenAI token response omitted access_token")
	}
	expires := time.Time{}
	if parsed.ExpiresIn > 0 {
		expires = c.now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	accountID, _ := ParseAccountID(parsed.IDToken)
	return config.OpenAITokens{
		AccessToken:  access,
		RefreshToken: strings.TrimSpace(parsed.RefreshToken),
		ExpiresAt:    expires,
		AccountID:    accountID,
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
	if len(text) > 200 {
		return text[:200]
	}
	return text
}

// RequestDeviceCode starts a Codex device-authorization session.
func (c *Client) RequestDeviceCode(ctx context.Context) (DeviceCode, error) {
	payload, err := json.Marshal(map[string]string{"client_id": c.clientID()})
	if err != nil {
		return DeviceCode{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.deviceUserCodeURL(), strings.NewReader(string(payload)))
	if err != nil {
		return DeviceCode{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("OpenAI device code request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return DeviceCode{}, fmt.Errorf("read OpenAI device code response: %w", err)
	}
	if len(data) > maxBody {
		return DeviceCode{}, errors.New("OpenAI device code response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeviceCode{}, fmt.Errorf("OpenAI device code endpoint: HTTP %d: %s", resp.StatusCode, tokenErrorMessage(data))
	}
	var parsed struct {
		UserCode     string `json:"user_code"`
		DeviceAuthID string `json:"device_auth_id"`
		Interval     any    `json:"interval"`
		ExpiresIn    any    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return DeviceCode{}, fmt.Errorf("decode OpenAI device code response: %w", err)
	}
	if strings.TrimSpace(parsed.UserCode) == "" || strings.TrimSpace(parsed.DeviceAuthID) == "" {
		return DeviceCode{}, errors.New("OpenAI device code response omitted user_code or device_auth_id")
	}
	interval := durationValue(parsed.Interval, 5*time.Second)
	if interval < time.Second {
		interval = time.Second
	}
	return DeviceCode{
		UserCode:     parsed.UserCode,
		DeviceAuthID: parsed.DeviceAuthID,
		Interval:     interval,
		Verification: c.issuer() + deviceVerifyPath,
		ExpiresIn:    durationValue(parsed.ExpiresIn, 15*time.Minute),
	}, nil
}

// PollDevice waits until the user approves the device code.
func (c *Client) PollDevice(ctx context.Context, device DeviceCode) (DevicePoll, error) {
	interval := device.Interval
	if interval <= 0 {
		interval = c.pollInterval()
	}
	deadline := c.now().Add(c.pollTimeout())
	if device.ExpiresIn > 0 {
		if exp := c.now().Add(device.ExpiresIn); exp.Before(deadline) {
			deadline = exp
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return DevicePoll{}, err
		}
		if !c.now().Before(deadline) {
			return DevicePoll{}, errors.New("OpenAI device authorization timed out")
		}
		poll, pending, err := c.pollDeviceOnce(ctx, device)
		if err != nil {
			return DevicePoll{}, err
		}
		if !pending {
			return poll, nil
		}
		if err := sleepContext(ctx, interval); err != nil {
			return DevicePoll{}, err
		}
	}
}

func (c *Client) pollDeviceOnce(ctx context.Context, device DeviceCode) (DevicePoll, bool, error) {
	payload, err := json.Marshal(map[string]string{
		"device_auth_id": device.DeviceAuthID,
		"user_code":      device.UserCode,
	})
	if err != nil {
		return DevicePoll{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.devicePollURL(), strings.NewReader(string(payload)))
	if err != nil {
		return DevicePoll{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return DevicePoll{}, false, fmt.Errorf("OpenAI device poll: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return DevicePoll{}, false, fmt.Errorf("read OpenAI device poll: %w", err)
	}
	if len(data) > maxBody {
		return DevicePoll{}, false, errors.New("OpenAI device poll response is too large")
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		if devicePending(data) || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			return DevicePoll{}, true, nil
		}
		return DevicePoll{}, false, fmt.Errorf("OpenAI device poll: HTTP %d: %s", resp.StatusCode, tokenErrorMessage(data))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DevicePoll{}, false, fmt.Errorf("OpenAI device poll: HTTP %d: %s", resp.StatusCode, tokenErrorMessage(data))
	}
	var parsed DevicePoll
	if err := json.Unmarshal(data, &parsed); err != nil {
		return DevicePoll{}, false, fmt.Errorf("decode OpenAI device poll: %w", err)
	}
	if strings.TrimSpace(parsed.AuthorizationCode) == "" || strings.TrimSpace(parsed.CodeVerifier) == "" {
		return DevicePoll{}, false, errors.New("OpenAI device poll omitted authorization_code or code_verifier")
	}
	return parsed, false, nil
}

func devicePending(data []byte) bool {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return false
	}
	switch strings.TrimSpace(payload.Error) {
	case "authorization_pending", "slow_down":
		return true
	default:
		return false
	}
}

func durationValue(value any, fallback time.Duration) time.Duration {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	case int:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	case int64:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return time.Duration(parsed) * time.Second
		}
	case string:
		if parsed, err := time.ParseDuration(typed); err == nil && parsed > 0 {
			return parsed
		}
		if parsed, err := json.Number(typed).Int64(); err == nil && parsed > 0 {
			return time.Duration(parsed) * time.Second
		}
	}
	return fallback
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// NeedsRefresh reports whether the access token should be refreshed.
func NeedsRefresh(tokens config.OpenAITokens, now time.Time) bool {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return false
	}
	if tokens.ExpiresAt.IsZero() || strings.TrimSpace(tokens.RefreshToken) == "" {
		return false
	}
	return !now.Before(tokens.ExpiresAt.Add(-refreshSkew))
}

// Source loads, refreshes, and persists OpenAI Codex tokens.
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
	tokens, err := s.ensure(ctx)
	if err != nil {
		return "", err
	}
	return tokens.AccessToken, nil
}

// AccountID returns the persisted ChatGPT account id, refreshing when needed.
func (s *Source) AccountID(ctx context.Context) (string, error) {
	tokens, err := s.ensure(ctx)
	if err != nil {
		return "", err
	}
	return tokens.AccountID, nil
}

func (s *Source) ensure(ctx context.Context) (config.OpenAITokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := config.LoadOpenAITokens()
	if err != nil {
		return config.OpenAITokens{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return config.OpenAITokens{}, errors.New("OpenAI is not logged in; run /login openai")
	}
	if !NeedsRefresh(tokens, s.now()) {
		return tokens, nil
	}
	refreshed, err := s.client.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		return config.OpenAITokens{}, fmt.Errorf("refresh OpenAI token: %w", err)
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = tokens.AccountID
	}
	if _, err := config.SaveOpenAITokens(refreshed); err != nil {
		return config.OpenAITokens{}, err
	}
	return refreshed, nil
}

// Current returns the persisted tokens without refreshing.
func Current() (config.OpenAITokens, error) {
	return config.LoadOpenAITokens()
}
