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

	"gxx/internal/config"
	"gxx/internal/ui"
)

func TestMatchingCommandsFiltersByPrefix(t *testing.T) {
	matches := ui.MatchingCommands("/c")
	if len(matches) != 3 || matches[0].Name() != "/config" || matches[1].Name() != "/context" || matches[2].Name() != "/clear" {
		t.Fatalf("matches = %#v, want /config, /context, and /clear", matches)
	}
	if ui.MatchingCommands("help") != nil || ui.MatchingCommands("/help extra") != nil {
		t.Fatal("matchingCommands() accepted a non-command prefix")
	}
	usage := ui.MatchingCommands("/u")
	if len(usage) != 1 || usage[0].Name() != "/usage" {
		t.Fatalf("matches = %#v, want /usage", usage)
	}
}

func TestInputStateCompletesSlashCommands(t *testing.T) {
	var state ui.InputState
	state.Insert('/')
	if ghost := state.Ghost(); ghost != "help" {
		t.Fatalf("ghost = %q, want help", ghost)
	}
	line, _, submitted := state.Apply(ui.KeyEnter)
	if !submitted || line != "/help" {
		t.Fatalf("enter on / = %q submitted=%v, want /help", line, submitted)
	}

	state = ui.InputState{}
	for _, char := range "/c" {
		state.Insert(char)
	}
	state.Apply(ui.KeyDown)
	state.Apply(ui.KeyDown)
	state.Apply(ui.KeyTab)
	if state.Text() != "/clear" {
		t.Fatalf("tab after down = %q, want /clear", state.Text())
	}
}

func TestInputStateWalksHistoryWithArrows(t *testing.T) {
	state := ui.InputState{}
	state.Remember("first")
	state.Remember("second")
	state.SetText("draft")
	state.SetHistPosToEnd()

	state.Apply(ui.KeyUp)
	if state.Text() != "second" {
		t.Fatalf("up = %q, want second", state.Text())
	}
	state.Apply(ui.KeyUp)
	if state.Text() != "first" {
		t.Fatalf("second up = %q, want first", state.Text())
	}
	state.Apply(ui.KeyDown)
	state.Apply(ui.KeyDown)
	if state.Text() != "draft" {
		t.Fatalf("down to draft = %q", state.Text())
	}
}

func TestReadKeyParsesArrowsAndRunes(t *testing.T) {
	event, err := ui.ReadKey(bytes.NewReader([]byte{0x1b, '[', 'A'}))
	if err != nil || event.Kind != ui.KeyUp {
		t.Fatalf("up arrow = %+v, %v", event, err)
	}
	event, err = ui.ReadKey(strings.NewReader("ñ"))
	if err != nil || event.Kind != ui.KeyRune || event.Char != 'ñ' {
		t.Fatalf("utf8 rune = %+v, %v", event, err)
	}
	event, err = ui.ReadKey(bytes.NewReader([]byte{0x09}))
	if err != nil || event.Kind != ui.KeyTab {
		t.Fatalf("tab = %+v, %v", event, err)
	}
	event, err = ui.ReadKey(bytes.NewReader([]byte{0x1b, '[', 'Z'}))
	if err != nil || event.Kind != ui.KeyShiftTab {
		t.Fatalf("shift+tab = %+v, %v", event, err)
	}
	event, err = ui.ReadKey(bytes.NewReader([]byte{0x1b, '[', '1', ';', '2', 'Z'}))
	if err != nil || event.Kind != ui.KeyShiftTab {
		t.Fatalf("csi shift+tab = %+v, %v", event, err)
	}
	event, err = ui.ReadKey(bytes.NewReader([]byte{0x1b, '[', '3', '~'}))
	if err != nil || event.Kind != ui.KeyDelete {
		t.Fatalf("delete = %+v, %v", event, err)
	}
}

func TestWrapVisibleSplitsOnWidth(t *testing.T) {
	got := ui.WrapVisible("abcdef", 3)
	if len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("wrap = %#v", got)
	}
	if lines := ui.WrapVisible("ab", 5); len(lines) != 1 || lines[0] != "ab" {
		t.Fatalf("short wrap = %#v", lines)
	}
	if lines := ui.WrapVisible("", 5); len(lines) != 1 || lines[0] != "" {
		t.Fatalf("empty wrap = %#v", lines)
	}
}

