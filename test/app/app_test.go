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

package app_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gxx/internal/app"
	"gxx/internal/config"
)

func TestRunHelpDoesNotRequireConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "gxx ask") {
		t.Fatalf("stdout = %q, want usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "gxx usage") {
		t.Fatalf("stdout = %q, want usage command", stdout.String())
	}
	if !strings.Contains(stdout.String(), "gxx login") {
		t.Fatalf("stdout = %q, want login command", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--permission") {
		t.Fatalf("stdout = %q, want permission flag", stdout.String())
	}
}

func TestRunVersionPrintsRelease(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := app.Run(context.Background(), []string{"version"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, app.Version) {
		t.Fatalf("stdout = %q, want version %s", got, app.Version)
	}
}

func TestRunAskReportsMissingAPIKeyThroughInjectedIO(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(
		context.Background(),
		[]string{"ask", "--json", "inspect", "the", "repo"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		false,
	)
	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stdout.String(), `"error":"OpenAI is not configured; run gxx login"`) {
		t.Fatalf("stdout = %q, want JSON error", stdout.String())
	}
}

func TestAskPromptReadsPipedInput(t *testing.T) {
	prompt, err := app.AskPrompt(nil, strings.NewReader("inspect README\n"), false)
	if err != nil {
		t.Fatalf("askPrompt() error = %v", err)
	}
	if prompt != "inspect README" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestInteractiveModeRejectsUnexpectedArgument(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := app.Run(
		context.Background(),
		[]string{"unexpected"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		true,
	)
	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInteractiveModeStartsWithoutAPIKeyForConfigCommand(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(
		context.Background(),
		nil,
		strings.NewReader("/exit\n"),
		&stdout,
		&stderr,
		true,
	)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Run /login") {
		t.Fatalf("stdout = %q, want missing-key guidance", stdout.String())
	}
}

func TestRunUsageReportsMissingAPIKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(context.Background(), []string{"usage"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 2 {
		t.Fatalf("Run() code = %d, want 2; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "gxx login") {
		t.Fatalf("stderr = %q, want missing OpenAI credentials", stderr.String())
	}
}

func TestAskRejectsUnknownPermissionMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(
		context.Background(),
		[]string{"ask", "--permission", "trust-me", "--json", "inspect"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		false,
	)
	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stdout.String(), "permission mode") {
		t.Fatalf("stdout = %q, want permission mode error", stdout.String())
	}
}

func TestRunAskReportsMissingClaudeLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(
		context.Background(),
		[]string{"ask", "--model", "opus", "--json", "inspect", "the", "repo"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		false,
	)
	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stdout.String(), "gxx login") {
		t.Fatalf("stdout = %q, want Claude login error", stdout.String())
	}
}

func TestRunUsageReportsMissingClaudeLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("GXX_MODEL", "sonnet")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := app.Run(context.Background(), []string{"usage"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 2 {
		t.Fatalf("Run() code = %d, want 2; stdout = %q", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "gxx login") {
		t.Fatalf("stderr = %q, want Claude login", stderr.String())
	}
}

func TestRunLoginRequiresProviderWithoutTTY(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := app.Run(context.Background(), []string{"login"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 2 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "openai, claude, or api") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunLogoutRequiresProviderWithoutTTY(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := app.Run(context.Background(), []string{"logout"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 2 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no account connected") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunLogoutClearsTokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	if _, err := config.SaveClaudeTokens(config.ClaudeTokens{AccessToken: "keep-me"}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := app.Run(context.Background(), []string{"logout"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Claude login cleared") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	tokens, err := config.LoadClaudeTokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "" {
		t.Fatalf("claude tokens still present: %+v", tokens)
	}
}

func TestRunLogoutOpenAIClearsCodexTokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := config.SaveOpenAITokens(config.OpenAITokens{AccessToken: "oauth", AccountID: "acct"}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := app.Run(context.Background(), []string{"logout", "chatgpt"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("Run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpenAI login cleared") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	tokens, err := config.LoadOpenAITokens()
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "" {
		t.Fatalf("openai tokens still present: %+v", tokens)
	}
}
