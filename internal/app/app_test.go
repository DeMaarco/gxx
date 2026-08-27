package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunHelpDoesNotRequireConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "gxx ask") {
		t.Fatalf("stdout = %q, want usage", stdout.String())
	}
}

func TestRunVersionPrintsRelease(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &stdout, &stderr, false)
	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "gxx 0.0.1\n" {
		t.Fatalf("stdout = %q, want version 0.0.1", stdout.String())
	}
}

func TestRunAskReportsMissingAPIKeyThroughInjectedIO(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(
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
	prompt, err := askPrompt(nil, strings.NewReader("inspect README\n"), false)
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
	code := Run(
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

	code := Run(
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
