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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gxx/internal/agent"
	"gxx/internal/approval"
)

// exitCodeLabel prefixes the result of a command that ran but exited non-zero.
// internal/ui matches it to show the code on the tool line.
const exitCodeLabel = "exit code"

func (r *Registry) runCommandSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name: "run_command",
			Description: "Run a shell command in the workspace with a finite timeout and sanitized environment. Requires user approval. " +
				"Commands that name sensitive paths such as .env, keys, or credentials are refused. " +
				"Commands that leave the workspace with .. or an absolute path, or that pipe a download into a shell, are refused. " +
				"The process and its children are killed when the command returns, so a background server does not survive the next tool call. " +
				"A workspace-relative path after agent-browser open is rewritten to a file:// URL of that workspace file. " +
				"A workspace-relative path after agent-browser screenshot is rewritten to that file in the workspace. " +
				"A command that exits non-zero is a failed command, returned as \"exit code N\" followed by its output; only a command that could not be started or that timed out is a tool error. " +
				commandShellDescription(),
			ReadOnly: false,
			Parameters: objectSchema(map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute.",
				},
				"timeout_seconds": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Requested timeout in seconds, or null for the configured default. Cannot exceed the configured default.",
				},
			}, "command", "timeout_seconds"),
		},
		preview: r.previewRunCommand,
		run:     r.runCommand,
	}
}

func commandShellDescription() string {
	if runtime.GOOS == "windows" {
		return "Commands run under PowerShell (-NoProfile, -NonInteractive). This is not bash; use PowerShell syntax. Profiles are not loaded, so interpreters installed only through a profile are not on PATH."
	}
	return "Commands run under /bin/sh, which is neither a login nor an interactive shell, so interpreters installed through version managers such as nvm are not on PATH."
}

type runCommandArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func (r *Registry) previewRunCommand(raw json.RawMessage) (approval.Action, error) {
	args, _, err := r.parseCommandArgs(raw)
	if err != nil {
		return approval.Action{}, err
	}
	preview := "$ " + args.Command
	notes := commandRiskNotes(args.Command)
	if len(notes) > 0 {
		preview += "\n" + strings.Join(notes, "\n")
	}
	repeat := args.Command
	if len(notes) > 0 {
		repeat = ""
	}
	return approval.Action{
		Title:     "Run command in " + r.workspace.Root(),
		Preview:   approval.CapPreview(preview),
		Kind:      approval.KindCommand,
		RepeatKey: repeat,
	}, nil
}

func (r *Registry) parseCommandArgs(raw json.RawMessage) (runCommandArgs, time.Duration, error) {
	var args runCommandArgs
	if err := decodeArgs(raw, &args); err != nil {
		return args, 0, err
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return args, 0, errors.New("command cannot be empty")
	}
	if hasSensitivePathToken(args.Command) {
		return args, 0, errors.New("refusing to run command that may touch a sensitive path")
	}
	if hasParentDirectoryPath(args.Command) {
		return args, 0, errors.New("refusing to run command that leaves the workspace (..)")
	}
	args.Command = rewriteLocalBrowserCommand(r.workspace, args.Command)
	root := ""
	if r.workspace != nil {
		root = r.workspace.Root()
	}
	if root != "" && hasEscapingFileURL(root, args.Command) {
		return args, 0, errors.New("refusing to run command with a file:// URL outside the workspace")
	}
	if hasEscapingAbsolutePath(root, args.Command) {
		return args, 0, errors.New("refusing to run command with an absolute path; stay inside the workspace")
	}
	if pipesToShell(args.Command) {
		return args, 0, errors.New("refusing to pipe a download into a shell")
	}

	timeout := r.commandTimeout
	if args.TimeoutSeconds != nil {
		if *args.TimeoutSeconds < 1 {
			return args, 0, errors.New("timeout_seconds must be positive")
		}
		requested := time.Duration(*args.TimeoutSeconds) * time.Second
		if requested < timeout {
			timeout = requested
		}
	}
	return args, timeout, nil
}

