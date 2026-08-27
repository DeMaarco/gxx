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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gxx/internal/tools"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/config"
	"gxx/internal/workspace"
)

type staticApprover struct {
	approved bool
	mu       sync.Mutex
	actions  []approval.Action
}

type approverFunc func(context.Context, approval.Action) (bool, error)

func (f approverFunc) Approve(ctx context.Context, action approval.Action) (bool, error) {
	return f(ctx, action)
}

func (a *staticApprover) Approve(_ context.Context, action approval.Action) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	return a.approved, nil
}

func TestReadToolsReturnUsefulBoundedResults(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/main.go", "package main\n\nfunc main() {}\n")
	writeTestFile(t, root, "README.md", "Hello GXX\nsecond line\n")
	writeTestFile(t, root, "node_modules/ignored.js", "Hello GXX\n")

	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  1024,
		MaxSearchResult: 10,
		ParallelReads:   3,
		CommandTimeout:  time.Second,
	})
	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": nil, "max_depth": 4}),
		toolCall("search", "search_files", map[string]any{"query": "hello gxx", "path": nil, "max_results": nil}),
		toolCall("read", "read_file", map[string]any{"path": "README.md", "offset_line": 2, "limit_lines": 1}),
	}, nil)

	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	for _, result := range results {
		if result.IsError {
			t.Fatalf("%s failed: %s", result.Name, result.Output)
		}
	}
	if !strings.Contains(results[0].Output, "src/main.go") {
		t.Fatalf("list output = %q", results[0].Output)
	}
	if strings.Contains(results[0].Output, "node_modules") {
		t.Fatalf("list output includes ignored directory: %q", results[0].Output)
	}
	if !strings.Contains(results[1].Output, "README.md:1:Hello GXX") {
		t.Fatalf("search output = %q", results[1].Output)
	}
	if strings.Contains(results[1].Output, "ignored.js") {
		t.Fatalf("search output includes ignored file: %q", results[1].Output)
	}
	if !strings.Contains(results[2].Output, "2|second line") {
		t.Fatalf("read output = %q", results[2].Output)
	}
}

func TestMutationRequiresApproval(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	approver := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("edit", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update", "old_text": "before", "new_text": "after",
			}},
		}),
	}, nil)
	if !results[0].IsError || !strings.Contains(results[0].Output, "permission denied") {
		t.Fatalf("result = %+v, want denied error", results[0])
	}
	data, err := os.ReadFile(filepath.Join(root, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before\n" {
		t.Fatalf("file changed without approval: %q", data)
	}
	if len(approver.actions) != 1 ||
		approver.actions[0].Kind != approval.KindWrite ||
		!strings.Contains(approver.actions[0].Preview, "-before") {
		t.Fatalf("approval actions = %#v", approver.actions)
	}
}

func TestWriteFileRejectsExistingPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("write", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "add", "content": "after\n",
			}},
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "cannot add existing") {
		t.Fatalf("result = %+v, want existing-file error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "before\n")
}

func TestListAndSearchHonorGitignore(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".gitignore", "*.secret\nignored/\n")
	writeTestFile(t, root, ".gxxignore", "skip-me.txt\n")
	writeTestFile(t, root, "keep.txt", "hello visible\n")
	writeTestFile(t, root, "hidden.secret", "hello secret\n")
	writeTestFile(t, root, "skip-me.txt", "hello skipped\n")
	writeTestFile(t, root, "ignored/file.txt", "hello ignored\n")

	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": nil, "max_depth": 4}),
		toolCall("search", "search_files", map[string]any{"query": "hello", "path": nil, "max_results": nil}),
	}, nil)
	if results[0].IsError || results[1].IsError {
		t.Fatalf("results = %+v", results)
	}
	if !strings.Contains(results[0].Output, "keep.txt") {
		t.Fatalf("list = %q, want keep.txt", results[0].Output)
	}
	for _, hidden := range []string{"hidden.secret", "skip-me.txt", "ignored/"} {
		if strings.Contains(results[0].Output, hidden) {
			t.Fatalf("list = %q, should omit %s", results[0].Output, hidden)
		}
	}
	if !strings.Contains(results[1].Output, "keep.txt") {
		t.Fatalf("search = %q, want keep.txt", results[1].Output)
	}
	for _, hidden := range []string{"hidden.secret", "skip-me.txt", "ignored/file.txt"} {
		if strings.Contains(results[1].Output, hidden) {
			t.Fatalf("search = %q, should omit %s", results[1].Output, hidden)
		}
	}
}

