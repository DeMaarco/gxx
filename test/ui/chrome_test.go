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
	"strings"
	"testing"

	"gxx/internal/ui"

	"gxx/internal/config"
)

func TestWriteChromePrintsHeaderPromptAndStatus(t *testing.T) {
	var output bytes.Buffer
	err := ui.WriteChrome(&output, ui.REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
	})
	if err != nil {
		t.Fatalf("writeChrome() error = %v", err)
	}
	got := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	want := []string{
		"◆ gxx  v0.0.1",
		"",
		">",
		"  gpt-5.6-sol  ·  ask  ·  medium  ·  272k  ·  (0%)",
	}
	if len(got) != len(want) {
		t.Fatalf("chrome lines = %#v, want %#v", got, want)
	}
	for index, line := range want {
		if got[index] != line {
			t.Fatalf("line %d = %q, want %q", index+1, got[index], line)
		}
	}
}

func TestWriteAskHeaderOmitsPrompt(t *testing.T) {
	var output bytes.Buffer
	if err := ui.WriteAskHeader(&output, ui.REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: "ask",
		Effort:         "high",
		Color:          false,
	}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.HasPrefix(text, "◆ gxx  v0.0.1\n") {
		t.Fatalf("header = %q", text)
	}
	if !strings.Contains(text, "gpt-5.6-sol  ·  ask  ·  high  ·  272k  ·  (0%)") {
		t.Fatalf("status missing from %q", text)
	}
	if strings.Contains(text, "\n>\n") {
		t.Fatalf("ask header should not include the prompt: %q", text)
	}
}

func TestFormatStatusAddsContextAndFast(t *testing.T) {
	plain := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
	})
	if plain != "gpt-5.6-sol  ·  ask  ·  medium  ·  272k  ·  (0%)" {
		t.Fatalf("default status = %q", plain)
	}
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-terra",
		PermissionMode: config.PermissionAsk,
		Effort:         "high",
		Context:        "1m",
		Fast:           true,
	})
	if got != "gpt-5.6-terra  ·  ask  ·  high  ·  1m  ·  (0%)  ·  fast" {
		t.Fatalf("status = %q", got)
	}
}

func TestFormatStatusHidesFastForClaude(t *testing.T) {
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "claude-sonnet-5",
		PermissionMode: config.PermissionAsk,
		Effort:         "high",
		Context:        "272k",
		Fast:           true,
		ActiveAccount:  config.AccountClaude,
	})
	if strings.Contains(got, "fast") {
		t.Fatalf("claude status = %q, want no fast badge", got)
	}
}

func TestFormatStatusShowsPlan(t *testing.T) {
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
		Plan:           true,
	})
	if got != "gpt-5.6-sol  ·  ask  ·  medium  ·  272k  ·  (0%)" {
		t.Fatalf("plan should not appear in status = %q", got)
	}
	if prefix := ui.PromptPrefix(ui.REPLSettings{Plan: true}); prefix != "> plan " {
		t.Fatalf("prompt prefix = %q, want %q", prefix, "> plan ")
	}
	if prefix := ui.PromptPrefix(ui.REPLSettings{Ask: true}); prefix != "> ask " {
		t.Fatalf("ask prompt prefix = %q, want %q", prefix, "> ask ")
	}
	if prefix := ui.PromptPrefix(ui.REPLSettings{Ask: true, Plan: true}); prefix != "> plan " {
		t.Fatalf("ask+plan prefix = %q, want plan only", prefix)
	}
	if prefix := ui.PromptPrefix(ui.REPLSettings{}); prefix != "> " {
		t.Fatalf("agent prompt prefix = %q, want %q", prefix, "> ")
	}
	colored := ui.PromptPrefix(ui.REPLSettings{Plan: true, Color: true})
	if !strings.Contains(colored, ui.Paint(true, ui.ColorYellow, "plan")) {
		t.Fatalf("colored plan prefix = %q, want yellow plan", colored)
	}
	coloredAsk := ui.PromptPrefix(ui.REPLSettings{Ask: true, Color: true})
	if !strings.Contains(coloredAsk, ui.Paint(true, ui.ColorCyan, "ask")) {
		t.Fatalf("colored ask prefix = %q, want cyan ask", coloredAsk)
	}
}

