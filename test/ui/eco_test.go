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
	"strings"
	"testing"

	"gxx/internal/config"
	"gxx/internal/ui"
)

func TestParseEcoCommand(t *testing.T) {
	command, err := ui.ParseEcoCommand("/eco")
	if err != nil || !command.Toggle {
		t.Fatalf("parse /eco = %+v, %v, want Toggle", command, err)
	}

	command, err = ui.ParseEcoCommand("/eco 2")
	if err != nil || command.Level != 2 || command.Toggle {
		t.Fatalf("parse /eco 2 = %+v, %v", command, err)
	}

	command, err = ui.ParseEcoCommand("/eco full")
	if err != nil || command.Level != 2 {
		t.Fatalf("parse /eco full = %+v, %v", command, err)
	}

	command, err = ui.ParseEcoCommand("/eco ultra")
	if err != nil || command.Level != 3 {
		t.Fatalf("parse /eco ultra = %+v, %v", command, err)
	}

	command, err = ui.ParseEcoCommand("/eco off")
	if err != nil || command.Level != config.EcoOff || command.Toggle {
		t.Fatalf("parse /eco off = %+v, %v", command, err)
	}

	if _, err := ui.ParseEcoCommand("/eco 4"); err == nil {
		t.Fatal("expected invalid eco level")
	}
}

func TestEcoLabelAndStatus(t *testing.T) {
	if got := ui.EcoLabel(1); got != "eco lite" {
		t.Fatalf("label 1 = %q", got)
	}
	if got := ui.EcoLabel(3); got != "eco ultra" {
		t.Fatalf("label 3 = %q", got)
	}
	got := ui.FormatEcoStatus(ui.REPLSettings{
		Model:   "gpt-5.6-luna",
		Effort:  "max",
		Context: "1m",
		Fast:    true,
		Eco:     2,
	})
	if !strings.Contains(got, "eco full") || !strings.Contains(got, "caveman") {
		t.Fatalf("status = %q, want eco full caveman", got)
	}
	if strings.Contains(got, "gpt-5.6-sol") {
		t.Fatalf("status = %q, eco must not mention sol", got)
	}
}