func TestApprovedWriteAndEditAreAtomic(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("write", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "nested/file.txt", "action": "add", "content": "before\n",
			}},
		}),
		toolCall("edit", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "nested/file.txt", "action": "update", "old_text": "before", "new_text": "after",
			}},
		}),
	}, nil)
	for _, result := range results {
		if result.IsError {
			t.Fatalf("%s failed: %s", result.Name, result.Output)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after\n" {
		t.Fatalf("file contents = %q, want after", data)
	}
}

func TestEditAbortsIfFileChangesAfterPreview(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "file.txt")
	writeTestFile(t, root, "file.txt", "before\n")
	approver := approverFunc(func(_ context.Context, _ approval.Action) (bool, error) {
		if err := os.WriteFile(target, []byte("external change\n"), 0o644); err != nil {
			return false, err
		}
		return true, nil
	})
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("edit", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update", "old_text": "before", "new_text": "after",
			}},
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "changed before transaction") {
		t.Fatalf("result = %+v, want changed-file error", result)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external change\n" {
		t.Fatalf("file contents = %q, external update was overwritten", data)
	}
}

func TestToolOutputIsTruncated(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "large.txt", strings.Repeat("x", 3000)+"\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  1024,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "large.txt", "offset_line": 1, "limit_lines": 1,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("read failed: %s", result.Output)
	}
	if !result.Truncated || !strings.Contains(result.Output, "output truncated") {
		t.Fatalf("result = %+v, want truncation marker", result)
	}
}

func TestReadToolsDoNotExposeSensitiveFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".env", "API_TOKEN=very-secret\n")
	writeTestFile(t, root, "safe.txt", "ordinary text\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   2,
		CommandTimeout:  time.Second,
	})

	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "very-secret", "path": nil, "max_results": nil,
		}),
		toolCall("read", "read_file", map[string]any{
			"path": ".env", "offset_line": nil, "limit_lines": nil,
		}),
	}, nil)
	if results[0].IsError || strings.Contains(results[0].Output, "very-secret") {
		t.Fatalf("search result = %+v, want secret skipped", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Output, "sensitive path") {
		t.Fatalf("read result = %+v, want sensitive-path error", results[1])
	}
}

func TestReadCannotBypassSensitivePolicyThroughSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".git/config", "token=very-secret\n")
	if err := os.Symlink(".git", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "alias/config", "offset_line": nil, "limit_lines": nil,
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "symlink component") {
		t.Fatalf("result = %+v, want symlink rejection", result)
	}
}

func TestToolCallFanOutIsBounded(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   4,
		CommandTimeout:  time.Second,
	})
	calls := make([]agent.ToolCall, tools.MaxToolCallsPerBatch+3)
	for index := range calls {
		calls[index] = toolCall(
			string(rune('a'+index)),
			"list_files",
			map[string]any{"path": nil, "max_depth": 1},
		)
	}

	results := registry.Execute(context.Background(), calls, nil)
	for index := 0; index < tools.MaxToolCallsPerBatch; index++ {
		if results[index].IsError {
			t.Fatalf("allowed call %d failed: %s", index, results[index].Output)
		}
	}
	for index := tools.MaxToolCallsPerBatch; index < len(results); index++ {
		if !results[index].IsError || !strings.Contains(results[index].Output, "limit exceeded") {
			t.Fatalf("overflow result %d = %+v", index, results[index])
		}
	}
}

func TestApplyPatchUpdatesMultipleFilesTransactionally(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "existing.txt", "before\n")
	writeTestFile(t, root, "obsolete.txt", "remove me\n")
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{
				{"path": "existing.txt", "action": "update", "old_text": "before\n", "new_text": "after\n"},
				{"path": "nested/new.txt", "action": "add", "content": "created\n"},
				{"path": "obsolete.txt", "action": "delete"},
			},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "existing.txt"), "after\n")
	assertToolFileContents(t, filepath.Join(root, "nested", "new.txt"), "created\n")
	if _, err := os.Stat(filepath.Join(root, "obsolete.txt")); !os.IsNotExist(err) {
		t.Fatalf("obsolete file still exists: %v", err)
	}
	if len(approver.actions) != 1 {
		t.Fatalf("approval count = %d, want 1", len(approver.actions))
	}
	if approver.actions[0].Kind != approval.KindWrite {
		t.Fatalf("patch kind = %q, want write", approver.actions[0].Kind)
	}
	for _, expected := range []string{
		"--- existing.txt",
		"-before",
		"+after",
		"nested/new.txt",
		"+created",
		"obsolete.txt",
		"-remove me",
	} {
		if !strings.Contains(approver.actions[0].Preview, expected) {
			t.Fatalf("preview = %q, want %s", approver.actions[0].Preview, expected)
		}
	}
}

