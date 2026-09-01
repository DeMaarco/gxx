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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

type approverFunc func(context.Context, approval.Action) (approval.Decision, error)

func (f approverFunc) Approve(ctx context.Context, action approval.Action) (approval.Decision, error) {
	return f(ctx, action)
}

func (a *staticApprover) Approve(_ context.Context, action approval.Action) (approval.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	return approval.Decision{Approved: a.approved}, nil
}

func TestMissingPathErrorsNameThePathNotTheSyscall(t *testing.T) {
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": "assets", "max_depth": nil}),
	}, nil)[0]

	if !result.IsError {
		t.Fatalf("listing a missing directory succeeded: %q", result.Output)
	}
	// os.Root names its own syscall, so this used to read "statat assets".
	for _, leaked := range []string{"statat", "openat", "lstatat"} {
		if strings.Contains(result.Output, leaked) {
			t.Fatalf("output = %q, want no syscall name", result.Output)
		}
	}
	if !strings.Contains(result.Output, "assets: no such file or directory") {
		t.Fatalf("output = %q, want the path and a plain reason", result.Output)
	}
}

func TestLongMatchesAreCutOnCharacterBoundaries(t *testing.T) {
	root := t.TempDir()
	// Byte 300 of this line falls inside the second byte of an accented
	// character, which is where the search result limit lands.
	line := "x" + strings.Repeat("é", 200)
	if err := os.WriteFile(filepath.Join(root, "acentos.txt"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "x", "path": nil, "glob": nil, "max_results": nil, "case_sensitive": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !utf8.ValidString(result.Output) {
		t.Fatalf("output = %q, want valid UTF-8 after truncation", result.Output)
	}
}

func TestDescribeKeepsErrorIdentity(t *testing.T) {
	root := t.TempDir()
	space, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer space.Close()

	_, err = space.Stat("missing/deep")
	if err == nil {
		t.Fatal("Stat() on a missing path succeeded")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want it to still match fs.ErrNotExist", err)
	}
}

func TestToolSchemasRequireEveryProperty(t *testing.T) {
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{}, tools.Options{
		MaxResultBytes:  1024,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	for _, def := range registry.Definitions() {
		assertStrictSchema(t, def.Name, def.Parameters)
	}
}

func assertStrictSchema(t *testing.T, name string, schema map[string]any) {
	t.Helper()
	if schema == nil {
		return
	}
	if items, ok := schema["items"].(map[string]any); ok {
		assertStrictSchema(t, name+".items", items)
	}
	if !schemaHasObjectType(schema["type"]) {
		return
	}
	properties, _ := schema["properties"].(map[string]any)
	required := stringSlice(schema["required"])
	seen := make(map[string]bool, len(required))
	for _, key := range required {
		seen[key] = true
	}
	for key, raw := range properties {
		if !seen[key] {
			t.Fatalf("%s: property %q must be listed in required for strict tools", name, key)
		}
		if nested, ok := raw.(map[string]any); ok {
			assertStrictSchema(t, name+"."+key, nested)
		}
	}
}

func schemaHasObjectType(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "object"
	case []string:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			text, _ := item.(string)
			if text == "object" {
				return true
			}
		}
	}
	return false
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, _ := item.(string)
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
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
	if !strings.Contains(results[2].Output, "(end of file, 2 lines)") {
		t.Fatalf("read output = %q, want end of file", results[2].Output)
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

func TestMutationDoesNotStartBeforeApproval(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\n")
	var events []agent.Event
	emit := func(event agent.Event) {
		events = append(events, event)
	}
	approver := approverFunc(func(_ context.Context, _ approval.Action) (approval.Decision, error) {
		for _, event := range events {
			if event.Kind == agent.EventToolStarted {
				t.Fatal("tool started before approval")
			}
		}
		return approval.Decision{Approved: true}, nil
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
	}, emit)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	if len(events) == 0 || events[0].Kind != agent.EventToolStarted {
		t.Fatalf("events = %#v, want started after approval", events)
	}
	if events[len(events)-1].Kind != agent.EventToolDone {
		t.Fatalf("events = %#v, want done after the tool runs", events)
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

func TestListAndSearchHonorNestedGxxignore(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "keep.txt", "hello root\n")
	writeTestFile(t, root, "pkg/keep.go", "hello pkg\n")
	writeTestFile(t, root, "pkg/skip.txt", "hello nested\n")
	writeTestFile(t, root, "pkg/.gxxignore", "skip.txt\n")
	writeTestFile(t, root, "pkg/nested/also.txt", "hello deep\n")

	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": nil, "max_depth": 4}),
		toolCall("search", "search_files", map[string]any{"query": "hello", "path": nil, "max_results": nil}),
		toolCall("list-pkg", "list_files", map[string]any{"path": "pkg", "max_depth": 4}),
	}, nil)
	for _, result := range results {
		if result.IsError {
			t.Fatalf("%s failed: %s", result.Name, result.Output)
		}
	}
	if !strings.Contains(results[0].Output, "keep.txt") || !strings.Contains(results[0].Output, "pkg/keep.go") {
		t.Fatalf("list = %q, want visible files", results[0].Output)
	}
	if strings.Contains(results[0].Output, "skip.txt") {
		t.Fatalf("list = %q, should omit pkg/skip.txt", results[0].Output)
	}
	if strings.Contains(results[1].Output, "skip.txt") {
		t.Fatalf("search = %q, should omit pkg/skip.txt", results[1].Output)
	}
	if !strings.Contains(results[1].Output, "pkg/keep.go") {
		t.Fatalf("search = %q, want pkg/keep.go", results[1].Output)
	}
	if strings.Contains(results[2].Output, "skip.txt") {
		t.Fatalf("list pkg = %q, should omit skip.txt", results[2].Output)
	}
	if !strings.Contains(results[2].Output, "keep.go") {
		t.Fatalf("list pkg = %q, want keep.go", results[2].Output)
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
	approver := approverFunc(func(_ context.Context, _ approval.Action) (approval.Decision, error) {
		if err := os.WriteFile(target, []byte("external change\n"), 0o644); err != nil {
			return approval.Decision{}, err
		}
		return approval.Decision{Approved: true}, nil
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
	requireSymlink(t, ".git", filepath.Join(root, "alias"))
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
	requireSymlink(t, "real.txt", filepath.Join(root, "alias.txt"))
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
	approver := approverFunc(func(_ context.Context, _ approval.Action) (approval.Decision, error) {
		if err := os.WriteFile(first, []byte("external\n"), 0o644); err != nil {
			return approval.Decision{}, err
		}
		return approval.Decision{Approved: true}, nil
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
		toolCall("img", "generate_image", map[string]any{
			"prompt": "x", "path": "out.png", "model": nil, "size": nil, "quality": nil,
			"output_format": nil, "background": nil,
		}),
		toolCall("read", "read_file", map[string]any{"path": "main.go", "offset_line": 1, "limit_lines": 4}),
	}, nil)
	if len(results) != 4 {
		t.Fatalf("len(results) = %d, want 4", len(results))
	}
	if !results[0].IsError || !strings.Contains(results[0].Output, "plan mode") {
		t.Fatalf("write in plan mode = %#v", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Output, "plan mode") {
		t.Fatalf("command in plan mode = %#v", results[1])
	}
	if !results[2].IsError || !strings.Contains(results[2].Output, "plan mode") {
		t.Fatalf("image in plan mode = %#v", results[2])
	}
	if results[3].IsError || !strings.Contains(results[3].Output, "package main") {
		t.Fatalf("read in plan mode = %#v", results[3])
	}
	if _, err := os.Stat(filepath.Join(root, "created.go")); !os.IsNotExist(err) {
		t.Fatalf("plan mode created a file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "out.png")); !os.IsNotExist(err) {
		t.Fatalf("plan mode wrote an image: %v", err)
	}
	approver.mu.Lock()
	actions := len(approver.actions)
	approver.mu.Unlock()
	if actions != 0 {
		t.Fatalf("plan mode asked for approval: %#v", approver.actions)
	}

	registry.SetPlan(false)
	foundPatch := false
	foundImage := false
	for _, def := range registry.Definitions() {
		if def.Name == "apply_patch" {
			foundPatch = true
		}
		if def.Name == "generate_image" {
			foundImage = true
		}
	}
	if !foundPatch {
		t.Fatal("agent mode hid apply_patch")
	}
	if !foundImage {
		t.Fatal("agent mode hid generate_image")
	}
}

func TestAskModeHidesWritesAndRejectsMutations(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n")
	approver := &staticApprover{approved: true}
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   2,
		CommandTimeout:  time.Second,
	})
	registry.SetAsk(true)

	for _, def := range registry.Definitions() {
		if !def.ReadOnly {
			t.Fatalf("ask mode exposed writable tool %s", def.Name)
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
	if !results[0].IsError || !strings.Contains(results[0].Output, "ask mode is read-only") {
		t.Fatalf("write in ask mode = %#v", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Output, "ask mode is read-only") {
		t.Fatalf("command in ask mode = %#v", results[1])
	}
	if results[2].IsError || !strings.Contains(results[2].Output, "package main") {
		t.Fatalf("read in ask mode = %#v", results[2])
	}
	if _, err := os.Stat(filepath.Join(root, "created.go")); !os.IsNotExist(err) {
		t.Fatalf("ask mode created a file: %v", err)
	}
	approver.mu.Lock()
	actions := len(approver.actions)
	approver.mu.Unlock()
	if actions != 0 {
		t.Fatalf("ask mode asked for approval: %#v", approver.actions)
	}

	registry.SetAsk(false)
	foundPatch := false
	for _, def := range registry.Definitions() {
		if def.Name == "apply_patch" {
			foundPatch = true
		}
	}
	if !foundPatch {
		t.Fatal("leaving ask mode hid apply_patch")
	}
}

func TestAskAndPlanCannotBeEnabledTogether(t *testing.T) {
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	registry.SetAsk(true)
	if !registry.Ask() || registry.Plan() {
		t.Fatalf("ask=%v plan=%v after SetAsk(true)", registry.Ask(), registry.Plan())
	}
	registry.SetPlan(true)
	if !registry.Plan() || registry.Ask() {
		t.Fatalf("ask=%v plan=%v after SetPlan(true)", registry.Ask(), registry.Plan())
	}
	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("run", "run_command", map[string]any{"command": "echo hi", "timeout_seconds": nil}),
	}, nil)
	if !results[0].IsError || !strings.Contains(results[0].Output, "plan mode") {
		t.Fatalf("stacked session error = %#v, want plan mode", results[0])
	}
	if strings.Contains(results[0].Output, "ask mode") {
		t.Fatalf("plan session used ask error: %q", results[0].Output)
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

func TestSearchFilesTruncatesDenseMatches(t *testing.T) {
	root := t.TempDir()
	var css strings.Builder
	for i := 0; i < 40; i++ {
		css.WriteString(".widget" + strings.Repeat("-x", 200) + "{color:red}\n")
	}
	writeTestFile(t, root, "src/app.css", css.String())
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  64 * 1024,
		MaxSearchResult: 100,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "widget", "path": "src", "glob": nil, "max_results": nil, "case_sensitive": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "src/app.css") {
		t.Fatalf("search = %q, want css hits", result.Output)
	}
	if !strings.Contains(result.Output, "search result truncated") {
		t.Fatalf("search = %q, want byte truncation", result.Output)
	}
	if len(result.Output) > 6*1024 {
		t.Fatalf("search length = %d, want capped near 4KB", len(result.Output))
	}
}

func TestSearchFilesTODODoesNotMatchSpanishTodo(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/copy.go", "package src\n\nconst n = \"Ponerlo todo en una sola apuesta\"\n")
	writeTestFile(t, root, "src/todo.go", "package src\n\n// TODO: handle timeout\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 20,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "TODO", "path": "src", "glob": nil, "max_results": nil, "case_sensitive": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("search failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "src/todo.go") {
		t.Fatalf("search = %q, want TODO marker", result.Output)
	}
	if strings.Contains(result.Output, "copy.go") {
		t.Fatalf("search = %q, should not match Spanish todo", result.Output)
	}

	insensitive := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "TODO", "path": "src", "glob": nil, "max_results": nil, "case_sensitive": false,
		}),
	}, nil)[0]
	if insensitive.IsError || !strings.Contains(insensitive.Output, "copy.go") {
		t.Fatalf("explicit case-insensitive TODO = %+v, want Spanish todo", insensitive)
	}

	writeTestFile(t, root, "src/helpers.go", "package src\n\nfunc helpers() {}\nfunc helper() {}\n")
	word := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("search", "search_files", map[string]any{
			"query": "helper", "path": "src", "glob": nil, "max_results": nil, "case_sensitive": nil,
		}),
	}, nil)[0]
	if word.IsError {
		t.Fatalf("identifier search failed: %s", word.Output)
	}
	if !strings.Contains(word.Output, "func helper()") {
		t.Fatalf("identifier search = %q, want whole word helper", word.Output)
	}
	if strings.Contains(word.Output, "func helpers()") {
		t.Fatalf("identifier search = %q, should not match helpers", word.Output)
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
	if !result.IsError || !strings.Contains(result.Output, "ambiguous") {
		t.Fatalf("result = %+v, want unique-replace error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "same\nsame\n")
}

func TestApplyPatchReplacesEveryLongOldText(t *testing.T) {
	root := t.TempDir()
	url := "https://images.unsplash.com/photo-1524592094714-0f0654e20314?auto=format&fit=crop&w=900&q=88"
	writeTestFile(t, root, "index.html", url+"\n"+url+"\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "index.html", "action": "update", "old_text": url, "new_text": "assets/nebula.svg",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "index.html"), "assets/nebula.svg\nassets/nebula.svg\n")
}

func TestApplyPatchNotFoundIsShort(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "hello\n")
	var events []agent.Event
	emit := func(event agent.Event) {
		events = append(events, event)
	}
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update", "old_text": "missing", "new_text": "other",
			}},
		}),
	}, emit)[0]
	if !result.IsError || !strings.Contains(result.Output, "old_text not found in file.txt") {
		t.Fatalf("result = %+v, want not-found error", result)
	}
	if strings.Contains(result.Output, "exactly once") || strings.Contains(result.Output, "occurrences") {
		t.Fatalf("result = %+v, want short not-found error", result)
	}
	if len(events) < 2 || events[0].Kind != agent.EventToolStarted || events[1].Kind != agent.EventToolDone {
		t.Fatalf("events = %#v, want started then done on prepare error", events)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "hello\n")
}

