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
	"testing"

	"gxx/internal/workspace"
)

func TestWorkspaceRejectsTraversalAndOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, path := range []string{"../secret.txt", "escape/secret.txt"} {
		if _, err := ws.Stat(path); err == nil {
			t.Fatalf("Stat(%q) succeeded, want confinement error", path)
		}
		if _, err := ws.ReadRegularFile(path, 1024); err == nil {
			t.Fatalf("ReadRegularFile(%q) succeeded, want confinement error", path)
		}
	}
	if err := ws.AtomicWrite("escape/secret.txt", []byte("changed")); err == nil {
		t.Fatal("AtomicWrite() followed an outside symlink")
	}
	data, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret" {
		t.Fatalf("outside file was modified: %q", data)
	}
}

func TestWorkspaceRejectsSymlinkInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err := ws.ReadRegularFile("inside/file.txt", 1024); err == nil {
		t.Fatal("ReadRegularFile() followed an in-workspace directory symlink")
	}
	if err := ws.AtomicWrite("inside/new.txt", []byte("x")); err == nil {
		t.Fatal("AtomicWrite() followed an in-workspace directory symlink")
	}
}

func TestAtomicWriteCreatesParentsAndPreservesMode(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	path := "nested/script.sh"
	if err := ws.AtomicWrite(path, []byte("#!/bin/sh\n")); err != nil {
		t.Fatalf("AtomicWrite(create) error = %v", err)
	}
	target := filepath.Join(root, "nested", "script.sh")
	if err := os.Chmod(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ws.AtomicWrite(path, []byte("#!/bin/sh\necho ok\n")); err != nil {
		t.Fatalf("AtomicWrite(update) error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\necho ok\n" {
		t.Fatalf("file contents = %q", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %o, want 700", info.Mode().Perm())
	}
}