func TestApplyPatchDenialChangesNothing(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: false}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{
				{"path": "file.txt", "action": "update", "old_text": "before\n", "new_text": "after\n"},
				{"path": "new.txt", "action": "add", "content": "new\n"},
			},
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "permission denied") {
		t.Fatalf("result = %+v, want permission denial", result)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "before\n")
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new file exists after denial: %v", err)
	}
}

func TestApplyPatchRejectsSensitiveAndSymlinkPaths(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".env", "API_TOKEN=very-secret\n")
	writeTestFile(t, root, "real.txt", "before\n")
	if err := os.Symlink("real.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	sensitive := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": ".env", "action": "update", "old_text": "API_TOKEN=very-secret\n", "new_text": "API_TOKEN=changed\n",
			}},
		}),
	}, nil)[0]
	if !sensitive.IsError || !strings.Contains(sensitive.Output, "sensitive path") {
		t.Fatalf("sensitive result = %+v, want sensitive-path error", sensitive)
	}
	assertToolFileContents(t, filepath.Join(root, ".env"), "API_TOKEN=very-secret\n")

	symlink := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "alias.txt", "action": "update", "old_text": "before\n", "new_text": "after\n",
			}},
		}),
	}, nil)[0]
	if !symlink.IsError || !strings.Contains(symlink.Output, "symlink") {
		t.Fatalf("symlink result = %+v, want symlink rejection", symlink)
	}
	assertToolFileContents(t, filepath.Join(root, "real.txt"), "before\n")
}

func TestApplyPatchAbortsAllFilesChangedAfterApprovalPreview(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	writeTestFile(t, root, "first.txt", "one\n")
	writeTestFile(t, root, "second.txt", "two\n")
	approver := approverFunc(func(_ context.Context, _ approval.Action) (bool, error) {
		if err := os.WriteFile(first, []byte("external\n"), 0o644); err != nil {
			return false, err
		}
		return true, nil
	})
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{
				{"path": "first.txt", "action": "update", "old_text": "one\n", "new_text": "ONE\n"},
				{"path": "second.txt", "action": "update", "old_text": "two\n", "new_text": "TWO\n"},
			},
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "changed before transaction") {
		t.Fatalf("result = %+v, want snapshot error", result)
	}
	assertToolFileContents(t, first, "external\n")
	assertToolFileContents(t, filepath.Join(root, "second.txt"), "two\n")
}

func TestAutoWritesAppliesFileChangesWithoutPrompt(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	inner := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approval.NewPolicy(config.PermissionAutoWrites, inner), tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("edit", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update", "old_text": "before", "new_text": "after",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("edit failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "after\n")
	if len(inner.actions) != 0 {
		t.Fatalf("auto-writes prompted for a file change: %#v", inner.actions)
	}
}

