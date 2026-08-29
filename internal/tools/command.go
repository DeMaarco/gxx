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
				"A command that exits non-zero is a normal result, returned as \"exit code N\" followed by its output; only a command that could not be started or that timed out is a tool error. " +
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
	if notes := commandRiskNotes(args.Command); len(notes) > 0 {
		preview += "\n" + strings.Join(notes, "\n")
	}
	return approval.Action{
		Title:     "Run command in " + r.workspace.Root(),
		Preview:   approval.CapPreview(preview),
		Kind:      approval.KindCommand,
		RepeatKey: args.Command,
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
	return filtered
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
	case "OPENAI_API_KEY", "OPENAI_ADMIN_KEY", "GH_TOKEN", "GITHUB_TOKEN",
		"GOOGLE_APPLICATION_CREDENTIALS", "BASH_ENV", "CDPATH", "ENV", "SHELLOPTS",
		"ZDOTDIR":
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
// That is an answer, not a tool failure. Reporting it as an error tells the
// model the same thing a crashed tool would, and leaves the exit code behind
// the first line of output where the renderer drops it.
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