func sanitizedEnvironment(environment []string) []string {
	home := ""
	userProfile := ""
	present := make(map[string]bool, len(environment))
	filtered := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if strings.EqualFold(name, "HOME") {
			home = value
		}
		if strings.EqualFold(name, "USERPROFILE") {
			userProfile = value
		}
		if isSensitiveEnvironmentName(name) {
			continue
		}
		filtered = append(filtered, entry)
		present[name] = true
	}
	if home == "" {
		home = userProfile
	}
	if home != "" {
		if !present["GOMODCACHE"] {
			filtered = append(filtered, "GOMODCACHE="+filepath.Join(home, "go", "pkg", "mod"))
		}
		if !present["GOCACHE"] {
			filtered = append(filtered, "GOCACHE="+defaultGoCache(home))
		}
	}
	if !present["GIT_TERMINAL_PROMPT"] {
		filtered = append(filtered, "GIT_TERMINAL_PROMPT=0")
	}
	return augmentSkillPath(filtered)
}

// augmentSkillPath prepends user-local bin dirs (npm, cargo, go, gxx) when
// they exist, so skill CLIs installed outside a PowerShell profile stay on PATH.
func augmentSkillPath(env []string) []string {
	extras := existingSkillBins(env)
	if len(extras) == 0 {
		return env
	}
	key, value, index := lookupEnv(env, "PATH")
	merged := prependUniquePath(value, extras)
	if index >= 0 {
		env[index] = key + "=" + merged
		return env
	}
	return append(env, "PATH="+merged)
}

func existingSkillBins(env []string) []string {
	_, home, _ := lookupEnv(env, "HOME")
	_, profile, _ := lookupEnv(env, "USERPROFILE")
	_, appdata, _ := lookupEnv(env, "APPDATA")
	_, local, _ := lookupEnv(env, "LOCALAPPDATA")
	var candidates []string
	for _, root := range []string{home, profile} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, ".local", "bin"),
			filepath.Join(root, ".cargo", "bin"),
			filepath.Join(root, "go", "bin"),
		)
	}
	if appdata != "" {
		candidates = append(candidates, filepath.Join(appdata, "npm"))
	}
	if local != "" {
		candidates = append(candidates, filepath.Join(local, "npm"), filepath.Join(local, "gxx"))
	}
	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		if dir == "" || seen[dir] {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

func lookupEnv(env []string, name string) (key, value string, index int) {
	index = -1
	for i, entry := range env {
		k, v, found := strings.Cut(entry, "=")
		if !found || !strings.EqualFold(k, name) {
			continue
		}
		return k, v, i
	}
	return name, "", -1
}

func prependUniquePath(path string, extras []string) string {
	parts := splitPathList(path)
	have := make(map[string]bool, len(parts)+len(extras))
	for _, part := range parts {
		have[pathKey(part)] = true
	}
	prefix := make([]string, 0, len(extras))
	for _, extra := range extras {
		key := pathKey(extra)
		if extra == "" || have[key] {
			continue
		}
		have[key] = true
		prefix = append(prefix, extra)
	}
	if len(prefix) == 0 {
		return path
	}
	if strings.TrimSpace(path) == "" {
		return strings.Join(prefix, string(os.PathListSeparator))
	}
	return strings.Join(prefix, string(os.PathListSeparator)) + string(os.PathListSeparator) + path
}

func splitPathList(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return strings.Split(path, string(os.PathListSeparator))
}

func pathKey(value string) string {
	cleaned := strings.TrimRight(strings.TrimSpace(value), `/\`)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func withMissingCommandHint(command, result string) string {
	result = appendMissingCommandHint(command, result)
	result = appendDeadChildHint(result)
	return appendScreenshotPathHint(command, result)
}

func appendMissingCommandHint(command, result string) string {
	if !looksLikeMissingCommand(result) {
		return result
	}
	// Successful scripts can still print "is not recognized" for a later
	// statement. Only suggest npx when the command actually failed.
	if !commandFailed(result) {
		return result
	}
	name := missingCommandName(command, result)
	if name == "" || skipMissingCommandHint(name) {
		return result
	}
	if strings.Contains(result, "npx --yes ") {
		return result
	}
	return result + "\n" + fmt.Sprintf(
		"Command %q was not found on PATH. If this is an npm CLI named by a skill, retry with: npx --yes %s <same arguments>",
		name,
		name,
	)
}

func commandFailed(result string) bool {
	return strings.HasPrefix(strings.TrimSpace(result), exitCodeLabel+" ")
}

func missingCommandName(command, result string) string {
	if name := unrecognizedCommandName(result); name != "" {
		return name
	}
	return firstCommandWord(command)
}

func unrecognizedCommandName(result string) string {
	lower := strings.ToLower(result)
	for _, prefix := range []string{"the term '", "the term \""} {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		rest := result[idx+len(prefix):]
		end := strings.IndexAny(rest, `'"`)
		if end <= 0 {
			continue
		}
		return strings.ToLower(strings.TrimSpace(rest[:end]))
	}
	return ""
}

func appendDeadChildHint(result string) string {
	if !looksLikeConnectionRefused(result) {
		return result
	}
	const hint = "Local servers from a previous run_command do not stay running; that command's process tree is killed when it returns. Open a workspace-relative path (for example: agent-browser open index.html) or start the server and browse in the same command."
	if strings.Contains(result, hint) {
		return result
	}
	return result + "\n" + hint
}

func looksLikeConnectionRefused(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "err_connection_refused") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "econnrefused")
}

