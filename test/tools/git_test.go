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

package tools_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gxx/internal/tools"

	"gxx/internal/agent"
)

func TestGitToolsInspectWorkspaceRepo(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "hello\n")
	initGitRepo(t, root)
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")
	writeTestFile(t, root, "README.md", "hello world\n")

	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	status := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("status", "git_status", map[string]any{}),
	}, nil)[0]
	if status.IsError {
		t.Fatalf("git_status failed: %s", status.Output)
	}
	if !strings.Contains(status.Output, "README.md") {
		t.Fatalf("status = %q, want README.md", status.Output)
	}

	diff := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("diff", "git_diff", map[string]any{"path": nil, "staged": nil}),
	}, nil)[0]
	if diff.IsError {
		t.Fatalf("git_diff failed: %s", diff.Output)
	}
	if !strings.Contains(diff.Output, "hello world") {
		t.Fatalf("diff = %q, want working tree change", diff.Output)
	}

	logResult := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("log", "git_log", map[string]any{"max_count": nil}),
	}, nil)[0]
	if logResult.IsError {
		t.Fatalf("git_log failed: %s", logResult.Output)
	}
	if !strings.Contains(logResult.Output, "initial") {
		t.Fatalf("log = %q, want initial commit", logResult.Output)
	}
}

func TestGitToolsRejectRepoOutsideWorkspace(t *testing.T) {
	requireGit(t)
	outside := t.TempDir()
	initGitRepo(t, outside)
	writeTestFile(t, outside, "secret.txt", "token\n")
	runGit(t, outside, "add", "secret.txt")
	runGit(t, outside, "commit", "-m", "secret")

	root := t.TempDir()
	gitdir := filepath.Join(outside, ".git")
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("status", "git_status", map[string]any{}),
	}, nil)[0]
	if !result.IsError {
		t.Fatalf("git_status succeeded for an outside repo: %q", result.Output)
	}
	if !strings.Contains(result.Output, "outside the workspace") {
		t.Fatalf("output = %q, want outside workspace", result.Output)
	}
	if strings.Contains(result.Output, "token") {
		t.Fatalf("outside repo content leaked: %q", result.Output)
	}
}

func TestPlanModeIncludesGitTools(t *testing.T) {
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{}, tools.Options{
		MaxResultBytes:  1024,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	registry.SetPlan(true)
	found := map[string]bool{}
	for _, def := range registry.Definitions() {
		found[def.Name] = true
		if !def.ReadOnly {
			t.Fatalf("plan mode exposed writable tool %s", def.Name)
		}
	}
	for _, name := range []string{"git_status", "git_diff", "git_log"} {
		if !found[name] {
			t.Fatalf("plan mode hid %s", name)
		}
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "gxx@example.com")
	runGit(t, root, "config", "user.name", "gxx")
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=gxx",
		"GIT_AUTHOR_EMAIL=gxx@example.com",
		"GIT_COMMITTER_NAME=gxx",
		"GIT_COMMITTER_EMAIL=gxx@example.com",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s (%v)", strings.Join(args, " "), output, err)
	}
}
