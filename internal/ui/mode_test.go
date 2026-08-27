package ui

import (
	"testing"

	"gxx/internal/config"
)

func TestParseModeCommand(t *testing.T) {
	command, err := parseModeCommand("/mode")
	if err != nil || !command.Show {
		t.Fatalf("parse /mode = %+v, %v, want Show", command, err)
	}

	command, err = parseModeCommand("/mode auto-writes")
	if err != nil || command.Mode != config.PermissionAutoWrites {
		t.Fatalf("auto-writes = %+v, %v", command, err)
	}

	command, err = parseModeCommand("/mode yolo")
	if err != nil || command.Mode != config.PermissionAuto {
		t.Fatalf("yolo alias = %+v, %v, want auto", command, err)
	}

	if _, err := parseModeCommand("/mode trust-me"); err == nil {
		t.Fatal("expected invalid mode error")
	}
	if _, err := parseModeCommand("/mode ask leftover"); err == nil {
		t.Fatal("expected unexpected argument error")
	}
}

func TestEncodeModeCommand(t *testing.T) {
	got := encodeModeCommand(config.PermissionAutoWrites)
	if got != "/mode auto-writes" {
		t.Fatalf("encode = %q", got)
	}
}

func TestIsModePickerTextDoesNotMatchModel(t *testing.T) {
	if !isModePickerText("/mode") || !isModePickerText("/mode ask") {
		t.Fatal("expected /mode prefixes to match")
	}
	if isModePickerText("/model") || isModePickerText("/model terra") {
		t.Fatal("/model should not keep the mode picker open")
	}
}
