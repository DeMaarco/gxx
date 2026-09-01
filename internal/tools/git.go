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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gxx/internal/agent"
)

const (
	defaultGitLogCount = 20
	maxGitLogCount     = 50
	maxGitTimeout      = 30 * time.Second
)

func (r *Registry) gitStatusSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "git_status",
			Description: "Show git status for the workspace repository (porcelain). Read-only. Sensitive paths are omitted. Fails if the git root or git dir is outside the workspace.",
			ReadOnly:    true,
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []any{},
				"additionalProperties": false,
			},
		},
		run: r.gitStatus,
	}
}

func (r *Registry) gitDiffSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "git_diff",
			Description: "Show git diff for the workspace repository. Optional path limits the diff. staged true uses the index. Read-only. Sensitive paths are omitted.",
			ReadOnly:    true,
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        []string{"string", "null"},
					"description": "Workspace-relative file or directory, or null for the whole repository.",
				},
				"staged": map[string]any{
					"type":        []string{"boolean", "null"},
					"description": "If true, show staged diff. Null or false is the working tree.",
				},
			}, "path", "staged"),
		},
		run: r.gitDiff,
	}
}

func (r *Registry) gitLogSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "git_log",
			Description: "Show recent git commits as one line each. Read-only.",
			ReadOnly:    true,
			Parameters: objectSchema(map[string]any{
				"max_count": map[string]any{
					"type":        []string{"integer", "null"},
					"description": "Number of commits from 1 to 50, or null for 20.",
				},
			}, "max_count"),
		},
		run: r.gitLog,
	}
}

func (r *Registry) gitStatus(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := decodeEmptyGitArgs(raw); err != nil {
		return "", err
	}
	output, err := r.runGit(ctx, "status", "--porcelain=v1", "-uall")
	if err != nil {
		return "", err
	}
	filtered, omitted := filterPorcelainStatus(output)
	if strings.TrimSpace(filtered) == "" {
		if omitted > 0 {
			return omittedSensitiveNotice(omitted), nil
		}
		return "No changes.", nil
	}
	if omitted > 0 {
		return filtered + "\n" + omittedSensitiveNotice(omitted), nil
	}
	return filtered, nil
}

type gitDiffArgs struct {
	Path   *string `json:"path"`
	Staged *bool   `json:"staged"`
}

func (r *Registry) gitDiff(ctx context.Context, raw json.RawMessage) (string, error) {
	var args gitDiffArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	staged := optionalBool(args.Staged, false)
	var clean string
	if path := optionalString(args.Path, ""); path != "" {
		var err error
		clean, err = r.workspace.Clean(path)
		if err != nil {
			return "", err
		}
		if isSensitivePath(clean) {
			return "", refuseSensitive("diff", path)
		}
	}
	kept, omitted, err := r.gitDiffNames(ctx, staged, clean)
	if err != nil {
		return "", err
	}
	if len(kept) == 0 {
		if omitted > 0 {
			return omittedSensitiveNotice(omitted), nil
		}
		return "No diff.", nil
	}
	command := []string{"diff"}
	if staged {
		command = append(command, "--cached")
	}
	command = append(command, "--")
	command = append(command, kept...)
	output, err := r.runGit(ctx, command...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		if omitted > 0 {
			return omittedSensitiveNotice(omitted), nil
		}
		return "No diff.", nil
	}
	if omitted > 0 {
		return output + "\n" + omittedSensitiveNotice(omitted), nil
	}
	return output, nil
}

func (r *Registry) gitDiffNames(ctx context.Context, staged bool, path string) ([]string, int, error) {
	command := []string{"diff", "--name-status", "-z"}
	if staged {
		command = append(command, "--cached")
	}
	if path != "" {
		command = append(command, "--", path)
	}
	output, err := r.runGit(ctx, command...)
	if err != nil {
		return nil, 0, err
	}
	kept, omitted := filterGitNameStatus(output)
	return kept, omitted, nil
}

type gitLogArgs struct {
	MaxCount *int `json:"max_count"`
}

func (r *Registry) gitLog(ctx context.Context, raw json.RawMessage) (string, error) {
	var args gitLogArgs
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	count := optionalInt(args.MaxCount, defaultGitLogCount)
	if count < 1 || count > maxGitLogCount {
		return "", fmt.Errorf("max_count must be between 1 and %d", maxGitLogCount)
	}
	output, err := r.runGit(ctx, "log", "--oneline", fmt.Sprintf("-n%d", count))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return "No commits.", nil
	}
	return output, nil
}

