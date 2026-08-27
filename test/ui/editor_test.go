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