func TestApplyPatchMatchesCRLFOldText(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "alpha\r\nbeta\r\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update",
				"old_text": "alpha\nbeta\n", "new_text": "ALPHA\nBETA\n",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "ALPHA\r\nBETA\r\n")
}

func TestApplyPatchRewritesExistingFileInPlace(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "file.txt", "before\nkeep\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "file.txt", "action": "update", "content": "rewritten\n",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "rewritten\n")
}

func TestApplyPatchDeleteThenAddRewritesInPlace(t *testing.T) {
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
				{"path": "file.txt", "action": "delete"},
				{"path": "file.txt", "action": "add", "content": "after\n"},
			},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "file.txt"), "after\n")
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

func TestApplyPatchAcceptsDotSlashPaths(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/file.txt", "before\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": "./src/file.txt", "action": "update", "old_text": "before", "new_text": "after",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "src", "file.txt"), "after\n")
}

func TestApplyPatchAcceptsWindowsSeparators(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash separators")
	}
	root := t.TempDir()
	writeTestFile(t, root, "src/file.txt", "before\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("patch", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path": `src\file.txt`, "action": "update", "old_text": "before", "new_text": "after",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "src", "file.txt"), "after\n")
}

func TestListAndSearchEmitProgress(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/a.go", "package a\n")
	writeTestFile(t, root, "src/b.go", "package b\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	var mu sync.Mutex
	var events []agent.Event
	results := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": nil, "max_depth": 4}),
		toolCall("search", "search_files", map[string]any{"query": "package", "path": nil, "max_results": nil}),
	}, func(event agent.Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	for _, result := range results {
		if result.IsError {
			t.Fatalf("%s failed: %s", result.Name, result.Output)
		}
	}
	progress := 0
	sawList := false
	sawSearch := false
	for _, event := range events {
		if event.Kind != agent.EventToolProgress {
			continue
		}
		progress++
		name := ""
		if event.ToolCall != nil {
			name = event.ToolCall.Name
		}
		if name == "list_files" || strings.Contains(event.Text, "src/") {
			sawList = true
		}
		if name == "search_files" || strings.HasSuffix(event.Text, ".go") {
			sawSearch = true
		}
	}
	if progress == 0 || !sawList || !sawSearch {
		t.Fatalf("progress events = %#v, want list and search updates", events)
	}
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

func TestListFilesReportsIgnoredDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "node_modules/pkg/index.js", "module.exports = 1\n")
	writeTestFile(t, root, "src/main.go", "package main\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("list", "list_files", map[string]any{"path": "node_modules", "max_depth": 2}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("list ignored dir failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "ignored") {
		t.Fatalf("list node_modules = %q, want ignored notice", result.Output)
	}
	if strings.Contains(result.Output, "No files found") {
		t.Fatalf("list node_modules = %q, should not look empty", result.Output)
	}
}

