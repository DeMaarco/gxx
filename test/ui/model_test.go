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
	"bytes"
	"errors"
	"testing"

	"gxx/internal/ui"
)

func TestParseModelCommand(t *testing.T) {
	command, err := ui.ParseModelCommand("/model")
	if err != nil || !command.Show {
		t.Fatalf("parse /model = %+v, %v, want Show", command, err)
	}

	command, err = ui.ParseModelCommand("/model terra context=1m effort=high fast=on")
	if err != nil {
		t.Fatal(err)
	}
	if command.Model != "gpt-5.6-terra" || command.Context != "1m" || command.Effort != "high" {
		t.Fatalf("keyed fields = %+v", command)
	}
	if command.Fast == nil || !*command.Fast {
		t.Fatalf("fast = %v, want on", command.Fast)
	}

	command, err = ui.ParseModelCommand("/model context 128k fast off")
	if err != nil {
		t.Fatal(err)
	}
	if command.Model != "" || command.Context != "128k" {
		t.Fatalf("spaced fields = %+v", command)
	}
	if command.Fast == nil || *command.Fast {
		t.Fatalf("fast = %v, want off", command.Fast)
	}

	command, err = ui.ParseModelCommand("/model luna")
	if err != nil || command.Model != "gpt-5.6-luna" {
		t.Fatalf("alias = %+v, %v, want gpt-5.6-luna", command, err)
	}

	command, err = ui.ParseModelCommand("/model opus")
	if err != nil || command.Model != "claude-opus-5" {
		t.Fatalf("claude alias = %+v, %v, want claude-opus-5", command, err)
	}

	if _, err := ui.ParseModelCommand("/model effort bogus"); err == nil {
		t.Fatal("expected invalid effort error")
	}
	if _, err := ui.ParseModelCommand("/model extra leftover"); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}

func TestApplyModelCommandRollsBackOnSyncError(t *testing.T) {
	settings := ui.REPLSettings{
		Model:         "gpt-5.6-sol",
		Context:       "272k",
		Effort:        "medium",
		Fast:          false,
		ActiveAccount: "api",
		SyncSession: func(ui.REPLSettings) error {
			return errors.New("disk full")
		},
	}
	var output bytes.Buffer
	changed, err := ui.ApplyModelCommand(&output, &settings, "/model terra context=1m effort=high fast=on")
	if err == nil {
		t.Fatal("ApplyModelCommand() succeeded, want sync error")
	}
	if changed {
		t.Fatal("failed sync reported a model change")
	}
	if settings.Model != "gpt-5.6-sol" || settings.Context != "272k" || settings.Effort != "medium" || settings.Fast {
		t.Fatalf("settings mutated after failed sync: %+v", settings)
	}
}

func TestEncodeModelCommand(t *testing.T) {
	got := ui.EncodeModelCommand("gpt-5.6-sol", "272k", "medium", false)
	want := "/model gpt-5.6-sol context=272k effort=medium fast=off"
	if got != want {
		t.Fatalf("encode = %q, want %q", got, want)
	}
}

func TestCatalogModelsListsOpenAIAndClaude(t *testing.T) {
	got := ui.CatalogModels("gpt-5.6-sol")
	want := []string{
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
	}
	if len(got) != len(want) {
		t.Fatalf("catalog = %#v, want %#v", got, want)
	}
	for index, model := range want {
		if got[index] != model {
			t.Fatalf("catalog = %#v, want %#v", got, want)
		}
	}
}
