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

package auth_test

import (
	"bytes"
	"strings"
	"testing"

	"gxx/internal/auth"
)

func TestCanonicalProviderAliases(t *testing.T) {
	tests := map[string]string{
		"openai":     auth.ProviderOpenAI,
		"chatgpt":    auth.ProviderOpenAI,
		"codex":      auth.ProviderOpenAI,
		"claude":     auth.ProviderClaude,
		"anthropic":  auth.ProviderClaude,
		"api":        auth.ProviderAPI,
		"  OpenAI  ": auth.ProviderOpenAI,
	}
	for input, want := range tests {
		got, err := auth.CanonicalProvider(input)
		if err != nil || got != want {
			t.Fatalf("CanonicalProvider(%q) = %q %v, want %s", input, got, err, want)
		}
	}
	if _, err := auth.CanonicalProvider("gemini"); err == nil {
		t.Fatal("unknown provider succeeded")
	}
}

func TestParseCommand(t *testing.T) {
	command, err := auth.ParseCommand([]string{"chatgpt", "--device"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Provider != auth.ProviderOpenAI || !command.Device {
		t.Fatalf("command = %+v", command)
	}

	command, err = auth.ParseCommand([]string{"--help"})
	if err != nil || !command.Help {
		t.Fatalf("help = %+v %v", command, err)
	}

	if _, err := auth.ParseCommand([]string{"openai", "claude"}); err == nil {
		t.Fatal("extra argument succeeded")
	}
	if _, err := auth.ParseCommand([]string{"--unknown"}); err == nil {
		t.Fatal("unknown flag succeeded")
	}
}

func TestOptionsHideAPIAfterOAuth(t *testing.T) {
	open := auth.Options("")
	if len(open) != 3 || open[2].ID != auth.ProviderAPI {
		t.Fatalf("empty options = %#v", open)
	}
	connected := auth.Options(auth.ProviderClaude)
	for _, option := range connected {
		if option.ID == auth.ProviderAPI {
			t.Fatalf("api still listed after Claude login: %#v", connected)
		}
	}
}

func TestPickProvider(t *testing.T) {
	var stdout bytes.Buffer
	got, err := auth.PickProvider(&stdout, func() (string, error) {
		return "codex\n", nil
	})
	if err != nil || got != auth.ProviderOpenAI {
		t.Fatalf("PickProvider() = %q %v", got, err)
	}
	if !strings.Contains(stdout.String(), "openai or claude") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	_, err = auth.PickProvider(&stdout, func() (string, error) {
		return "  ", nil
	})
	if !auth.IsCanceled(err) {
		t.Fatalf("blank pick error = %v", err)
	}
}