func TestWorkspaceOverviewListsDepthThreeAndGitFlag(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "hello\n")
	writeTestFile(t, root, "src/main.go", "package main\n")
	writeTestFile(t, root, "src/pkg/nested.go", "package pkg\n")
	writeTestFile(t, root, "src/pkg/more/deep.go", "package more\n")
	writeTestFile(t, root, "node_modules/pkg/index.js", "module.exports = 1\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	plain := registry.WorkspaceOverview(context.Background())
	if !strings.Contains(plain, "git: no") {
		t.Fatalf("overview = %q, want git: no", plain)
	}
	if !strings.Contains(plain, "README.md") || !strings.Contains(plain, "src/main.go") || !strings.Contains(plain, "src/pkg/nested.go") {
		t.Fatalf("overview = %q, want depth-3 files", plain)
	}
	if strings.Contains(plain, "deep.go") {
		t.Fatalf("overview = %q, should omit depth-4 files", plain)
	}
	if strings.Contains(plain, "node_modules/pkg") {
		t.Fatalf("overview = %q, should not list ignored contents", plain)
	}
	if !strings.Contains(plain, "ignored:") || !strings.Contains(plain, "node_modules/") {
		t.Fatalf("overview = %q, want ignored top-level dirs", plain)
	}

	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	withGit := registry.WorkspaceOverview(context.Background())
	if !strings.Contains(withGit, "git: yes") {
		t.Fatalf("overview = %q, want git: yes", withGit)
	}
	if strings.Contains(withGit, ".git/config") || strings.Contains(withGit, ".git/HEAD") {
		t.Fatalf("overview = %q, should not list .git contents", withGit)
	}
	if !strings.Contains(withGit, "ignored:") || !strings.Contains(withGit, ".git/") {
		t.Fatalf("overview = %q, want .git in ignored", withGit)
	}
}