func TestPromptFrameWrapsWithoutRepeatingPrefix(t *testing.T) {
	settings := ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: config.PermissionAsk,
		Effort:         "medium",
	}
	text := strings.Repeat("x", 40)
	var first bytes.Buffer
	cursorRow, err := ui.RenderPromptFrame(&first, settings, text, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := first.String()
	if cursorRow < 1 {
		t.Fatalf("cursor row = %d, want wrapped", cursorRow)
	}
	if strings.Contains(got, strings.Repeat("x", 21)) {
		t.Fatalf("prompt should wrap before exceeding the width: %q", got)
	}
	if strings.Count(got, "> ") != 1 {
		t.Fatalf("wrapped prompt reprinted the prefix: %q", got)
	}
	if !strings.Contains(got, "\r\n") {
		t.Fatalf("wrapped prompt should break to the next line: %q", got)
	}

	var second bytes.Buffer
	if _, err := ui.RenderPromptFrame(&second, settings, text+"yz", 20, cursorRow); err != nil {
		t.Fatal(err)
	}
	redraw := second.String()
	if !strings.HasPrefix(redraw, ui.PromptHome(cursorRow)) {
		t.Fatalf("redraw should return to the first prompt row, got %q", redraw)
	}
	if strings.Count(redraw, "> ") != 1 {
		t.Fatalf("redraw reprinted the prefix: %q", redraw)
	}
}

func TestTogglePlanFlipsSessionFlag(t *testing.T) {
	var applied bool
	settings := ui.REPLSettings{
		SetPlan: func(plan bool) error {
			applied = plan
			return nil
		},
	}
	ui.TogglePlan(&settings)
	if !settings.Plan || !applied {
		t.Fatalf("first toggle plan=%v applied=%v", settings.Plan, applied)
	}
	ui.TogglePlan(&settings)
	if settings.Plan || applied {
		t.Fatalf("second toggle plan=%v applied=%v", settings.Plan, applied)
	}
}

func TestInputStateModelPickerTabAndEnter(t *testing.T) {
	state := ui.InputState{}
	state.SetSession("gpt-5.6-sol", "272k", "medium", "")
	state.SetText("/model")
	line, _, submitted := state.Apply(ui.KeyEnter)
	if !submitted || line != "" || state.Picker() != ui.PickerModels {
		t.Fatalf("enter on /model = %q picker=%d submitted=%v, want open picker", line, state.Picker(), submitted)
	}

	state.Apply(ui.KeyDown)
	if state.SelectedModel() != "gpt-5.6-terra" {
		t.Fatalf("selected model = %q, want gpt-5.6-terra", state.SelectedModel())
	}
	state.Apply(ui.KeyTab)
	if state.Picker() != ui.PickerOptions {
		t.Fatalf("tab picker = %d, want options", state.Picker())
	}
	state.SetOptionIndex(ui.OptionEffort)
	state.Apply(ui.KeyRight)
	if state.PickEffort() != "high" {
		t.Fatalf("effort = %q, want high", state.PickEffort())
	}
	state.SetOptionIndex(ui.OptionContext)
	state.Apply(ui.KeyRight)
	if state.PickContext() != "1m" {
		t.Fatalf("context = %q, want 1m", state.PickContext())
	}
	state.SetOptionIndex(ui.OptionFast)
	state.Apply(ui.KeyRight)
	if !state.PickFast() {
		t.Fatal("fast should be on")
	}

	line, _, submitted = state.Apply(ui.KeyEnter)
	want := "/model gpt-5.6-terra context=1m effort=high fast=on"
	if !submitted || line != want {
		t.Fatalf("apply = %q submitted=%v, want %q", line, submitted, want)
	}
}

func TestInputStateLoginPickerSelectsAccount(t *testing.T) {
	state := ui.InputState{}
	state.SetText("/login")
	line, _, submitted := state.Apply(ui.KeyEnter)
	if !submitted || line != "" || state.Picker() != ui.PickerLogin {
		t.Fatalf("enter on /login = %q picker=%d submitted=%v", line, state.Picker(), submitted)
	}
	state.Apply(ui.KeyDown)
	if state.SelectedLogin() != "claude" {
		t.Fatalf("selected login = %q, want claude", state.SelectedLogin())
	}
	line, _, submitted = state.Apply(ui.KeyEnter)
	if !submitted || line != "/login claude" {
		t.Fatalf("apply = %q submitted=%v", line, submitted)
	}
}

func TestWindowRangeKeepsSelectionInView(t *testing.T) {
	start, end := ui.WindowRange(40, 0, 8)
	if start != 0 || end != 8 {
		t.Fatalf("top window = %d:%d, want 0:8", start, end)
	}
	start, end = ui.WindowRange(40, 39, 8)
	if start != 32 || end != 40 {
		t.Fatalf("bottom window = %d:%d, want 32:40", start, end)
	}
	start, end = ui.WindowRange(40, 20, 8)
	if start > 20 || end <= 20 || end-start != 8 {
		t.Fatalf("mid window = %d:%d, want 8 rows containing 20", start, end)
	}
	start, end = ui.WindowRange(5, 2, 8)
	if start != 0 || end != 5 {
		t.Fatalf("short list = %d:%d, want 0:5", start, end)
	}
}

