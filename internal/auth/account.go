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

// Package auth holds shared login-provider helpers for Claude and OpenAI.
package auth

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gxx/internal/config"
)

const (
	ProviderOpenAI = config.AccountOpenAI
	ProviderClaude = config.AccountClaude
	ProviderAPI    = config.AccountAPI
)

// Option is a row in the login picker.
type Option struct {
	ID    string
	Label string
	Help  string
}

var ErrCanceled = errors.New("login canceled")

// Command is a parsed `login` or `logout` invocation.
type Command struct {
	Provider string
	Device   bool
	Help     bool
}

// CanonicalProvider maps login aliases onto openai or claude.
func CanonicalProvider(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProviderOpenAI, "chatgpt", "codex", "gpt", "oai":
		return ProviderOpenAI, nil
	case ProviderClaude, config.ProviderAnthropic:
		return ProviderClaude, nil
	case ProviderAPI, "key", "apikey":
		return ProviderAPI, nil
	case "":
		return "", errors.New("provider cannot be empty")
	default:
		return "", fmt.Errorf("provider must be openai, claude, or api")
	}
}

// Options returns the login menu. The API row is hidden once another account is active.
func Options(active string) []Option {
	options := []Option{
		{ID: ProviderOpenAI, Label: "openai", Help: "ChatGPT account"},
		{ID: ProviderClaude, Label: "claude", Help: "Claude subscription"},
	}
	if active == "" || active == ProviderAPI {
		options = append(options, Option{ID: ProviderAPI, Label: "api", Help: "OpenAI API key"})
	}
	return options
}

// ParseCommand reads a provider, optional --device, and --help.
func ParseCommand(args []string) (Command, error) {
	var command Command
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			command.Help = true
		case "--device":
			command.Device = true
		default:
			if strings.HasPrefix(arg, "-") {
				return Command{}, fmt.Errorf("unexpected flag %s", arg)
			}
			if command.Provider != "" {
				return Command{}, fmt.Errorf("unexpected argument %q", arg)
			}
			provider, err := CanonicalProvider(arg)
			if err != nil {
				return Command{}, err
			}
			command.Provider = provider
		}
	}
	return command, nil
}

// PickProvider asks a TTY user to choose openai or claude.
func PickProvider(stdout io.Writer, readLine func() (string, error)) (string, error) {
	if readLine == nil {
		return "", errors.New("provider picker requires a line reader")
	}
	if _, err := fmt.Fprintln(stdout, "Choose a provider: openai or claude"); err != nil {
		return "", err
	}
	if _, err := fmt.Fprint(stdout, "Provider: "); err != nil {
		return "", err
	}
	line, err := readLine()
	if err != nil {
		return "", fmt.Errorf("read provider: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ErrCanceled
	}
	return CanonicalProvider(line)
}

// IsCanceled reports whether the user left a login prompt blank.
func IsCanceled(err error) bool {
	return errors.Is(err, ErrCanceled)
}