func TestWorkspaceOverviewOmitsGeneratedSiblingScripts(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "vite.config.ts", "export default {}\n")
	writeTestFile(t, root, "vite.config.js", "export default {}\n")
	writeTestFile(t, root, "vite.config.d.ts", "export {}\n")
	writeTestFile(t, root, "plain.js", "console.log(1)\n")
	writeTestFile(t, root, "package-lock.json", "{}\n")
	writeTestFile(t, root, "big.css", strings.Repeat("x", 20*1024)+"\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	got := registry.WorkspaceOverview(context.Background())
	if !strings.Contains(got, "vite.config.ts") || !strings.Contains(got, "plain.js") {
		t.Fatalf("overview = %q, want source files", got)
	}
	if strings.Contains(got, "vite.config.js") || strings.Contains(got, "vite.config.d.ts") {
		t.Fatalf("overview = %q, should omit compiled siblings", got)
	}
	if strings.Contains(got, "package-lock.json") {
		t.Fatalf("overview = %q, should omit lockfiles", got)
	}
	if !strings.Contains(got, "big.css (20KB)") {
		t.Fatalf("overview = %q, want size for large files", got)
	}
}

func TestReadFileStopsAroundByteBudget(t *testing.T) {
	root := t.TempDir()
	var builder strings.Builder
	for i := 0; i < 80; i++ {
		builder.WriteString(strings.Repeat("a", 200))
		builder.WriteByte('\n')
	}
	writeTestFile(t, root, "wide.css", builder.String())
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  64 * 1024,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "wide.css", "offset_line": nil, "limit_lines": nil,
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("read failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "truncated at 12KB") {
		t.Fatalf("read = %q, want byte budget notice", result.Output)
	}
	if len(result.Output) > 16*1024 {
		t.Fatalf("read length = %d, want capped near 12KB", len(result.Output))
	}
}

