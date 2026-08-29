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

package config_test

import (
	"strings"
	"testing"
	"time"

	"gxx/internal/config"
)

func TestCanonicalModelClaudeAliases(t *testing.T) {
	tests := map[string]string{
		"opus":             "claude-opus-5",
		"sonnet":           "claude-sonnet-5",
		"fable":            "claude-fable-5",
		"mythos":           "claude-mythos-5",
		"haiku":            "claude-haiku-4-5",
		"claude":           "claude-sonnet-5",
		"claude-opus-5":    "claude-opus-5",
		"claude-sonnet-5":  "claude-sonnet-5",
		"claude-haiku-4-5": "claude-haiku-4-5",
	}
	for input, want := range tests {
		if got := config.CanonicalModel(input); got != want {
			t.Fatalf("CanonicalModel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProviderForModel(t *testing.T) {
	if got := config.ProviderForModel("opus"); got != config.ProviderAnthropic {
		t.Fatalf("opus provider = %q", got)
	}
	if got := config.ProviderForModel("terra"); got != config.ProviderOpenAI {
		t.Fatalf("terra provider = %q", got)
	}
}

func TestLoadSelectsClaudeFromProviderHint(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GXX_MODEL", "")
	t.Setenv("GXX_PROVIDER", "anthropic")
	settings := config.Load(t.TempDir())
	if settings.Model != config.DefaultClaudeModel {
		t.Fatalf("Model = %q, want %s", settings.Model, config.DefaultClaudeModel)
	}
	if settings.Provider != config.ProviderAnthropic {
		t.Fatalf("Provider = %q", settings.Provider)
	}
}

func TestValidateRejectsMissingClaudeLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	settings := config.Load(t.TempDir())
	settings.Model = "opus"
	err := settings.Validate()
	if err == nil || !strings.Contains(err.Error(), "gxx login") {
		t.Fatalf("Validate() error = %v, want Claude login", err)
	}
}

func TestValidateAcceptsClaudeEnvToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "setup-token")
	t.Setenv("GXX_MODEL", "sonnet")
	settings := config.Load(t.TempDir())
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if settings.ClaudeTokens.AccessToken != "setup-token" {
		t.Fatalf("token = %q", settings.ClaudeTokens.AccessToken)
	}
}

func TestSaveAndLoadClaudeTokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	expires := time.Date(2026, 8, 29, 15, 0, 0, 0, time.UTC)
	if _, err := config.SaveClaudeTokens(config.ClaudeTokens{
		AccessToken:  " access ",
		RefreshToken: " refresh ",
		ExpiresAt:    expires,
	}); err != nil {
		t.Fatal(err)
	}
	tokens, err := config.LoadClaudeTokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "access" || tokens.RefreshToken != "refresh" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if !tokens.ExpiresAt.Equal(expires) {
		t.Fatalf("expires = %s, want %s", tokens.ExpiresAt, expires)
	}

	settings := config.Load(t.TempDir())
	if settings.ClaudeTokens.AccessToken != "access" {
		t.Fatalf("loaded config tokens = %+v", settings.ClaudeTokens)
	}

	if _, err := config.ClearClaudeTokens(); err != nil {
		t.Fatal(err)
	}
	tokens, err = config.LoadClaudeTokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "" || tokens.RefreshToken != "" {
		t.Fatalf("cleared tokens = %+v", tokens)
	}
}

func TestEnvironmentClaudeTokenOverridesPersistent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := config.SaveClaudeTokens(config.ClaudeTokens{AccessToken: "file"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env")
	settings := config.Load(t.TempDir())
	if settings.ClaudeTokens.AccessToken != "env" {
		t.Fatalf("token = %q", settings.ClaudeTokens.AccessToken)
	}
}

func TestSaveSessionPersistsProviderFromModel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GXX_MODEL", "")
	t.Setenv("GXX_PROVIDER", "")
	if _, err := config.SaveSession("claude-opus-4-6", "medium", "272k", false, config.PermissionAsk); err != nil {
		t.Fatal(err)
	}
	settings := config.Load(t.TempDir())
	if settings.Provider != config.ProviderAnthropic || settings.Model != "claude-opus-4-6" {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestSaveAndLoadOpenAITokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	expires := time.Date(2026, 8, 29, 15, 0, 0, 0, time.UTC)
	if _, err := config.SaveOpenAITokens(config.OpenAITokens{
		AccessToken:  " access ",
		RefreshToken: " refresh ",
		ExpiresAt:    expires,
		AccountID:    " acct-1 ",
	}); err != nil {
		t.Fatal(err)
	}
	tokens, err := config.LoadOpenAITokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "access" || tokens.RefreshToken != "refresh" || tokens.AccountID != "acct-1" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if !tokens.ExpiresAt.Equal(expires) {
		t.Fatalf("expires = %s, want %s", tokens.ExpiresAt, expires)
	}

	settings := config.Load(t.TempDir())
	if !settings.HasOpenAICredentials() || settings.HasOpenAIAPIKey() {
		t.Fatalf("oauth-only credentials = key=%q tokens=%+v", settings.APIKey, settings.OpenAITokens)
	}
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if _, err := config.ClearOpenAITokens(); err != nil {
		t.Fatal(err)
	}
	tokens, err = config.LoadOpenAITokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "" || tokens.AccountID != "" {
		t.Fatalf("cleared tokens = %+v", tokens)
	}
}

func TestOpenAIAPIKeyTakesPrecedenceOverOAuth(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "environment")
	if _, err := config.SaveOpenAITokens(config.OpenAITokens{AccessToken: "oauth", AccountID: "acct"}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.SaveAPIKey("persisted-key"); err != nil {
		t.Fatal(err)
	}
	settings := config.Load(t.TempDir())
	if settings.APIKey != "environment" {
		t.Fatalf("APIKey = %q, want environment", settings.APIKey)
	}
	if settings.ActiveAccount() != config.AccountAPI {
		t.Fatalf("active = %q, want api", settings.ActiveAccount())
	}
}

func TestPersistedAPIKeyClearsOAuth(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := config.SaveOpenAITokens(config.OpenAITokens{AccessToken: "oauth"}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.SaveAPIKey("file-key"); err != nil {
		t.Fatal(err)
	}
	settings := config.Load(t.TempDir())
	if settings.APIKey != "file-key" {
		t.Fatalf("APIKey = %q", settings.APIKey)
	}
	if settings.OpenAITokens.AccessToken != "" {
		t.Fatalf("oauth still present: %+v", settings.OpenAITokens)
	}
	if settings.ActiveAccount() != config.AccountAPI {
		t.Fatalf("active = %q", settings.ActiveAccount())
	}
}

func TestContextTokensForClaude(t *testing.T) {
	if got := config.ContextTokensFor(config.ProviderAnthropic, "272k"); got != 200_000 {
		t.Fatalf("272k = %d", got)
	}
	if got := config.ContextTokensFor(config.ProviderOpenAI, "272k"); got != 272_000 {
		t.Fatalf("openai 272k = %d", got)
	}
}
