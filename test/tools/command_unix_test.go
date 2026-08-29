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

//go:build darwin || linux

package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gxx/internal/tools"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/config"
)

func TestRunCommandScrubsProviderCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "super-secret")
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command":         `printf '%s' "${OPENAI_API_KEY-unset}"`,
			"timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if result.Output != "unset" {
		t.Fatalf("command output = %q, want unset", result.Output)
	}
}

func TestRunCommandKeepsHomeAndScrubsSecrets(t *testing.T) {
	t.Setenv("OPENAI_ADMIN_KEY", "admin-secret")
	t.Setenv("HOME", "/tmp/gxx-home-should-not-leak")
	t.Setenv("ENCRYPTION_KEY", "enc-secret")
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command":         `printf '%s %s %s' "${OPENAI_ADMIN_KEY-unset}" "${HOME-unset}" "${ENCRYPTION_KEY-unset}"`,
			"timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if result.Output != "unset /tmp/gxx-home-should-not-leak unset" {
		t.Fatalf("command output = %q, want unset home unset", result.Output)
	}
}

func TestRunCommandReportsNonZeroExitAsResult(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command":         "printf 'syntax error on line 1\\n' >&2; exit 3",
			"timeout_seconds": nil,
		}),
	}, nil)[0]

	// A command that runs and fails is an answer, not a broken tool.
	if result.IsError {
		t.Fatalf("non-zero exit reported as a tool error: %q", result.Output)
	}
	if !strings.HasPrefix(result.Output, tools.ExitCodeLabel+" 3") {
		t.Fatalf("output = %q, want an %q header", result.Output, tools.ExitCodeLabel)
	}
	if !strings.Contains(result.Output, "syntax error on line 1") {
		t.Fatalf("output = %q, want the command output preserved", result.Output)
	}
}

func TestRunCommandReportsMissingInterpreter(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command":         "gxx-no-such-binary --check",
			"timeout_seconds": nil,
		}),
	}, nil)[0]

	if result.IsError {
		t.Fatalf("missing binary reported as a tool error: %q", result.Output)
	}
	if !strings.HasPrefix(result.Output, tools.ExitCodeLabel+" 127") {
		t.Fatalf("output = %q, want exit code 127", result.Output)
	}
}

func TestRunCommandKeepsExitCodeWhenCommandIsSilent(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "exit 1", "timeout_seconds": nil,
		}),
	}, nil)[0]

	if result.IsError || !strings.HasPrefix(result.Output, tools.ExitCodeLabel+" 1") {
		t.Fatalf("result = %+v, want a silent failure to still report its code", result)
	}
}

func TestRunCommandHonorsTimeout(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  100 * time.Millisecond,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command":         "sleep 5",
			"timeout_seconds": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "timed out") {
		t.Fatalf("result = %+v, want timeout error", result)
	}
	if result.Duration > time.Second {
		t.Fatalf("command cancellation took %s", result.Duration)
	}
}

func TestRunCommandDoesNotLoadCallerShellProfiles(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "profile-loaded")
	profile := filepath.Join(root, "profile.sh")
	if err := os.WriteFile(profile, []byte("touch "+marker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", profile)
	t.Setenv("ENV", profile)
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "true", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("shell profile was loaded; marker stat error = %v", err)
	}
}

func TestRunCommandKillsBackgroundProcessGroup(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "sleep 10 >/dev/null 2>&1 & echo $!", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(result.Output))
	if err != nil {
		t.Fatalf("parse child pid from %q: %v", result.Output, err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background process %d survived command completion", pid)
}

func TestRunCommandReportsCommandKind(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "true", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if len(approver.actions) != 1 || approver.actions[0].Kind != approval.KindCommand {
		t.Fatalf("approval actions = %#v", approver.actions)
	}
	if approver.actions[0].RepeatKey != "true" {
		t.Fatalf("RepeatKey = %q, want true", approver.actions[0].RepeatKey)
	}
}

func TestAutoWritesStillAsksForCommands(t *testing.T) {
	root := t.TempDir()
	inner := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approval.NewPolicy(config.PermissionAutoWrites, inner), tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "true", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "permission denied") {
		t.Fatalf("result = %+v, want command prompt denial", result)
	}
	if len(inner.actions) != 1 || inner.actions[0].Kind != approval.KindCommand {
		t.Fatalf("inner actions = %#v", inner.actions)
	}
}

func TestAutoRunsCommandsWithoutPrompt(t *testing.T) {
	root := t.TempDir()
	inner := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approval.NewPolicy(config.PermissionAuto, inner), tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "printf ran", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if result.Output != "ran" {
		t.Fatalf("output = %q, want ran", result.Output)
	}
	if len(inner.actions) != 0 {
		t.Fatalf("auto prompted for a command: %#v", inner.actions)
	}
}

func TestRunCommandPreviewRejectsSensitivePath(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "cat .env", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "sensitive path") {
		t.Fatalf("result = %+v, want sensitive-path refusal", result)
	}
	if len(approver.actions) != 0 {
		t.Fatalf("sensitive command was prompted: %#v", approver.actions)
	}
}
