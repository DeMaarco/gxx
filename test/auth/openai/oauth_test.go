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

package openai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	openaiauth "gxx/internal/auth/openai"
	"gxx/internal/config"
)

func TestGeneratePKCEAndAuthorizeURL(t *testing.T) {
	pkce, err := openaiauth.GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if pkce.Verifier == "" || pkce.Challenge == "" || pkce.Verifier == pkce.Challenge {
		t.Fatalf("PKCE = %+v", pkce)
	}
	raw := openaiauth.AuthorizationURL(pkce, "http://localhost:1455/auth/callback", "state-1")
	for _, part := range []string{
		"response_type=code",
		"code_challenge=" + pkce.Challenge,
		"code_challenge_method=S256",
		"id_token_add_organizations=true",
		"codex_cli_simplified_flow=true",
		"originator=gxx",
		"state=state-1",
		"redirect_uri=",
	} {
		if !strings.Contains(raw, part) {
			t.Fatalf("url missing %q: %s", part, raw)
		}
	}
	if strings.Contains(raw, "codex_cli_rs") {
		t.Fatal("authorize URL impersonated Codex CLI")
	}
}

func TestParseAccountIDFromIDToken(t *testing.T) {
	id, err := openaiauth.ParseAccountID(jwtWithAccount("acct-nested"))
	if err != nil || id != "acct-nested" {
		t.Fatalf("nested = %q %v", id, err)
	}
	id, err = openaiauth.ParseAccountID(jwtWithClaims(`{"chatgpt_account_id":"acct-top"}`))
	if err != nil || id != "acct-top" {
		t.Fatalf("top-level = %q %v", id, err)
	}
	if _, err := openaiauth.ParseAccountID(""); err == nil {
		t.Fatal("empty id_token succeeded")
	}
	if _, err := openaiauth.ParseAccountID("not-a-jwt"); err == nil {
		t.Fatal("invalid JWT succeeded")
	}
	if _, err := openaiauth.ParseAccountID(jwtWithClaims(`{"sub":"x"}`)); err == nil {
		t.Fatal("missing account id succeeded")
	}
}

