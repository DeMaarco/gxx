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

package claude_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gxx/internal/auth/claude"
	"gxx/internal/config"
)

func TestGeneratePKCEAndAuthorizeURL(t *testing.T) {
	pkce, err := claude.GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if pkce.Verifier == "" || pkce.Challenge == "" {
		t.Fatalf("empty PKCE: %+v", pkce)
	}
	if pkce.Verifier == pkce.Challenge {
		t.Fatal("verifier and challenge should differ")
	}
	raw := claude.AuthorizationURL(pkce)
	if !strings.Contains(raw, "code=true") {
		t.Fatalf("url missing code=true: %s", raw)
	}
	if !strings.Contains(raw, "code_challenge="+pkce.Challenge) {
		t.Fatalf("url missing challenge: %s", raw)
	}
	if !strings.Contains(raw, "state="+pkce.Verifier) {
		t.Fatalf("url missing state: %s", raw)
	}
}

func TestParsePastedCode(t *testing.T) {
	code, state, err := claude.ParsePastedCode("  abc#def  ")
	if err != nil || code != "abc" || state != "def" {
		t.Fatalf("code#state = %q %q %v", code, state, err)
	}
	code, state, err = claude.ParsePastedCode("only-code")
	if err != nil || code != "only-code" || state != "" {
		t.Fatalf("bare code = %q %q %v", code, state, err)
	}
	if _, _, err := claude.ParsePastedCode(""); err == nil {
		t.Fatal("empty code succeeded")
	}
	if _, _, err := claude.ParsePastedCode("https://example.com?code=x"); err == nil {
		t.Fatal("URL paste succeeded")
	}
}

func TestExchangeAndRefresh(t *testing.T) {
	var seen []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		seen = append(seen, payload)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "access-" + payload["grant_type"],
			"refresh_token": "refresh-2",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	client := &claude.Client{HTTP: server.Client(), TokenURL: server.URL, ClientID: "cid", RedirectURI: "https://example/cb"}
	tokens, err := client.Exchange(context.Background(), "auth-code", "st", "verifier")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if tokens.AccessToken != "access-authorization_code" || tokens.RefreshToken != "refresh-2" {
		t.Fatalf("exchange tokens = %+v", tokens)
	}
	if tokens.ExpiresAt.Before(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("expires_at too soon: %s", tokens.ExpiresAt)
	}

	tokens, err = client.Refresh(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tokens.AccessToken != "access-refresh_token" {
		t.Fatalf("refresh tokens = %+v", tokens)
	}
	if len(seen) != 2 || seen[0]["code"] != "auth-code" || seen[1]["refresh_token"] != "refresh-1" {
		t.Fatalf("requests = %#v", seen)
	}
}

func TestNeedsRefresh(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if claude.NeedsRefresh(config.ClaudeTokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(time.Hour)}, now) {
		t.Fatal("fresh token should not refresh")
	}
	if !claude.NeedsRefresh(config.ClaudeTokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(2 * time.Minute)}, now) {
		t.Fatal("token inside skew should refresh")
	}
	if claude.NeedsRefresh(config.ClaudeTokens{AccessToken: "a", ExpiresAt: now}, now) {
		t.Fatal("missing refresh token should not refresh")
	}
}

func TestSourcePrefersEnvironmentToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env-token")
	if _, err := config.SaveClaudeTokens(config.ClaudeTokens{AccessToken: "file-token", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	token, err := claude.NewSource(nil).AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "env-token" {
		t.Fatalf("token = %q, want env-token", token)
	}
}

func TestSourceRefreshesExpiredTokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	if _, err := config.SaveClaudeTokens(config.ClaudeTokens{
		AccessToken:  "old",
		RefreshToken: "refresh-me",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	source := claude.NewSource(&claude.Client{HTTP: server.Client(), TokenURL: server.URL})
	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q", token)
	}
	stored, err := config.LoadClaudeTokens()
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "new-access" || stored.RefreshToken != "new-refresh" {
		t.Fatalf("persisted = %+v", stored)
	}
}
