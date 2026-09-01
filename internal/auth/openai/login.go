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

package openai

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gxx/internal/auth"
	"gxx/internal/config"
)

// Login runs the ChatGPT Codex OAuth flow and persists tokens.
func Login(
	ctx context.Context,
	client *Client,
	stdout io.Writer,
	device bool,
) (config.OpenAITokens, string, error) {
	if client == nil {
		client = NewClient()
	}
	if device {
		return loginDevice(ctx, client, stdout)
	}
	return loginBrowser(ctx, client, stdout)
}

func loginBrowser(ctx context.Context, client *Client, stdout io.Writer) (config.OpenAITokens, string, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	server := NewCallbackServer()
	redirectURI, err := server.Start()
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	defer server.Close()

	authURL := AuthorizationURL(pkce, redirectURI, server.State)
	if _, err := fmt.Fprintln(stdout, "Open this URL to log in with ChatGPT:"); err != nil {
		return config.OpenAITokens{}, "", err
	}
	if _, err := fmt.Fprintln(stdout, authURL); err != nil {
		return config.OpenAITokens{}, "", err
	}
	_ = OpenBrowser(authURL)
	if _, err := fmt.Fprintln(stdout, "Waiting for the browser callback…"); err != nil {
		return config.OpenAITokens{}, "", err
	}

	result, err := server.Wait(ctx)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	if !stateEqual(result.State, server.State) {
		return config.OpenAITokens{}, "", errors.New("OAuth state mismatch")
	}
	tokens, err := client.Exchange(ctx, result.Code, pkce.Verifier, redirectURI)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	path, err := config.SaveOpenAITokens(tokens)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	return tokens, path, nil
}

func loginDevice(ctx context.Context, client *Client, stdout io.Writer) (config.OpenAITokens, string, error) {
	device, err := client.RequestDeviceCode(ctx)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	if _, err := fmt.Fprintln(stdout, "Open this URL to log in with ChatGPT:"); err != nil {
		return config.OpenAITokens{}, "", err
	}
	if _, err := fmt.Fprintln(stdout, device.Verification); err != nil {
		return config.OpenAITokens{}, "", err
	}
	if _, err := fmt.Fprintf(stdout, "Enter this code: %s\n", device.UserCode); err != nil {
		return config.OpenAITokens{}, "", err
	}
	_ = OpenBrowser(device.Verification)
	if _, err := fmt.Fprintln(stdout, "Waiting for device authorization…"); err != nil {
		return config.OpenAITokens{}, "", err
	}

	poll, err := client.PollDevice(ctx, device)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	redirectURI := client.issuer() + deviceCallbackPath
	tokens, err := client.Exchange(ctx, poll.AuthorizationCode, poll.CodeVerifier, redirectURI)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	path, err := config.SaveOpenAITokens(tokens)
	if err != nil {
		return config.OpenAITokens{}, "", err
	}
	return tokens, path, nil
}

// Logout clears persisted OpenAI Codex tokens.
func Logout() (string, error) {
	return config.ClearOpenAITokens()
}

// IsCanceled reports whether login was left blank.
func IsCanceled(err error) bool {
	return auth.IsCanceled(err)
}
