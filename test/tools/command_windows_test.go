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

//go:build windows

package tools_test

import (
	"context"
	"io"
	"os"
	"os/exec"
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

	"golang.org/x/sys/windows"
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
			"command":         `if ($null -eq $env:OPENAI_API_KEY -or $env:OPENAI_API_KEY -eq '') { [Console]::Out.Write('unset') } else { [Console]::Out.Write($env:OPENAI_API_KEY) }`,
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

func TestRunCommandKeepsMarkerAndScrubsSecrets(t *testing.T) {
	t.Setenv("OPENAI_ADMIN_KEY", "admin-secret")
	t.Setenv("GXX_KEEP", "keep-me")
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
			"command":         `$admin = if ($env:OPENAI_ADMIN_KEY) { $env:OPENAI_ADMIN_KEY } else { 'unset' }; $keep = if ($env:GXX_KEEP) { $env:GXX_KEEP } else { 'unset' }; $enc = if ($env:ENCRYPTION_KEY) { $env:ENCRYPTION_KEY } else { 'unset' }; [Console]::Out.Write("$admin $keep $enc")`,
			"timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if result.Output != "unset keep-me unset" {
		t.Fatalf("command output = %q, want unset keep-me unset", result.Output)
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
			"command":         `[Console]::Error.WriteLine('syntax error on line 1'); exit 3`,
			"timeout_seconds": nil,
		}),
	}, nil)[0]

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
			"command":         "Start-Sleep -Seconds 5",
			"timeout_seconds": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "timed out") {
		t.Fatalf("result = %+v, want timeout error", result)
	}
	if result.Duration > 3*time.Second {
		t.Fatalf("command cancellation took %s", result.Duration)
	}
}

func TestJobObjectKillsAssignedProcess(t *testing.T) {
	job, err := tools.NewKillJob()
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	defer tools.CloseJob(job)

	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "ping.exe")
	command := exec.Command(ping, "-n", "30", "127.0.0.1")
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start ping: %v", err)
	}
	if err := tools.AssignPIDToJob(job, command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		t.Fatalf("assign process: %v", err)
	}
	if err := tools.ResumeProcess(command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		t.Fatalf("resume process: %v", err)
	}
	if err := tools.TerminateJob(job); err != nil {
		t.Fatalf("terminate job: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !windowsProcessExists(command.Process.Pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("assigned process %d survived job termination", command.Process.Pid)
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
			"command": "exit 0", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if len(approver.actions) != 1 || approver.actions[0].Kind != approval.KindCommand {
		t.Fatalf("approval actions = %#v", approver.actions)
	}
	if approver.actions[0].RepeatKey != "exit 0" {
		t.Fatalf("RepeatKey = %q, want exit 0", approver.actions[0].RepeatKey)
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
			"command": "exit 0", "timeout_seconds": nil,
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
			"command": "[Console]::Out.Write('ran')", "timeout_seconds": nil,
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

func TestRunCommandPreviewWarnsOnSensitivePath(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	_ = registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "Get-Content .env", "timeout_seconds": nil,
		}),
	}, nil)
	if len(approver.actions) != 1 {
		t.Fatalf("actions = %#v", approver.actions)
	}
	if !strings.Contains(approver.actions[0].Preview, "sensitive path") {
		t.Fatalf("preview = %q, want sensitive path warning", approver.actions[0].Preview)
	}
	if approver.actions[0].RepeatKey != "Get-Content .env" {
		t.Fatalf("RepeatKey = %q", approver.actions[0].RepeatKey)
	}
}

func TestRunCommandDoesNotLoadCallerProfiles(t *testing.T) {
	root := t.TempDir()
	marker := root + `\profile-loaded`
	profile := root + `\profile.ps1`
	if err := os.WriteFile(profile, []byte("Set-Content -Path '"+marker+"' -Value loaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", root)
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": "exit 0", "timeout_seconds": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("run command failed: %s", result.Output)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("PowerShell profile was loaded; marker stat error = %v", err)
	}
}

func TestResumeProcessStartsSuspendedProcess(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "ping.exe")
	command := exec.Command(ping, "-n", "1", "127.0.0.1")
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_SUSPENDED,
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start ping: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("suspended process exited: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := tools.ResumeProcess(command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		t.Fatalf("resume process: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait after resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("process did not exit after resume")
	}
}

func TestRunCommandTimeoutKillsSpawnedChild(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "ping.exe")
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  800 * time.Millisecond,
	})

	script := `$psi = New-Object System.Diagnostics.ProcessStartInfo; $psi.FileName = '` + ping + `'; $psi.Arguments = '-n 40 127.0.0.1'; $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true; $p = [Diagnostics.Process]::Start($psi); Set-Content -Path 'child.pid' -Value $p.Id; $p.WaitForExit()`
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("command", "run_command", map[string]any{
			"command": script, "timeout_seconds": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "timed out") {
		t.Fatalf("result = %+v, want timeout error", result)
	}

	pidPath := filepath.Join(root, "child.pid")
	deadline := time.Now().Add(2 * time.Second)
	var pidBytes []byte
	var err error
	for time.Now().Before(deadline) {
		pidBytes, err = os.ReadFile(pidPath)
		if err == nil && strings.TrimSpace(string(pidBytes)) != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if convErr != nil {
		t.Fatalf("child pid %q: %v", pidBytes, convErr)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !windowsProcessExists(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("spawned child %d survived command timeout", pid)
}

func windowsProcessExists(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
