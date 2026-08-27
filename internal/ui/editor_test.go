package ui

import (
	"bytes"
	"strings"
	"testing"

	"gxx/internal/config"
)

func TestMatchingCommandsFiltersByPrefix(t *testing.T) {
	matches := matchingCommands("/c")
	if len(matches) != 2 || matches[0].name != "/config" || matches[1].name != "/clear" {
		t.Fatalf("matches = %#v, want /config and /clear", matches)
	}
	if matchingCommands("help") != nil || matchingCommands("/help extra") != nil {
		t.Fatal("matchingCommands() accepted a non-command prefix")
	}
}

func TestInputStateCompletesSlashCommands(t *testing.T) {
	var state inputState
	state.insert('/')
	if ghost := state.ghost(); ghost != "help" {
		t.Fatalf("ghost = %q, want help", ghost)
	}
	line, _, submitted := state.apply(keyEvent{kind: keyEnter})
	if !submitted || line != "/help" {
		t.Fatalf("enter on / = %q submitted=%v, want /help", line, submitted)
	}

	state = inputState{}
	for _, char := range "/c" {
		state.insert(char)
	}
	state.apply(keyEvent{kind: keyDown})
	state.apply(keyEvent{kind: keyTab})
	if state.text() != "/clear" {
		t.Fatalf("tab after down = %q, want /clear", state.text())
	}
}

func TestInputStateWalksHistoryWithArrows(t *testing.T) {
	state := inputState{}
	state.remember("first")
	state.remember("second")
	state.setText("draft")
	state.histPos = len(state.history)

	state.apply(keyEvent{kind: keyUp})
	if state.text() != "second" {
		t.Fatalf("up = %q, want second", state.text())
	}
	state.apply(keyEvent{kind: keyUp})
	if state.text() != "first" {
		t.Fatalf("second up = %q, want first", state.text())
	}
	state.apply(keyEvent{kind: keyDown})
	state.apply(keyEvent{kind: keyDown})
	if state.text() != "draft" {
		t.Fatalf("down to draft = %q", state.text())
	}
}

func TestReadKeyParsesArrowsAndRunes(t *testing.T) {
	event, err := readKey(bytes.NewReader([]byte{0x1b, '[', 'A'}))
	if err != nil || event.kind != keyUp {
		t.Fatalf("up arrow = %+v, %v", event, err)
	}
	event, err = readKey(strings.NewReader("ñ"))
	if err != nil || event.kind != keyRune || event.char != 'ñ' {
		t.Fatalf("utf8 rune = %+v, %v", event, err)
	}
	event, err = readKey(bytes.NewReader([]byte{0x09}))
	if err != nil || event.kind != keyTab {
		t.Fatalf("tab = %+v, %v", event, err)
	}
}

func TestInputStateModelPickerTabAndEnter(t *testing.T) {
	state := inputState{
		sessionModel:   "gpt-5.6-sol",
		sessionContext: "272k",
		sessionEffort:  "medium",
	}
	state.setText("/model")
	line, _, submitted := state.apply(keyEvent{kind: keyEnter})
	if !submitted || line != "" || state.picker != pickerModels {
		t.Fatalf("enter on /model = %q picker=%d submitted=%v, want open picker", line, state.picker, submitted)
	}

	state.apply(keyEvent{kind: keyDown})
	if state.selectedModel() != "gpt-5.6-terra" {
		t.Fatalf("selected model = %q, want gpt-5.6-terra", state.selectedModel())
	}
	state.apply(keyEvent{kind: keyTab})
	if state.picker != pickerOptions {
		t.Fatalf("tab picker = %d, want options", state.picker)
	}
	state.optionIndex = optionEffort
	state.apply(keyEvent{kind: keyRight})
	if state.pickEffort != "high" {
		t.Fatalf("effort = %q, want high", state.pickEffort)
	}
	state.optionIndex = optionContext
	state.apply(keyEvent{kind: keyRight})
	if state.pickContext != "1m" {
		t.Fatalf("context = %q, want 1m", state.pickContext)
	}
	state.optionIndex = optionFast
	state.apply(keyEvent{kind: keyRight})
	if !state.pickFast {
		t.Fatal("fast should be on")
	}

	line, _, submitted = state.apply(keyEvent{kind: keyEnter})
	want := "/model gpt-5.6-terra context=1m effort=high fast=on"
	if !submitted || line != want {
		t.Fatalf("apply = %q submitted=%v, want %q", line, submitted, want)
	}
}

func TestInputStateTabOnModelCommandOpensPicker(t *testing.T) {
	state := inputState{sessionModel: "gpt-5.6-sol", sessionEffort: "medium"}
	for _, char := range "/m" {
		state.insert(char)
	}
	state.apply(keyEvent{kind: keyTab})
	if state.text() != "/model" || state.picker != pickerModels {
		t.Fatalf("tab /m = %q picker=%d, want /model picker", state.text(), state.picker)
	}
}

func TestInputStateModePickerTabAndEnter(t *testing.T) {
	state := inputState{sessionPermission: config.PermissionAsk}
	state.setText("/mode")
	line, _, submitted := state.apply(keyEvent{kind: keyEnter})
	if !submitted || line != "" || state.picker != pickerModes {
		t.Fatalf("enter on /mode = %q picker=%d submitted=%v, want open picker", line, state.picker, submitted)
	}
	if state.selectedPermission() != config.PermissionAsk {
		t.Fatalf("selected mode = %q, want ask", state.selectedPermission())
	}

	state.apply(keyEvent{kind: keyDown})
	if state.selectedPermission() != config.PermissionAutoWrites {
		t.Fatalf("selected mode = %q, want auto-writes", state.selectedPermission())
	}
	state.apply(keyEvent{kind: keyDown})
	if state.selectedPermission() != config.PermissionAuto {
		t.Fatalf("selected mode = %q, want auto", state.selectedPermission())
	}

	line, _, submitted = state.apply(keyEvent{kind: keyEnter})
	if !submitted || line != "/mode auto" {
		t.Fatalf("apply = %q submitted=%v, want /mode auto", line, submitted)
	}
}

func TestInputStateTabOnModeCommandOpensPicker(t *testing.T) {
	state := inputState{sessionPermission: config.PermissionAsk}
	for _, char := range "/mode" {
		state.insert(char)
	}
	state.apply(keyEvent{kind: keyTab})
	if state.text() != "/mode" || state.picker != pickerModes {
		t.Fatalf("tab /mode = %q picker=%d, want mode picker", state.text(), state.picker)
	}
}