func TestExchangeAndRefreshParseAccountID(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("content-type = %q", request.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, values)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "access-" + values.Get("grant_type"),
			"refresh_token": "refresh-2",
			"id_token":      jwtWithAccount("acct-99"),
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	client := &openaiauth.Client{HTTP: server.Client(), TokenURL: server.URL, ClientID: "cid"}
	tokens, err := client.Exchange(context.Background(), "auth-code", "verifier", "http://localhost:1455/auth/callback")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if tokens.AccessToken != "access-authorization_code" || tokens.AccountID != "acct-99" {
		t.Fatalf("exchange tokens = %+v", tokens)
	}

	tokens, err = client.Refresh(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tokens.AccessToken != "access-refresh_token" {
		t.Fatalf("refresh tokens = %+v", tokens)
	}
	if len(seen) != 2 || seen[0].Get("code") != "auth-code" || seen[1].Get("refresh_token") != "refresh-1" {
		t.Fatalf("requests = %#v", seen)
	}
}

func TestDeviceCodePollAndExchange(t *testing.T) {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/usercode", func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload["client_id"] == "" {
			t.Error("missing client_id")
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"user_code":      "ABCD-1234",
			"device_auth_id": "dev-1",
			"interval":       0,
		})
	})
	mux.HandleFunc("/poll", func(writer http.ResponseWriter, _ *http.Request) {
		polls++
		if polls < 2 {
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"authorization_code": "dev-code",
			"code_verifier":      "server-verifier",
		})
	})
	mux.HandleFunc("/token", func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("code") != "dev-code" || values.Get("code_verifier") != "server-verifier" {
			t.Errorf("exchange = %s", body)
		}
		if !strings.Contains(values.Get("redirect_uri"), "deviceauth/callback") {
			t.Errorf("redirect_uri = %q", values.Get("redirect_uri"))
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"access_token":  "device-access",
			"refresh_token": "device-refresh",
			"id_token":      jwtWithAccount("acct-device"),
			"expires_in":    3600,
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &openaiauth.Client{
		HTTP:              server.Client(),
		Issuer:            server.URL,
		TokenURL:          server.URL + "/token",
		DeviceUserCodeURL: server.URL + "/usercode",
		DevicePollURL:     server.URL + "/poll",
		PollInterval:      time.Millisecond,
		PollTimeout:       time.Second,
	}
	device, err := client.RequestDeviceCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if device.UserCode != "ABCD-1234" || device.DeviceAuthID != "dev-1" {
		t.Fatalf("device = %+v", device)
	}
	device.Interval = time.Millisecond
	poll, err := client.PollDevice(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := client.Exchange(context.Background(), poll.AuthorizationCode, poll.CodeVerifier, client.Issuer+"/deviceauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "device-access" || tokens.AccountID != "acct-device" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if polls != 2 {
		t.Fatalf("polls = %d", polls)
	}
}

func TestCallbackServerAcceptsMatchingState(t *testing.T) {
	server := openaiauth.NewCallbackServer()
	server.Ports = []int{0}
	server.State = "expected-state"
	redirect, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if !strings.Contains(redirect, "http://localhost:") || !strings.HasSuffix(redirect, "/auth/callback") {
		t.Fatalf("redirect = %q", redirect)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Get(redirect + "?code=browser-code&state=expected-state")
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			errCh <- fmt.Errorf("status %d", resp.StatusCode)
			return
		}
		errCh <- nil
	}()

	result, err := server.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "browser-code" {
		t.Fatalf("code = %q", result.Code)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestCallbackServerAcceptsCodeWhenStateDiffers(t *testing.T) {
	server := openaiauth.NewCallbackServer()
	server.Ports = []int{0}
	server.State = "expected-state"
	redirect, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		resp, err := http.Get(redirect + "?code=browser-code&state=other")
		if err == nil {
			resp.Body.Close()
		}
	}()
	result, err := server.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "browser-code" {
		t.Fatalf("code = %q", result.Code)
	}
}

func TestCallbackServerIgnoresProbeThenAcceptsCode(t *testing.T) {
	server := openaiauth.NewCallbackServer()
	server.Ports = []int{0}
	server.State = "expected-state"
	redirect, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	resp, err := http.Get(redirect)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Waiting for login") {
		t.Fatalf("probe = %d %s", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		resp, err := http.Get(redirect + "?code=after-probe")
		if err == nil {
			resp.Body.Close()
		}
	}()
	result, err := server.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "after-probe" {
		t.Fatalf("code = %q", result.Code)
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if openaiauth.NeedsRefresh(config.OpenAITokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(time.Hour)}, now) {
		t.Fatal("fresh token should not refresh")
	}
	if !openaiauth.NeedsRefresh(config.OpenAITokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: now.Add(2 * time.Minute)}, now) {
		t.Fatal("token inside skew should refresh")
	}
	if openaiauth.NeedsRefresh(config.OpenAITokens{AccessToken: "a", ExpiresAt: now}, now) {
		t.Fatal("missing refresh token should not refresh")
	}
}

func TestSourceRefreshesExpiredTokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := config.SaveOpenAITokens(config.OpenAITokens{
		AccessToken:  "old",
		RefreshToken: "refresh-me",
		ExpiresAt:    time.Now().Add(-time.Minute),
		AccountID:    "acct-keep",
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

	source := openaiauth.NewSource(&openaiauth.Client{HTTP: server.Client(), TokenURL: server.URL})
	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q", token)
	}
	account, err := source.AccountID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if account != "acct-keep" {
		t.Fatalf("account = %q", account)
	}
	stored, err := config.LoadOpenAITokens()
	if err != nil {
		t.Fatal(err)
	}
	if stored.AccessToken != "new-access" || stored.RefreshToken != "new-refresh" || stored.AccountID != "acct-keep" {
		t.Fatalf("persisted = %+v", stored)
	}
}

func jwtWithAccount(id string) string {
	return jwtWithClaims(fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":%q}}`, id))
}

func jwtWithClaims(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return header + "." + body + ".sig"
}
