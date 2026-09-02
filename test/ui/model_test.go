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
	"strings"
	"testing"

	"gxx/internal/config"
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

	command, err = ui.ParseModelCommand("/model context 272k fast off")
	if err != nil {
		t.Fatal(err)
	}
	if command.Model != "" || command.Context != "272k" {
		t.Fatalf("spaced fields = %+v", command)
	}
	if command.Fast == nil || *command.Fast {
		t.Fatalf("fast = %v, want off", command.Fast)
	}
	if _, err := ui.ParseModelCommand("/model context 128k"); err == nil {
		t.Fatal("expected removed context size error")
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

func TestApplyModelCommandRejectsUnsupportedContext(t *testing.T) {
	settings := ui.REPLSettings{
		Model:         "claude-haiku-4-5",
		Context:       "200k",
		Effort:        "medium",
		ActiveAccount: config.AccountClaude,
		SyncSession:   func(ui.REPLSettings) error { return nil },
	}
	var output bytes.Buffer
	_, err := ui.ApplyModelCommand(&output, &settings, "/model haiku context=1m")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v, want unsupported context", err)
	}
	if settings.Context != "200k" {
		t.Fatalf("context mutated = %q", settings.Context)
	}
}

func TestApplyModelCommandClampsContextOnModelSwitch(t *testing.T) {
	settings := ui.REPLSettings{
		Model:            "gpt-5.6-sol",
		Context:          "272k",
		Effort:           "medium",
		ActiveAccount:    config.AccountAPI,
		APIKeyConfigured: true,
		OpenAIConfigured: true,
		ClaudeConfigured: true,
		SyncSession:      func(ui.REPLSettings) error { return nil },
	}
	var output bytes.Buffer
	_, err := ui.ApplyModelCommand(&output, &settings, "/model sonnet")
	if err != nil {
		t.Fatalf("ApplyModelCommand() error = %v", err)
	}
	if settings.Context != "300k" {
		t.Fatalf("context = %q, want clamped 300k", settings.Context)
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

func TestApplyModelCommandSwitchesAccountWhenBothAreConfigured(t *testing.T) {
	settings := ui.REPLSettings{
		Model:            "gpt-5.6-sol",
		Context:          "272k",
		Effort:           "medium",
		ActiveAccount:    config.AccountAPI,
		APIKeyConfigured: true,
		OpenAIConfigured: true,
		ClaudeConfigured: true,
		SyncSession:      func(ui.REPLSettings) error { return nil },
	}
	var output bytes.Buffer
	changed, err := ui.ApplyModelCommand(&output, &settings, "/model sonnet")
	if err != nil {
		t.Fatalf("ApplyModelCommand() error = %v", err)
	}
	if !changed {
		t.Fatal("expected a model change")
	}
	if settings.Model != "claude-sonnet-5" {
		t.Fatalf("Model = %q, want claude-sonnet-5", settings.Model)
	}
	if settings.ActiveAccount != config.AccountClaude {
		t.Fatalf("ActiveAccount = %q, want claude", settings.ActiveAccount)
	}

	changed, err = ui.ApplyModelCommand(&output, &settings, "/model terra")
	if err != nil {
		t.Fatalf("switch back error = %v", err)
	}
	if !changed || settings.Model != "gpt-5.6-terra" {
		t.Fatalf("settings = %+v, want gpt-5.6-terra", settings)
	}
	if settings.ActiveAccount != config.AccountAPI {
		t.Fatalf("ActiveAccount = %q, want api", settings.ActiveAccount)
	}
}

func TestEncodeModelCommand(t *testing.T) {
	got := ui.EncodeModelCommand("gpt-5.6-sol", "272k", "medium", false)
	want := "/model gpt-5.6-sol context=272k effort=medium fast=off"
	if got != want {
		t.Fatalf("encode = %q, want %q", got, want)
	}
	got = ui.EncodeModelCommand("claude-sonnet-5", "1m", "high", true)
	want = "/model claude-sonnet-5 context=1m effort=high"
	if got != want {
		t.Fatalf("claude encode = %q, want %q", got, want)
	}
}

func TestApplyModelCommandRejectsFastOnClaude(t *testing.T) {
	settings := ui.REPLSettings{
		Model:            "claude-sonnet-5",
		Context:          "272k",
		Effort:           "medium",
		ActiveAccount:    config.AccountClaude,
		ClaudeConfigured: true,
		SyncSession:      func(ui.REPLSettings) error { return nil },
	}
	var output bytes.Buffer
	_, err := ui.ApplyModelCommand(&output, &settings, "/model fast=on")
	if err == nil || !strings.Contains(err.Error(), "OpenAI-only") {
		t.Fatalf("error = %v, want OpenAI-only", err)
	}
	if settings.Fast {
		t.Fatal("Fast should stay false on Claude")
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
