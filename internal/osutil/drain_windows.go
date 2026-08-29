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

// DrainReadyInput drops keystrokes already sitting on the console so a leftover
// Enter from the raw REPL editor cannot answer the next approval prompt.
func DrainReadyInput(file *os.File) {
	if !isCharDevice(file) {
		return
	}
	if windows.FlushConsoleInputBuffer(windows.Handle(file.Fd())) == nil {
		return
	}
	drainDeadline(file)
}
