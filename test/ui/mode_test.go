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

package ui_test

import (
	"testing"

	"gxx/internal/ui"

	"gxx/internal/config"
)

func TestParseModeCommand(t *testing.T) {
	command, err := ui.ParseModeCommand("/mode")
	if err != nil || !command.Show {
		t.Fatalf("parse /mode = %+v, %v, want Show", command, err)
	}

	command, err = ui.ParseModeCommand("/mode auto-writes")
	if err != nil || command.Mode != config.PermissionAutoWrites {
		t.Fatalf("auto-writes = %+v, %v", command, err)
	}

	command, err = ui.ParseModeCommand("/mode yolo")
	if err != nil || command.Mode != config.PermissionAuto {
		t.Fatalf("yolo alias = %+v, %v, want auto", command, err)
	}

	if _, err := ui.ParseModeCommand("/mode trust-me"); err == nil {
		t.Fatal("expected invalid mode error")
	}
	if _, err := ui.ParseModeCommand("/mode ask leftover"); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}

func TestEncodeModeCommand(t *testing.T) {
	got := ui.EncodeModeCommand(config.PermissionAutoWrites)
	if got != "/mode auto-writes" {
		t.Fatalf("encode = %q", got)
	}
}

func TestIsModePickerTextDoesNotMatchModel(t *testing.T) {
	if !ui.IsModePickerText("/mode") || !ui.IsModePickerText("/mode ask") {
		t.Fatal("expected /mode prefixes to match")
	}
	if ui.IsModePickerText("/model") || ui.IsModePickerText("/model terra") {
		t.Fatal("/model should not keep the mode picker open")
	}
}
