//go:build darwin || linux

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"gxx/internal/agent"
	"gxx/internal/approval"
)

func (r *Registry) runCommandSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name:        "run_command",
			Description: "Run a shell command in the workspace with a finite timeout and sanitized environment. Requires user approval.",
			ReadOnly:    false,
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

type runCommandArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

func (r *Registry) previewRunCommand(raw json.RawMessage) (approval.Action, error) {
	args, _, err := r.parseCommandArgs(raw)
	if err != nil {
		return approval.Action{}, err
	}
	return approval.Action{
		Title:   "Run command in " + r.workspace.Root(),
		Preview: "$ " + args.Command,
	}, nil
}

func (r *Registry) runCommand(ctx context.Context, raw json.RawMessage) (string, error) {
	args, timeout, err := r.parseCommandArgs(raw)
	if err != nil {
		return "", err
	}

	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.Command("/bin/sh", "-c", args.Command)
	command.Dir = r.workspace.Root()
	command.Env = sanitizedEnvironment(os.Environ())
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second

	output := &limitedWriter{limit: r.maxResultBytes * 2}
	command.Stdout = output
	command.Stderr = output

	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}

	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()

	select {
	case err := <-waited:
		// The shell may have launched background children and exited. They
		// remain in its process group, so terminate the group before returning.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		text := output.String()
		if err != nil {
			return text, fmt.Errorf("command failed: %w", err)
		}
		if strings.TrimSpace(text) == "" {
			return "Command completed successfully.", nil
		}
		return text, nil
	case <-commandContext.Done():
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-waited
		if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
			return output.String(), fmt.Errorf("command timed out after %s", timeout)
		}
		return output.String(), commandContext.Err()
	}
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
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && isSensitiveEnvironmentName(name) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func isSensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	switch upper {
	case "OPENAI_API_KEY", "GH_TOKEN", "GITHUB_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS",
		"BASH_ENV", "CDPATH", "ENV", "SHELLOPTS", "ZDOTDIR":
		return true
	}
	if strings.HasPrefix(upper, "AWS_") {
		return true
	}
	for _, suffix := range []string{"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_PRIVATE_KEY"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
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
