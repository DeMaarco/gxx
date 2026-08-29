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
	"os"

	"golang.org/x/sys/windows"
)

const enableVirtualTerminalProcessing uint32 = 0x0004

// EnableConsoleVT turns on ANSI cursor/erase sequences for a Windows console.
// Without this, picker redraws print a new copy under the last one.
func EnableConsoleVT(file *os.File) func() {
	if file == nil {
		return func() {}
	}
	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return func() {}
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return func() {}
	}
	if err := windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing); err != nil {
		return func() {}
	}
	return func() {
		_ = windows.SetConsoleMode(handle, mode)
	}
}