func decodeEmptyGitArgs(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("tool arguments are empty")
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	if len(args) != 0 {
		return errors.New("invalid tool arguments: unexpected fields")
	}
	return nil
}

func (r *Registry) runGit(ctx context.Context, args ...string) (string, error) {
	if err := r.verifyGitWorkspace(ctx); err != nil {
		return "", err
	}
	return r.execGit(ctx, args...)
}

func (r *Registry) verifyGitWorkspace(ctx context.Context) error {
	toplevel, err := r.execGit(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if !pathInsideWorkspace(r.workspace.Root(), strings.TrimSpace(toplevel)) {
		return errors.New("git repository root is outside the workspace")
	}
	gitDir, err := r.execGit(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	if !pathInsideWorkspace(r.workspace.Root(), strings.TrimSpace(gitDir)) {
		return errors.New("git directory is outside the workspace")
	}
	return nil
}

func (r *Registry) execGit(ctx context.Context, args ...string) (string, error) {
	timeout := r.commandTimeout
	if timeout <= 0 || timeout > maxGitTimeout {
		timeout = maxGitTimeout
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(commandContext, "git", args...)
	command.Dir = r.workspace.Root()
	command.Env = append(sanitizedEnvironment(os.Environ()), "GIT_OPTIONAL_LOCKS=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("git timed out after %s", timeout)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git is not available on PATH")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	reportProgress(ctx, strings.Join(args, " "))
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func pathInsideWorkspace(root, candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return false
	}
	realRoot := evalPath(root)
	realCandidate := evalPath(candidate)
	if sameFilePath(realCandidate, realRoot) {
		return true
	}
	return hasFilePathPrefix(realCandidate, realRoot+string(filepath.Separator))
}

// evalPath resolves symlinks on the longest existing prefix so a missing
// child under /var on macOS still compares equal to a /private/var workspace.
func evalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	rest := ""
	current := abs
	for {
		parent := filepath.Dir(current)
		if parent == current {
			if rest == "" {
				return current
			}
			return filepath.Join(current, rest)
		}
		base := filepath.Base(current)
		if rest == "" {
			rest = base
		} else {
			rest = filepath.Join(base, rest)
		}
		current = parent
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, rest)
		}
	}
}

func sameFilePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func hasFilePathPrefix(path, prefix string) bool {
	if runtime.GOOS == "windows" {
		return len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
	}
	return strings.HasPrefix(path, prefix)
}

func filterGitNameStatus(output string) (kept []string, omitted int) {
	fields := splitGitNull(output)
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			continue
		}
		rename := status[0] == 'R' || status[0] == 'C'
		if rename {
			if i+1 >= len(fields) {
				break
			}
			oldPath, newPath := fields[i], fields[i+1]
			i += 2
			if isSensitivePath(oldPath) || isSensitivePath(newPath) {
				omitted++
				continue
			}
			kept = append(kept, newPath)
			continue
		}
		if i >= len(fields) {
			break
		}
		path := fields[i]
		i++
		if isSensitivePath(path) {
			omitted++
			continue
		}
		kept = append(kept, path)
	}
	return kept, omitted
}

func filterPorcelainStatus(output string) (string, int) {
	if strings.TrimSpace(output) == "" {
		return "", 0
	}
	var kept []string
	omitted := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if porcelainLineSensitive(line) {
			omitted++
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), omitted
}

func porcelainLineSensitive(line string) bool {
	rest := line
	if len(line) >= 3 && line[2] == ' ' {
		rest = line[3:]
	}
	for _, path := range porcelainPaths(rest) {
		if isSensitivePath(path) {
			return true
		}
	}
	return false
}

func porcelainPaths(rest string) []string {
	rest = strings.TrimSpace(rest)
	if left, right, ok := strings.Cut(rest, " -> "); ok {
		return []string{unquoteGitPath(left), unquoteGitPath(right)}
	}
	return []string{unquoteGitPath(rest)}
}

func unquoteGitPath(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func splitGitNull(output string) []string {
	var paths []string
	for _, part := range strings.Split(output, "\x00") {
		if part == "" {
			continue
		}
		paths = append(paths, part)
	}
	return paths
}
