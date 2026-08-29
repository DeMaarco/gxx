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

package workspace_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gxx/internal/workspace"
)

func TestApplyTransactionAddsUpdatesAndDeletesTogether(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "update.txt"), []byte("before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "delete.txt"), []byte("obsolete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	err = ws.ApplyTransaction([]workspace.FileChange{
		{
			Path:           "update.txt",
			Data:           []byte("after\n"),
			Expected:       []byte("before\n"),
			ExpectedExists: true,
		},
		{
			Path: "nested/new.txt",
			Data: []byte("new\n"),
		},
		{
			Path:           "delete.txt",
			Delete:         true,
			Expected:       []byte("obsolete\n"),
			ExpectedExists: true,
		},
	})
	if err != nil {
		t.Fatalf("ApplyTransaction() error = %v", err)
	}

	assertFileContents(t, filepath.Join(root, "update.txt"), "after\n")
	assertFileContents(t, filepath.Join(root, "nested", "new.txt"), "new\n")
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "update.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("updated mode = %04o, want 0640", info.Mode().Perm())
	}
	assertNoTransactionArtifacts(t, root)
}

func TestApplyTransactionLeavesFilesUntouchedWhenPreparationFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "first.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "blocked"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	err = ws.ApplyTransaction([]workspace.FileChange{
		{
			Path:           "first.txt",
			Data:           []byte("after\n"),
			Expected:       []byte("before\n"),
			ExpectedExists: true,
		},
		{
			Path: "blocked/new.txt",
			Data: []byte("new\n"),
		},
	})
	if err == nil {
		t.Fatal("ApplyTransaction() succeeded, want preparation error")
	}
	assertFileContents(t, filepath.Join(root, "first.txt"), "before\n")
	assertNoTransactionArtifacts(t, root)
}

func TestApplyTransactionRejectsChangedSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	err = ws.ApplyTransaction([]workspace.FileChange{{
		Path:           "file.txt",
		Data:           []byte("new\n"),
		Expected:       []byte("stale\n"),
		ExpectedExists: true,
	}})
	if err == nil || !strings.Contains(err.Error(), "changed before transaction") {
		t.Fatalf("ApplyTransaction() error = %v, want snapshot error", err)
	}
	assertFileContents(t, filepath.Join(root, "file.txt"), "current\n")
	assertNoTransactionArtifacts(t, root)
}

func TestApplyTransactionRemovesCreatedParentsOnFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blocked"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	err = ws.ApplyTransaction([]workspace.FileChange{
		{Path: "nested/deep/new.txt", Data: []byte("new\n")},
		{Path: "blocked/x.txt", Data: []byte("x\n")},
	})
	if err == nil {
		t.Fatal("ApplyTransaction() succeeded, want parent error")
	}
	if _, err := os.Stat(filepath.Join(root, "nested")); !os.IsNotExist(err) {
		t.Fatalf("created parent remained after rollback: %v", err)
	}
	assertNoTransactionArtifacts(t, root)
}

func TestApplyTransactionRejectsNestedTargetPaths(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	err = ws.ApplyTransaction([]workspace.FileChange{
		{Path: "parent", Data: []byte("file\n")},
		{Path: "parent/child", Data: []byte("child\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "contain one another") {
		t.Fatalf("ApplyTransaction() error = %v, want nested-path rejection", err)
	}
	if _, err := os.Stat(filepath.Join(root, "parent")); !os.IsNotExist(err) {
		t.Fatalf("parent path exists after rejection: %v", err)
	}
	assertNoTransactionArtifacts(t, root)
}

func assertFileContents(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s contents = %q, want %q", path, data, expected)
	}
}

func assertNoTransactionArtifacts(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".gxx-") {
			t.Errorf("transaction artifact remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
