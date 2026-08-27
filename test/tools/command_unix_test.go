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

func TestSanitizedEnvironmentInjectsGoCachesFromHome(t *testing.T) {
	env := tools.SanitizedEnvironment([]string{
		"HOME=/tmp/gxx-home-should-not-leak",
		"PATH=/usr/bin",
		"OPENAI_API_KEY=secret",
	})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HOME=/tmp/gxx-home-should-not-leak") {
		t.Fatalf("HOME missing: %q", env)
	}
	if strings.Contains(joined, "OPENAI_API_KEY=") {
		t.Fatalf("API key leaked: %q", env)
	}
	if !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("GIT_TERMINAL_PROMPT missing: %q", env)
	}
	wantMod := "GOMODCACHE=" + filepath.Join("/tmp/gxx-home-should-not-leak", "go", "pkg", "mod")
	wantCache := "GOCACHE=" + tools.DefaultGoCache("/tmp/gxx-home-should-not-leak")
	if !strings.Contains(joined, wantMod) || !strings.Contains(joined, wantCache) {
		t.Fatalf("env = %q, want %s and %s", env, wantMod, wantCache)
	}
}

func TestSanitizedEnvironmentKeepsExistingGoCaches(t *testing.T) {
	env := tools.SanitizedEnvironment([]string{
		"HOME=/tmp/gxx-home-should-not-leak",
		"GOMODCACHE=/custom/mod",
		"GOCACHE=/custom/cache",
	})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GOMODCACHE=/custom/mod") || !strings.Contains(joined, "GOCACHE=/custom/cache") {
		t.Fatalf("existing Go caches were replaced: %q", env)
	}
	if strings.Count(joined, "GOMODCACHE=") != 1 || strings.Count(joined, "GOCACHE=") != 1 {
		t.Fatalf("duplicate Go cache entries: %q", env)
	}
}

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