func skipMissingCommandHint(name string) bool {
	switch strings.ToLower(name) {
	case "npx", "npm", "pnpm", "yarn", "bun", "node", "python", "python3", "py", "pip", "go", "git",
		"pwsh", "powershell", "cmd", "sh", "bash",
		"which", "where", "where.exe", "ls", "cat", "pwd", "true", "false",
		"identify", "convert", "magick", "sips", "file", "exiftool", "ffmpeg", "ffprobe":
		return true
	default:
		return false
	}
}

func looksLikeMissingCommand(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "is not recognized") ||
		strings.Contains(lower, "command not found") ||
		strings.Contains(lower, "commandnotfoundexception") {
		return true
	}
	for _, line := range strings.Split(lower, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), ": not found") {
			return true
		}
	}
	return false
}

func defaultGoCache(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "go-build")
	case "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "go-build")
		}
		return filepath.Join(home, "AppData", "Local", "go-build")
	default:
		return filepath.Join(home, ".cache", "go-build")
	}
}

func isSensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	switch upper {
	case "OPENAI_API_KEY", "OPENAI_ADMIN_KEY", "ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "GH_TOKEN", "GITHUB_TOKEN",
		"GOOGLE_APPLICATION_CREDENTIALS", "BASH_ENV", "BASHOPTS", "CDPATH", "ENV",
		"SHELLOPTS", "ZDOTDIR", "IFS", "PROMPT_COMMAND",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH",
		"NODE_OPTIONS", "NODE_PATH", "PYTHONSTARTUP", "PERL5OPT", "RUBYOPT",
		"JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS", "SSLKEYLOGFILE", "GIT_ASKPASS",
		"SSH_ASKPASS", "DOCKER_CONFIG":
		return true
	}
	if strings.HasPrefix(upper, "AWS_") {
		return true
	}
	for _, suffix := range []string{"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_PRIVATE_KEY", "_KEY"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}

// reportExit describes a process that ran to completion but exited non-zero.
// That is still a tool result, not a crashed tool: reporting it as IsError
// looks the same as a start/timeout failure. The first line is "exit code N"
// so the UI can mark the command as failed.
func reportExit(exitError *exec.ExitError, text string) string {
	header := fmt.Sprintf("%s %d", exitCodeLabel, exitError.ExitCode())
	if extra := signaledExitLabel(exitError); extra != "" {
		header = extra
	}
	if strings.TrimSpace(text) == "" {
		return header + "\nCommand produced no output."
	}
	return header + "\n" + text
}

type limitedWriter struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - len(w.data)
	if remaining > 0 {
		w.data = append(w.data, data[:min(len(data), remaining)]...)
	}
	if len(data) > remaining {
		w.truncated = true
	}
	return len(data), nil
}

func (w *limitedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	value := string(w.data)
	if w.truncated {
		value += "\n… command output capture limit reached"
	}
	return value
}
