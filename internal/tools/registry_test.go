package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/approval"
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

	registry := newTestRegistry(t, root, &staticApprover{}, Options{
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
	registry := newTestRegistry(t, root, approver, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("edit", "edit_file", map[string]any{
			"path": "file.txt", "old_text": "before", "new_text": "after",
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
	if len(approver.actions) != 1 || !strings.Contains(approver.actions[0].Preview, "-before") {
		t.Fatalf("approval actions = %#v", approver.actions)
	}
}

func TestApprovedWriteAndEditAreAtomic(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("write", "write_file", map[string]any{
			"path": "nested/file.txt", "content": "before\n",
		}),
		toolCall("edit", "edit_file", map[string]any{
			"path": "nested/file.txt", "old_text": "before", "new_text": "after",
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
	registry := newTestRegistry(t, root, approver, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("edit", "edit_file", map[string]any{
			"path": "file.txt", "old_text": "before", "new_text": "after",
		}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "changed after approval") {
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
	registry := newTestRegistry(t, root, &staticApprover{}, Options{
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
	registry := newTestRegistry(t, root, &staticApprover{}, Options{
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
	registry := newTestRegistry(t, root, &staticApprover{}, Options{
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
	registry := newTestRegistry(t, root, &staticApprover{}, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   4,
		CommandTimeout:  time.Second,
	})
	calls := make([]agent.ToolCall, maxToolCallsPerBatch+3)
	for index := range calls {
		calls[index] = toolCall(
			string(rune('a'+index)),
			"list_files",
			map[string]any{"path": nil, "max_depth": 1},
		)
	}

	results := registry.Execute(context.Background(), calls, nil)
	for index := 0; index < maxToolCallsPerBatch; index++ {
		if results[index].IsError {
			t.Fatalf("allowed call %d failed: %s", index, results[index].Output)
		}
	}
	for index := maxToolCallsPerBatch; index < len(results); index++ {
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
	registry := newTestRegistry(t, root, approver, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	patch := `*** Begin Patch
*** Update File: existing.txt
@@
-before
+after
*** Add File: nested/new.txt
+created
*** Delete File: obsolete.txt
*** End Patch`

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{"patch": patch}),
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
	registry := newTestRegistry(t, root, &staticApprover{approved: false}, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	patch := `*** Begin Patch
*** Update File: file.txt
@@
-before
+after
*** Add File: new.txt
+new
*** End Patch`

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{"patch": patch}),
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
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	sensitive := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"patch": "*** Begin Patch\n*** Update File: .env\n@@\n-API_TOKEN=very-secret\n+API_TOKEN=changed\n*** End Patch",
		}),
	}, nil)[0]
	if !sensitive.IsError || !strings.Contains(sensitive.Output, "sensitive path") {
		t.Fatalf("sensitive result = %+v, want sensitive-path error", sensitive)
	}
	assertToolFileContents(t, filepath.Join(root, ".env"), "API_TOKEN=very-secret\n")

	symlink := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"patch": "*** Begin Patch\n*** Update File: alias.txt\n@@\n-before\n+after\n*** End Patch",
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
	registry := newTestRegistry(t, root, approver, Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	patch := `*** Begin Patch
*** Update File: first.txt
@@
-one
+ONE
*** Update File: second.txt
@@
-two
+TWO
*** End Patch`

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{"patch": patch}),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "changed before transaction") {
		t.Fatalf("result = %+v, want snapshot error", result)
	}
	assertToolFileContents(t, first, "external\n")
	assertToolFileContents(t, filepath.Join(root, "second.txt"), "two\n")
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
	options Options,
) *Registry {
	t.Helper()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return NewRegistry(ws, approver, options)
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
