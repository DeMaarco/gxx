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

package claude

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"gxx/internal/config"
)

// Login runs the copy-paste OAuth flow and persists tokens.
func Login(
	ctx context.Context,
	client *Client,
	stdout io.Writer,
	readLine func() (string, error),
) (config.ClaudeTokens, string, error) {
	if client == nil {
		client = NewClient()
	}
	if readLine == nil {
		return config.ClaudeTokens{}, "", fmt.Errorf("login requires a line reader")
	}
	pkce, err := GeneratePKCE()
	if err != nil {
		return config.ClaudeTokens{}, "", err
	}
	authURL := AuthorizationURL(pkce)
	if _, err := fmt.Fprintln(stdout, "Open this URL to log in with Claude:"); err != nil {
		return config.ClaudeTokens{}, "", err
	}
	if _, err := fmt.Fprintln(stdout, authURL); err != nil {
		return config.ClaudeTokens{}, "", err
	}
	_ = OpenBrowser(authURL)
	if _, err := fmt.Fprint(stdout, "Paste the authorization code (blank cancels): "); err != nil {
		return config.ClaudeTokens{}, "", err
	}
	pasted, err := readLine()
	if err != nil {
		return config.ClaudeTokens{}, "", fmt.Errorf("read authorization code: %w", err)
	}
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return config.ClaudeTokens{}, "", errCanceled
	}
	code, state, err := ParsePastedCode(pasted)
	if err != nil {
		return config.ClaudeTokens{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return config.ClaudeTokens{}, "", err
	}
	tokens, err := client.Exchange(ctx, code, state, pkce.Verifier)
	if err != nil {
		return config.ClaudeTokens{}, "", err
	}
	path, err := config.SaveClaudeTokens(tokens)
	if err != nil {
		return config.ClaudeTokens{}, "", err
	}
	return tokens, path, nil
}

// Logout clears persisted Claude tokens.
func Logout() (string, error) {
	return config.ClearClaudeTokens()
}

var errCanceled = fmt.Errorf("login canceled")

// IsCanceled reports whether login was left blank.
func IsCanceled(err error) bool {
	return err == errCanceled
}

// LineReader returns a reader that consumes one line from r.
func LineReader(r io.Reader) func() (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	return func() (string, error) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}
}
