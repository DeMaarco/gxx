package app_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gxx/internal/app"
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
	if stdout.String() != "gxx v0.0.3\n" {
		t.Fatalf("stdout = %q, want version v0.0.3", stdout.String())
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
	if !strings.Contains(stdout.String(), `"error":"OPENAI_API_KEY is not set"`) {
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
	if !strings.Contains(stdout.String(), "Run /config") {
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
	if !strings.Contains(stderr.String(), "OPENAI_API_KEY is not set") {
		t.Fatalf("stderr = %q, want missing API key", stderr.String())
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
