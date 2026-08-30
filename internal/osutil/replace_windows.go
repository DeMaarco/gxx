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

//go:build windows

package osutil

import (
	"errors"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	replaceFileWriteThrough      = 0x00000001
	replaceFileIgnoreMergeErrors = 0x00000002
	replaceFileIgnoreACLErrors   = 0x00000004
)

var procReplaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// ReplaceFile installs oldpath at newpath. If newpath already exists it is
// overwritten in place so editors and watchers see an update, not a delete.
func ReplaceFile(oldpath, newpath string) error {
	_, err := os.Lstat(newpath)
	if errors.Is(err, os.ErrNotExist) {
		return windows.Rename(oldpath, newpath)
	}
	if err != nil {
		return err
	}
	if err := replaceExisting(oldpath, newpath); err == nil {
		return nil
	}
	if err := windows.Rename(oldpath, newpath); err == nil {
		return nil
	}
	return overwriteInPlace(oldpath, newpath)
}

func replaceExisting(oldpath, newpath string) error {
	replaced, err := windows.UTF16PtrFromString(newpath)
	if err != nil {
		return err
	}
	replacement, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return err
	}
	r1, _, callErr := procReplaceFileW.Call(
		uintptr(unsafe.Pointer(replaced)),
		uintptr(unsafe.Pointer(replacement)),
		0,
		replaceFileWriteThrough|replaceFileIgnoreMergeErrors|replaceFileIgnoreACLErrors,
		0,
		0,
	)
	if r1 == 0 {
		if callErr != nil {
			return callErr
		}
		return errors.New("replace file failed")
	}
	return nil
}

func overwriteInPlace(oldpath, newpath string) error {
	source, err := os.Open(oldpath)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.OpenFile(newpath, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dest, source); err != nil {
		_ = dest.Close()
		return err
	}
	if err := dest.Close(); err != nil {
		return err
	}
	return os.Remove(oldpath)
}