func TestPromptPrefixShowsEcoInGreen(t *testing.T) {
	if prefix := ui.PromptPrefix(ui.REPLSettings{Eco: 1}); prefix != "> eco lite " {
		t.Fatalf("eco prefix = %q, want %q", prefix, "> eco lite ")
	}
	if prefix := ui.PromptPrefix(ui.REPLSettings{Eco: 2}); prefix != "> eco full " {
		t.Fatalf("eco 2 prefix = %q, want %q", prefix, "> eco full ")
	}
	if prefix := ui.PromptPrefix(ui.REPLSettings{Plan: true, Eco: 3}); prefix != "> plan eco ultra " {
		t.Fatalf("plan+eco prefix = %q", prefix)
	}
	colored := ui.PromptPrefix(ui.REPLSettings{Eco: 2, Color: true})
	if !strings.Contains(colored, ui.Paint(true, ui.ColorGreen, "eco full")) {
		t.Fatalf("colored eco prefix = %q, want green eco full", colored)
	}
}

func TestFormatStatusIgnoresEcoForModel(t *testing.T) {
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-luna",
		PermissionMode: config.PermissionAsk,
		Effort:         "max",
		Context:        "1m",
		Fast:           true,
		Eco:            2,
	})
	if got != "gpt-5.6-luna  ·  ask  ·  max  ·  1m  ·  (0%)  ·  fast" {
		t.Fatalf("eco must not change status model = %q", got)
	}
}

func TestWriteChromeShowsAskAfterPrompt(t *testing.T) {
	var output bytes.Buffer
	err := ui.WriteChrome(&output, ui.REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAutoWrites,
		Effort:         "medium",
		Ask:            true,
	})
	if err != nil {
		t.Fatalf("writeChrome() error = %v", err)
	}
	got := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	want := []string{
		"◆ gxx  v0.0.1",
		"",
		"> ask",
		"  gpt-5.6-sol  ·  auto-writes  ·  medium  ·  272k  ·  (0%)",
	}
	if len(got) != len(want) {
		t.Fatalf("chrome lines = %#v, want %#v", got, want)
	}
	for index, line := range want {
		if got[index] != line {
			t.Fatalf("line %d = %q, want %q", index+1, got[index], line)
		}
	}
}

func TestWriteChromeShowsPlanAfterPrompt(t *testing.T) {
	var output bytes.Buffer
	err := ui.WriteChrome(&output, ui.REPLSettings{
		Version:        "0.0.1",
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
		Plan:           true,
	})
	if err != nil {
		t.Fatalf("writeChrome() error = %v", err)
	}
	got := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	want := []string{
		"◆ gxx  v0.0.1",
		"",
		"> plan",
		"  gpt-5.6-sol  ·  ask  ·  medium  ·  272k  ·  (0%)",
	}
	if len(got) != len(want) {
		t.Fatalf("chrome lines = %#v, want %#v", got, want)
	}
	for index, line := range want {
		if got[index] != line {
			t.Fatalf("line %d = %q, want %q", index+1, got[index], line)
		}
	}
}

func TestFormatStatusPaintsAutoRed(t *testing.T) {
	plain := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAuto,
		Effort:         "medium",
	})
	if plain != "gpt-5.6-sol  ·  auto  ·  medium  ·  272k  ·  (0%)" {
		t.Fatalf("plain auto status = %q", plain)
	}
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAuto,
		Effort:         "medium",
		Color:          true,
	})
	if !strings.Contains(got, ui.Paint(true, ui.ColorBold+ui.ColorRed, "auto")) {
		t.Fatalf("colored auto status = %q, want red auto", got)
	}
	if strings.Contains(got, ui.Paint(true, ui.ColorBold+ui.ColorRed, "ask")) {
		t.Fatalf("colored auto status painted ask: %q", got)
	}
}

func TestFormatStatusKeepsAutoWritesPlain(t *testing.T) {
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAutoWrites,
		Effort:         "medium",
		Color:          true,
	})
	if strings.Contains(got, ui.Paint(true, ui.ColorRed, "auto-writes")) ||
		strings.Contains(got, ui.Paint(true, ui.ColorBold+ui.ColorRed, "auto-writes")) {
		t.Fatalf("auto-writes should not be red: %q", got)
	}
	if !strings.Contains(got, "auto-writes") {
		t.Fatalf("status = %q, want auto-writes", got)
	}
}

func TestFormatVersionPrefixesRelease(t *testing.T) {
	if got := ui.FormatVersion("0.0.1"); got != "v0.0.1" {
		t.Fatalf("formatVersion(0.0.1) = %q", got)
	}
	if got := ui.FormatVersion("v0.0.1"); got != "v0.0.1" {
		t.Fatalf("formatVersion(v0.0.1) = %q", got)
	}
	if got := ui.FormatVersion(""); got != "dev" {
		t.Fatalf("formatVersion(empty) = %q", got)
	}
}