func TestAutoAppliesFileChangesWhenInnerWouldDeny(t *testing.T) {
	root := t.TempDir()
	inner := &staticApprover{approved: false}
	registry := newTestRegistry(t, root, approval.NewPolicy(config.PermissionAuto, inner), tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("write", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "created.txt", "action": "add", "content": "ok\n",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("write failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "created.txt"), "ok\n")
	if len(inner.actions) != 0 {
		t.Fatalf("auto prompted for a write: %#v", inner.actions)
	}
}

func TestPlanModeHidesWritesAndRejectsMutations(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n")
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   2,
		CommandTimeout:  time.Second,
	})
	registry.SetPlan(true)

	for _, def := range registry.Definitions() {
		if !def.ReadOnly {
			t.Fatalf("plan mode exposed writable tool %s", def.Name)
		}
	}

	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("write", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "created.go", "action": "add", "content": "package created\n",
			}},
		}),
		toolCall("run", "run_command", map[string]any{"command": "echo hi", "timeout_seconds": nil}),
		toolCall("read", "read_file", map[string]any{"path": "main.go", "offset_line": 1, "limit_lines": 4}),
	}, nil)
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if !results[0].IsError || !strings.Contains(results[0].Output, "plan mode") {
		t.Fatalf("write in plan mode = %#v", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Output, "plan mode") {
		t.Fatalf("command in plan mode = %#v", results[1])
	}
	if results[2].IsError || !strings.Contains(results[2].Output, "package main") {
		t.Fatalf("read in plan mode = %#v", results[2])
	}
	if _, err := os.Stat(filepath.Join(root, "created.go")); !os.IsNotExist(err) {
		t.Fatalf("plan mode created a file: %v", err)
	}
	approver.mu.Lock()
	actions := len(approver.actions)
	approver.mu.Unlock()
	if actions != 0 {
		t.Fatalf("plan mode asked for approval: %#v", approver.actions)
	}

	registry.SetPlan(false)
	foundPatch := false
	for _, def := range registry.Definitions() {
		if def.Name == "apply_patch" {
			foundPatch = true
		}
	}
	if !foundPatch {
		t.Fatal("agent mode hid apply_patch")
	}
}

func TestSearchFilesSupportsRegexGlobAndLiteralFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/main.go", "package main\n\nfunc main() {}\n")
	writeTestFile(t, root, "src/util.go", "package src\n\nfunc helper() {}\n")
	writeTestFile(t, root, "README.md", "func main is documented here\nvalues[0]\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 20,
		ParallelReads:   2,
		CommandTimeout:  time.Second,
	})

	regex := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "func main", "path": nil, "glob": "*.go", "max_results": nil, "case_sensitive": nil,
		}),
	}, nil)[0]
	if regex.IsError {
		t.Fatalf("regex search failed: %s", regex.Output)
	}
	if !strings.Contains(regex.Output, "src/main.go") {
		t.Fatalf("regex search = %q, want src/main.go", regex.Output)
	}
	if strings.Contains(regex.Output, "README.md") {
		t.Fatalf("glob should exclude README.md: %q", regex.Output)
	}

	fallback := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "[", "path": nil, "glob": nil, "max_results": nil, "case_sensitive": nil,
		}),
	}, nil)[0]
	if fallback.IsError || !strings.Contains(fallback.Output, "README.md:2:values[0]") {
		t.Fatalf("literal fallback = %+v, want README.md match", fallback)
	}
}

func TestApplyPatchRejectsLegacyPatchDocument(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"patch": "*** Begin Patch\n*** Update File: file.txt\n@@\n-before\n+after\n*** End Patch",
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "changes") {
		t.Fatalf("result = %+v, want changes-array error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "before\n")
}

func TestApplyPatchAppliesMultipleUpdatesToSameFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "alpha\nbeta\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{
				{"path": "file.txt", "action": "update", "old_text": "alpha", "new_text": "ALPHA"},
				{"path": "file.txt", "action": "update", "old_text": "beta", "new_text": "BETA"},
			},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "ALPHA\nBETA\n")
}

func TestApplyPatchRejectsNonUniqueUpdate(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "same\nsame\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update", "old_text": "same", "new_text": "other",
			}},
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "exactly once") {
		t.Fatalf("result = %+v, want unique-replace error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "same\nsame\n")
}

func TestApplyPatchRejectsMixedActionsOnSamePath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{
				{"path": "file.txt", "action": "update", "old_text": "before", "new_text": "after"},
				{"path": "file.txt", "action": "delete"},
			},
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "cannot mix") {
		t.Fatalf("result = %+v, want mixed-action error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "before\n")
}

func TestListFilesDoesNotSkipVendorByDefault(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "vendor/pkg.go", "package pkg\n")
	writeTestFile(t, root, "src/main.go", "package main\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": nil, "max_depth": 4}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("list failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "vendor/pkg.go") {
		t.Fatalf("list = %q, want vendor/pkg.go", result.Output)
	}
}

func assertToolFileContents(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s contents = %q, want %q", path, data, expected)
	}
}

func newTestRegistry(
	t *testing.T,
	root string,
	approver approval.Approver,
	options tools.Options,
) *tools.Registry {
	t.Helper()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return tools.NewRegistry(ws, approver, options)
}

func toolCall(id, name string, arguments map[string]any) agent.ToolCall {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return agent.ToolCall{ID: id, Name: name, Arguments: encoded}
}

func writeTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