func TestReadFileSamplesDenseFilesAndRefusesPaging(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "min.css", strings.Repeat("a{x:1}", 2000)+"\n/* Footer styles */\n"+strings.Repeat("b{y:2}", 2000)+"\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  64 * 1024,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	first := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "min.css", "offset_line": nil, "limit_lines": nil,
		}),
	}, nil)[0]
	if first.IsError {
		t.Fatalf("dense read failed: %s", first.Output)
	}
	if !strings.Contains(first.Output, "dense file") {
		t.Fatalf("dense read = %q, want dense notice", first.Output)
	}
	if !strings.Contains(first.Output, "Footer styles") {
		t.Fatalf("dense read = %q, want later comment markers", first.Output)
	}
	if len(first.Output) > 14*1024 {
		t.Fatalf("dense read length = %d, want a single sample", len(first.Output))
	}
	second := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "min.css", "offset_line": 2, "limit_lines": nil,
		}),
	}, nil)[0]
	if second.IsError {
		t.Fatalf("dense page failed: %s", second.Output)
	}
	if !strings.Contains(second.Output, "Do not page") {
		t.Fatalf("dense page = %q, want paging refusal", second.Output)
	}
	if len(second.Output) > 512 {
		t.Fatalf("dense page length = %d, want a short refusal", len(second.Output))
	}
}

