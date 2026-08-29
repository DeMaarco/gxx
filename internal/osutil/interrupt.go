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

package osutil

import (
	"os"
	"time"
)

// InterruptRead unblocks a pending Read on file after a context cancel.
// Unix pipes honor SetReadDeadline; Windows anonymous pipes need CancelIoEx.
func InterruptRead(file *os.File) {
	if file == nil {
		return
	}
	if file.SetReadDeadline(time.Now()) == nil {
		return
	}
	forceInterruptRead(file)
}

// ClearReadDeadline restores a blocking read deadline after InterruptRead.
func ClearReadDeadline(file *os.File) {
	if file == nil {
		return
	}
	_ = file.SetReadDeadline(time.Time{})
}
