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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileChange describes one file in an all-or-nothing workspace transaction.
// Expected is compared after the original path is atomically moved aside.
type FileChange struct {
	Path           string
	Data           []byte
	Delete         bool
	Expected       []byte
	ExpectedExists bool
}

type stagedFileChange struct {
	FileChange
	cleanPath  string
	mode       os.FileMode
	stagePath  string
	backupPath string
	installed  bool
	backedUp   bool
}

// ApplyTransaction stages every new file, atomically captures existing files,
// verifies their exact approved snapshots, and then installs all results. Any
// failure before completion restores captured originals.
func (w *Workspace) ApplyTransaction(changes []FileChange) error {
	if len(changes) == 0 {
		return errors.New("transaction contains no file changes")
	}

	staged := make([]stagedFileChange, len(changes))
	seen := make(map[string]struct{}, len(changes))
	var createdDirectories []string
	createdSet := make(map[string]struct{})

	fail := func(err error) error {
		if cleanupErr := w.cleanupTransaction(staged, createdDirectories); cleanupErr != nil {
			return fmt.Errorf("%w; rollback failed: %v", err, cleanupErr)
		}
		return err
	}

	for index, change := range changes {
		clean, err := w.Clean(change.Path)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", change.Path, err))
		}
		if clean == "." {
			return fail(errors.New("cannot patch the workspace root"))
		}
		if _, exists := seen[clean]; exists {
			return fail(fmt.Errorf("duplicate transaction path %q", clean))
		}
		for existing := range seen {
			if strings.HasPrefix(clean, existing+"/") || strings.HasPrefix(existing, clean+"/") {
				return fail(fmt.Errorf(
					"transaction paths cannot contain one another: %q and %q",
					existing,
					clean,
				))
			}
		}
		seen[clean] = struct{}{}
		if change.Delete && !change.ExpectedExists {
			return fail(fmt.Errorf("cannot delete missing file %q", clean))
		}
		if err := w.rejectSymlinkComponents(clean, !change.ExpectedExists); err != nil {
			return fail(fmt.Errorf("%s: %w", clean, err))
		}

		mode, err := w.validateTransactionSnapshot(clean, change)
		if err != nil {
			return fail(err)
		}
		staged[index] = stagedFileChange{
			FileChange: FileChange{
				Path:           clean,
				Data:           append([]byte(nil), change.Data...),
				Delete:         change.Delete,
				Expected:       append([]byte(nil), change.Expected...),
				ExpectedExists: change.ExpectedExists,
			},
			cleanPath: clean,
			mode:      mode,
		}

		if change.Delete {
			continue
		}
		parent := filepath.Dir(clean)
		added, err := w.ensureTransactionParent(parent, createdSet)
		createdDirectories = append(createdDirectories, added...)
		if err != nil {
			return fail(fmt.Errorf("prepare parent for %s: %w", clean, err))
		}

		stagePath, file, err := w.createTemp(parent, mode)
		if err != nil {
			return fail(fmt.Errorf("stage %s: %w", clean, err))
		}
		staged[index].stagePath = stagePath
		if err := file.Chmod(mode); err != nil {
			_ = file.Close()
			return fail(fmt.Errorf("set staged mode for %s: %w", clean, err))
		}
		if _, err := file.Write(change.Data); err != nil {
			_ = file.Close()
			return fail(fmt.Errorf("stage %s: %w", clean, err))
		}
		if err := file.Close(); err != nil {
			return fail(fmt.Errorf("close staged %s: %w", clean, err))
		}
	}

	for index := range staged {
		change := &staged[index]
		if !change.ExpectedExists {
			continue
		}
		parent := filepath.Dir(change.cleanPath)
		backupPath, placeholder, err := w.createTemp(parent, change.mode)
		if err != nil {
			return fail(fmt.Errorf("reserve backup for %s: %w", change.cleanPath, err))
		}
		if err := placeholder.Close(); err != nil {
			_ = w.guard.Remove(backupPath)
			return fail(fmt.Errorf("close backup placeholder for %s: %w", change.cleanPath, err))
		}
		change.backupPath = backupPath
		if err := w.replace(change.cleanPath, backupPath); err != nil {
			_ = w.guard.Remove(backupPath)
			change.backupPath = ""
			return fail(fmt.Errorf("capture original %s: %w", change.cleanPath, err))
		}
		change.backedUp = true

		capturedInfo, err := w.guard.Lstat(backupPath)
		if err != nil {
			return fail(fmt.Errorf("inspect captured %s: %w", change.cleanPath, err))
		}
		if capturedInfo.Size() != int64(len(change.Expected)) {
			return fail(fmt.Errorf("%s changed after approval; patch was not applied", change.cleanPath))
		}
		captured, err := w.ReadRegularFile(
			backupPath,
			int64(len(change.Expected))+1,
		)
		if err != nil || !bytes.Equal(captured, change.Expected) {
			if err != nil {
				return fail(fmt.Errorf("verify captured %s: %w", change.cleanPath, err))
			}
			return fail(fmt.Errorf("%s changed after approval; patch was not applied", change.cleanPath))
		}
	}

	for index := range staged {
		change := &staged[index]
		if change.Delete {
			continue
		}
		if _, err := w.guard.Lstat(change.cleanPath); err == nil {
			return fail(fmt.Errorf("%s appeared during patch transaction", change.cleanPath))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(fmt.Errorf("inspect transaction target %s: %w", change.cleanPath, err))
		}
		if err := w.replace(change.stagePath, change.cleanPath); err != nil {
			return fail(fmt.Errorf("install %s: %w", change.cleanPath, err))
		}
		change.stagePath = ""
		change.installed = true
	}

	var cleanupErrors []string
	for index := range staged {
		change := &staged[index]
		if change.backupPath == "" {
			continue
		}
		if err := w.guard.Remove(change.backupPath); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", change.cleanPath, err))
			continue
		}
		change.backupPath = ""
		change.backedUp = false
	}
	if len(cleanupErrors) > 0 {
		return fmt.Errorf(
			"patch applied but backup cleanup failed: %s",
			strings.Join(cleanupErrors, "; "),
		)
	}
	return nil
}