func TestWorkspaceOverviewTruncatesLargeTrees(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		writeTestFile(t, root, filepath.Join("f", fmt.Sprintf("%02d.txt", i)), "x\n")
	}
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	got := registry.WorkspaceOverview(context.Background())
	if !strings.Contains(got, "… truncated") {
		t.Fatalf("overview = %q, want truncation marker", got)
	}
	if strings.Count(got, ".txt") > 40 {
		t.Fatalf("overview listed too many files: %q", got)
	}
}

func TestReadFileReportsEndOfFileAndMoreLines(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "notes.txt", "one\ntwo\nthree\n")
	registry := newTestRegistry(t, root, &staticApprover{}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})

	partial := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "notes.txt", "offset_line": 1, "limit_lines": 1,
		}),
	}, nil)[0]
	if partial.IsError {
		t.Fatalf("partial read failed: %s", partial.Output)
	}
	if !strings.Contains(partial.Output, "… more lines follow") {
		t.Fatalf("partial = %q, want more lines", partial.Output)
	}

	full := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("read", "read_file", map[string]any{
			"path": "notes.txt", "offset_line": 1, "limit_lines": 10,
		}),
	}, nil)[0]
	if full.IsError {
		t.Fatalf("full read failed: %s", full.Output)
	}
	if !strings.Contains(full.Output, "(end of file, 3 lines)") {
		t.Fatalf("full = %q, want end of file", full.Output)
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

func requireSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
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
