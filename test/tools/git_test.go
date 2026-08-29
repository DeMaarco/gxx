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
	"runtime"
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

func TestGitToolsOmitSensitivePaths(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "hello\n")
	writeTestFile(t, root, ".env", "API_TOKEN=very-secret\n")
	initGitRepo(t, root)
	runGit(t, root, "add", "README.md", ".env")
	runGit(t, root, "commit", "-m", "initial")
	writeTestFile(t, root, "README.md", "hello world\n")
	writeTestFile(t, root, ".env", "API_TOKEN=changed\n")

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
	if strings.Contains(status.Output, ".env") || strings.Contains(status.Output, "very-secret") || strings.Contains(status.Output, "changed") {
		t.Fatalf("status leaked a sensitive path: %q", status.Output)
	}
	if !strings.Contains(status.Output, "omitted by gxx") {
		t.Fatalf("status = %q, want omitted notice", status.Output)
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
	if strings.Contains(diff.Output, ".env") || strings.Contains(diff.Output, "API_TOKEN") {
		t.Fatalf("diff leaked a sensitive path: %q", diff.Output)
	}

	targeted := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("diff", "git_diff", map[string]any{"path": ".env", "staged": nil}),
	}, nil)[0]
	if !targeted.IsError || !strings.Contains(targeted.Output, "sensitive path") {
		t.Fatalf("git_diff .env = %+v, want sensitive-path error", targeted)
	}

	runGit(t, root, "checkout", "--", ".env", "README.md")
	runGit(t, root, "mv", ".env", "leaked.txt")
	renamed := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("diff", "git_diff", map[string]any{"path": nil, "staged": true}),
	}, nil)[0]
	if renamed.IsError {
		t.Fatalf("git_diff rename failed: %s", renamed.Output)
	}
	if strings.Contains(renamed.Output, "API_TOKEN") || strings.Contains(renamed.Output, "very-secret") {
		t.Fatalf("rename diff leaked secret: %q", renamed.Output)
	}
	if !strings.Contains(renamed.Output, "omitted by gxx") {
		t.Fatalf("rename diff = %q, want omitted notice", renamed.Output)
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

func TestPathInsideWorkspaceHandlesWindowsCase(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "src", "main.go")
	if !tools.PathInsideWorkspace(root, child) {
		t.Fatalf("child %q should be inside %q", child, root)
	}
	if tools.PathInsideWorkspace(root, root+"-other") {
		t.Fatalf("sibling %q should not be inside %q", root+"-other", root)
	}
	if runtime.GOOS == "windows" {
		mixed := strings.ToUpper(root)
		if !tools.PathInsideWorkspace(root, mixed) {
			t.Fatalf("case-folded root %q should be inside %q", mixed, root)
		}
		if !tools.PathInsideWorkspace(root, filepath.Join(mixed, "src")) {
			t.Fatalf("case-folded child should be inside %q", root)
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