func (w *Workspace) validateTransactionSnapshot(
	path string,
	change FileChange,
) (os.FileMode, error) {
	info, err := w.guard.Lstat(path)
	if !change.ExpectedExists {
		if err == nil {
			return 0, fmt.Errorf("cannot add existing path %q", path)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("inspect %s: %w", path, err)
		}
		return 0o644, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return 0, fmt.Errorf("not a regular non-symlink file: %s", path)
	}
	if info.Size() != int64(len(change.Expected)) {
		return 0, fmt.Errorf("%s changed before transaction", path)
	}
	current, err := w.ReadRegularFile(path, int64(len(change.Expected))+1)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	if !bytes.Equal(current, change.Expected) {
		return 0, fmt.Errorf("%s changed before transaction", path)
	}
	return info.Mode().Perm(), nil
}

func (w *Workspace) ensureTransactionParent(
	parent string,
	createdSet map[string]struct{},
) ([]string, error) {
	if parent == "." {
		return nil, nil
	}
	if err := w.rejectSymlinkComponents(parent, true); err != nil {
		return nil, err
	}

	var missing []string
	for current := parent; current != "." && current != ""; current = filepath.Dir(current) {
		info, err := w.guard.Lstat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%s is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, current)
	}

	var added []string
	for index := len(missing) - 1; index >= 0; index-- {
		directory := missing[index]
		if _, exists := createdSet[directory]; exists {
			continue
		}
		if err := w.guard.Mkdir(directory, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				info, statErr := w.guard.Lstat(directory)
				if statErr != nil {
					return added, statErr
				}
				if !info.IsDir() {
					return added, fmt.Errorf("%s is not a directory", directory)
				}
				continue
			}
			return added, err
		}
		createdSet[directory] = struct{}{}
		added = append(added, directory)
	}
	return added, nil
}

func (w *Workspace) cleanupTransaction(
	changes []stagedFileChange,
	createdDirectories []string,
) error {
	var cleanupErrors []string
	record := func(operation, path string, err error) {
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s %s: %v", operation, path, err))
	}

	for index := len(changes) - 1; index >= 0; index-- {
		change := &changes[index]
		if change.backedUp && change.backupPath != "" {
			record("restore", change.cleanPath, w.replace(change.backupPath, change.cleanPath))
			change.backedUp = false
			change.backupPath = ""
			change.installed = false
		} else if change.installed {
			record("remove installed", change.cleanPath, w.guard.Remove(change.cleanPath))
			change.installed = false
		}
		if change.stagePath != "" {
			record("remove staged", change.stagePath, w.guard.Remove(change.stagePath))
			change.stagePath = ""
		}
	}
	for index := len(createdDirectories) - 1; index >= 0; index-- {
		directory := createdDirectories[index]
		record("remove directory", directory, w.guard.Remove(directory))
	}
	if len(cleanupErrors) > 0 {
		return errors.New(strings.Join(cleanupErrors, "; "))
	}
	return nil
}
