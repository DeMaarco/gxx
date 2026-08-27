package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRejectsTraversalAndOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, path := range []string{"../secret.txt", "escape/secret.txt"} {
		if _, err := ws.ResolveExisting(path); err == nil {
			t.Fatalf("ResolveExisting(%q) succeeded, want confinement error", path)
		}
		if _, err := ws.ResolveForWrite(path); err == nil {
			t.Fatalf("ResolveForWrite(%q) succeeded, want confinement error", path)
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

func TestResolveAllowsSymlinkInsideWorkspace(t *testing.T) {
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

	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	resolved, err := ws.ResolveExisting("inside/file.txt")
	if err != nil {
		t.Fatalf("ResolveExisting() error = %v", err)
	}
	if !strings.HasSuffix(resolved, filepath.Join("target", "file.txt")) {
		t.Fatalf("resolved path = %q, want target/file.txt", resolved)
	}
}

func TestAtomicWriteCreatesParentsAndPreservesMode(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
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
