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

package tools

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"gxx/internal/workspace"
)

const screenshotPathHint = "Screenshot without a path is written outside the workspace. If the user asked to keep the image, retry with a workspace-relative name such as hero.png."

func rewriteLocalBrowserOpen(ws *workspace.Workspace, command string) string {
	if ws == nil {
		return command
	}
	fields := strings.Fields(command)
	index, target := browserOpenTarget(fields)
	if index < 0 || hasURLScheme(target) {
		return command
	}
	clean, err := ws.Clean(target)
	if err != nil {
		return command
	}
	if _, err := ws.Stat(clean); err != nil {
		return command
	}
	fileURL := workspaceFileURL(filepath.Join(ws.Root(), filepath.FromSlash(clean)))
	if fileURL == "" {
		return command
	}
	fields[index] = quoteShellToken(fileURL)
	return strings.Join(fields, " ")
}

func rewriteLocalBrowserScreenshot(ws *workspace.Workspace, command string) string {
	if ws == nil {
		return command
	}
	fields := strings.Fields(command)
	index, dest := browserScreenshotDest(fields)
	if index < 0 || dest == "" || hasURLScheme(dest) {
		return command
	}
	if filepath.IsAbs(dest) {
		if pathInsideWorkspace(ws.Root(), dest) {
			fields[index] = quoteShellToken(filepath.Clean(dest))
			return strings.Join(fields, " ")
		}
		return command
	}
	clean, err := ws.Clean(dest)
	if err != nil {
		return command
	}
	abs := filepath.Join(ws.Root(), filepath.FromSlash(clean))
	fields[index] = quoteShellToken(abs)
	return strings.Join(fields, " ")
}

func rewriteLocalBrowserCommand(ws *workspace.Workspace, command string) string {
	return rewriteLocalBrowserScreenshot(ws, rewriteLocalBrowserOpen(ws, command))
}

func hasEscapingFileURL(root, command string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	for _, token := range fileURLTokens(command) {
		path, ok := fileURLPath(token)
		if !ok {
			return true
		}
		if !pathInsideWorkspace(root, path) {
			return true
		}
	}
	return false
}

func looksLikeBrowserScreenshotWithoutPath(command string) bool {
	fields := strings.Fields(command)
	if !containsAgentBrowser(fields) {
		return false
	}
	for i, field := range fields {
		name := strings.ToLower(strings.Trim(field, `"'`))
		if name != "screenshot" {
			continue
		}
		for _, rest := range fields[i+1:] {
			token := strings.Trim(rest, `"'`)
			if token == "" || strings.HasPrefix(token, "-") {
				continue
			}
			return false
		}
		return true
	}
	return false
}

func appendScreenshotPathHint(command, result string) string {
	if !looksLikeBrowserScreenshotWithoutPath(command) {
		return result
	}
	if strings.Contains(result, screenshotPathHint) {
		return result
	}
	if strings.TrimSpace(result) == "" {
		return screenshotPathHint
	}
	return result + "\n" + screenshotPathHint
}

func browserOpenTarget(fields []string) (index int, target string) {
	if !containsAgentBrowser(fields) {
		return -1, ""
	}
	for i, field := range fields {
		switch strings.ToLower(strings.Trim(field, `"'`)) {
		case "open", "goto", "navigate":
			return nextNonFlagField(fields, i+1)
		}
	}
	return -1, ""
}

func browserScreenshotDest(fields []string) (index int, dest string) {
	if !containsAgentBrowser(fields) {
		return -1, ""
	}
	for i, field := range fields {
		if strings.ToLower(strings.Trim(field, `"'`)) != "screenshot" {
			continue
		}
		return nextNonFlagField(fields, i+1)
	}
	return -1, ""
}

func nextNonFlagField(fields []string, start int) (int, string) {
	for i := start; i < len(fields); i++ {
		token := strings.Trim(fields[i], `"'`)
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		return i, token
	}
	return -1, ""
}

func containsAgentBrowser(fields []string) bool {
	for _, field := range fields {
		name := strings.ToLower(strings.Trim(field, `"'`))
		if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
			name = name[i+1:]
		}
		if name == "agent-browser" || name == "agent-browser.exe" {
			return true
		}
	}
	return false
}

func hasURLScheme(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	i := strings.Index(token, ":")
	if i <= 0 {
		return false
	}
	for _, r := range token[:i] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func fileURLTokens(command string) []string {
	var tokens []string
	for _, candidate := range []string{command, stripShellQuotes(command)} {
		for _, token := range sensitivePathTokens(candidate) {
			if strings.HasPrefix(strings.ToLower(token), "file:") {
				tokens = append(tokens, strings.Trim(token, `"'`))
			}
		}
	}
	return tokens
}

func fileURLPath(token string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(token)), "file:") {
		return "", false
	}
	parsed, err := url.Parse(token)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return "", false
	}
	path := parsed.Path
	if path == "" {
		path = parsed.Host
	}
	if path == "" || path == "/" {
		return "", false
	}
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/")
		if parsed.Host != "" && len(parsed.Host) == 1 {
			path = parsed.Host + ":" + path
		}
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		return "", false
	}
	return path, true
}

func workspaceFileURL(abs string) string {
	if strings.TrimSpace(abs) == "" {
		return ""
	}
	path := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func quoteShellToken(value string) string {
	if strings.ContainsAny(value, " \t\"'") {
		return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
	}
	return `'` + value + `'`
}