func TestInputStateTabOnModelCommandOpensPicker(t *testing.T) {
	state := ui.InputState{}
	state.SetSession("gpt-5.6-sol", "", "medium", "")
	for _, char := range "/m" {
		state.Insert(char)
	}
	state.Apply(ui.KeyTab)
	if state.Text() != "/model" || state.Picker() != ui.PickerModels {
		t.Fatalf("tab /m = %q picker=%d, want /model picker", state.Text(), state.Picker())
	}
}

func TestInputStateModePickerTabAndEnter(t *testing.T) {
	state := ui.InputState{}
	state.SetSession("", "", "", config.PermissionAsk)
	state.SetText("/mode")
	line, _, submitted := state.Apply(ui.KeyEnter)
	if !submitted || line != "" || state.Picker() != ui.PickerModes {
		t.Fatalf("enter on /mode = %q picker=%d submitted=%v, want open picker", line, state.Picker(), submitted)
	}
	if state.SelectedPermission() != config.PermissionAsk {
		t.Fatalf("selected mode = %q, want ask", state.SelectedPermission())
	}

	state.Apply(ui.KeyDown)
	if state.SelectedPermission() != config.PermissionAutoWrites {
		t.Fatalf("selected mode = %q, want auto-writes", state.SelectedPermission())
	}
	state.Apply(ui.KeyDown)
	if state.SelectedPermission() != config.PermissionAuto {
		t.Fatalf("selected mode = %q, want auto", state.SelectedPermission())
	}

	line, _, submitted = state.Apply(ui.KeyEnter)
	if !submitted || line != "/mode auto" {
		t.Fatalf("apply = %q submitted=%v, want /mode auto", line, submitted)
	}
}

func TestInputStateTabOnModeCommandOpensPicker(t *testing.T) {
	state := ui.InputState{}
	state.SetSession("", "", "", config.PermissionAsk)
	for _, char := range "/mode" {
		state.Insert(char)
	}
	state.Apply(ui.KeyTab)
	if state.Text() != "/mode" || state.Picker() != ui.PickerModes {
		t.Fatalf("tab /mode = %q picker=%d, want mode picker", state.Text(), state.Picker())
	}
}

func TestInputStateTabOnContextCommandOpensPicker(t *testing.T) {
	var state ui.InputState
	for _, char := range "/context" {
		state.Insert(char)
	}
	state.Apply(ui.KeyTab)
	if state.Text() != "/context" || state.Picker() != ui.PickerContext {
		t.Fatalf("tab /context = %q picker=%d, want context picker", state.Text(), state.Picker())
	}
	line, _, submitted := state.Apply(ui.KeyEnter)
	if !submitted || line != "" || state.Picker() != ui.PickerClosed {
		t.Fatalf("enter on context picker = %q picker=%d submitted=%v, want close", line, state.Picker(), submitted)
	}
}

func TestInputStateCtrlCConfirmsBeforeExit(t *testing.T) {
	var state ui.InputState
	_, eof, handled := state.Apply(ui.KeyCtrlC)
	if eof || handled || !state.ExitArmed() {
		t.Fatal("first ctrl+c on an empty prompt should arm exit, not leave")
	}
	_, eof, handled = state.Apply(ui.KeyCtrlC)
	if !eof || !handled {
		t.Fatal("second ctrl+c on an empty prompt should exit")
	}

	state = ui.InputState{}
	state.Apply(ui.KeyCtrlC)
	state.Insert('h')
	if state.ExitArmed() {
		t.Fatal("typing should cancel the exit confirmation")
	}

	state.Insert('i')
	line, eof, _ := state.Apply(ui.KeyCtrlC)
	if eof || line != "" || state.Text() != "" || state.ExitArmed() {
		t.Fatalf("ctrl+c with text = line %q eof=%v text=%q armed=%v, want clear without exit", line, eof, state.Text(), state.ExitArmed())
	}
	_, eof, _ = state.Apply(ui.KeyCtrlC)
	if eof || !state.ExitArmed() {
		t.Fatal("ctrl+c after clearing text should confirm, not exit")
	}

	state.SetText("/mode")
	state.Apply(ui.KeyTab)
	if state.Picker() == ui.PickerClosed {
		t.Fatal("want mode picker open")
	}
	_, eof, _ = state.Apply(ui.KeyCtrlC)
	if eof || state.Picker() != ui.PickerClosed || state.Text() != "" || state.ExitArmed() {
		t.Fatalf("ctrl+c in picker = eof=%v picker=%d text=%q armed=%v, want dismiss", eof, state.Picker(), state.Text(), state.ExitArmed())
	}
}
