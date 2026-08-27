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

package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Workspace confines filesystem operations to a single real directory tree.
type Workspace struct {
	root  string
	guard *os.Root
}

func New(root string) (*Workspace, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace symlinks: %w", err)
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return nil, fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace is not a directory: %s", realRoot)
	}
	guard, err := os.OpenRoot(realRoot)
	if err != nil {
		return nil, fmt.Errorf("open guarded workspace: %w", err)
	}
	return &Workspace{root: filepath.Clean(realRoot), guard: guard}, nil
}

func (w *Workspace) Root() string {
	return w.root
}

func (w *Workspace) Close() error {
	if w.guard == nil {
		return nil
	}
	return w.guard.Close()
}

func (w *Workspace) FS() fs.FS {
	return w.guard.FS()
}

func (w *Workspace) Clean(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path cannot be empty")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("absolute paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path leaves the workspace")
	}
	return filepath.ToSlash(clean), nil
}

func (w *Workspace) Stat(path string) (os.FileInfo, error) {
	clean, err := w.Clean(path)
	if err != nil {
		return nil, err
	}
	if err := w.rejectSymlinkComponents(clean, false); err != nil {
		return nil, err
	}
	return w.guard.Stat(clean)
}

func (w *Workspace) Lstat(path string) (os.FileInfo, error) {
	clean, err := w.Clean(path)
	if err != nil {
		return nil, err
	}
	return w.guard.Lstat(clean)
}

// OpenRegular opens a bounded regular file through os.Root. O_NONBLOCK keeps
// special files such as FIFOs from blocking before their type is verified.
func (w *Workspace) OpenRegular(path string, maxBytes int64) (*os.File, error) {
	clean, err := w.Clean(path)
	if err != nil {
		return nil, err
	}
	if err := w.rejectSymlinkComponents(clean, false); err != nil {
		return nil, err
	}
	file, err := w.guard.OpenFile(clean, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		_ = file.Close()
		return nil, fmt.Errorf("file exceeds %d bytes: %s", maxBytes, path)
	}
	return file, nil
}

func (w *Workspace) ReadRegularFile(path string, maxBytes int64) ([]byte, error) {
	file, err := w.OpenRegular(path, maxBytes)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := io.Reader(file)
	if maxBytes > 0 {
		reader = io.LimitReader(file, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes: %s", maxBytes, path)
	}
	return data, nil
}

func (w *Workspace) AtomicWrite(path string, data []byte) error {
	target, err := w.Clean(path)
	if err != nil {
		return err
	}
	if err := w.rejectSymlinkComponents(target, true); err != nil {
		return err
	}

	parent := filepath.Dir(target)
	if err := w.guard.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}

	mode := os.FileMode(0o644)
	if info, statErr := w.guard.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace symlink: %s", path)
		}
		if info.IsDir() {
			return fmt.Errorf("cannot replace directory: %s", path)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat target: %w", statErr)
	}

	tempPath, temp, err := w.createTemp(parent, mode)
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	cleanup := func() {
		_ = temp.Close()
		_ = w.guard.Remove(tempPath)
	}

	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("preserve file mode: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := w.guard.Rename(tempPath, target); err != nil {
		cleanup()
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}

func (w *Workspace) rejectSymlinkComponents(path string, allowMissing bool) error {
	if path == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == "" || component == "." {
			continue
		}
		current = pathJoin(current, component)
		info, err := w.guard.Lstat(current)
		if err != nil {
			if allowMissing && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing path with symlink component: %s", current)
		}
	}
	return nil
}

func pathJoin(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "/" + child
}

func (w *Workspace) createTemp(parent string, mode os.FileMode) (string, *os.File, error) {
	for range 10 {
		random := make([]byte, 8)
		if _, err := rand.Read(random); err != nil {
			return "", nil, err
		}
		name := filepath.Join(parent, ".gxx-"+hex.EncodeToString(random))
		file, err := w.guard.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("could not allocate temporary file")
}
