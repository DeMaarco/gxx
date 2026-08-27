//go:build darwin || linux

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"gxx/internal/agent"
)

func TestRunCommandScrubsProviderCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "super-secret")
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, Options{
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

func TestRunCommandHonorsTimeout(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, Options{
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
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, Options{
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
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, Options{
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
