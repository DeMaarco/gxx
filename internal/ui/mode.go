package ui

import (
	"fmt"
	"strings"

	"gxx/internal/config"
)

type modeCommand struct {
	Show bool
	Mode string
}

func parseModeCommand(line string) (modeCommand, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/mode" {
		return modeCommand{}, fmt.Errorf("not a mode command")
	}
	if len(fields) == 1 {
		return modeCommand{Show: true}, nil
	}
	if len(fields) > 2 {
		return modeCommand{}, fmt.Errorf("unexpected mode argument %q", fields[2])
	}
	mode, err := config.CanonicalPermission(fields[1])
	if err != nil {
		return modeCommand{}, err
	}
	return modeCommand{Mode: mode}, nil
}

func encodeModeCommand(mode string) string {
	return "/mode " + mode
}

func formatModeStatus(mode string) string {
	switch mode {
	case config.PermissionAutoWrites:
		return "permission auto-writes · file changes run without confirmation; commands still ask"
	case config.PermissionAuto:
		return "permission auto · file changes and commands run without confirmation"
	default:
		return "permission ask · confirm every file change and command"
	}
}

func permissionHelp(mode string) string {
	switch mode {
	case config.PermissionAutoWrites:
		return "file changes without confirmation; commands still ask"
	case config.PermissionAuto:
		return "all file changes and commands without confirmation"
	default:
		return "confirm every file change and command"
	}
}

func paintPermission(color bool, mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = config.PermissionAsk
	}
	if mode == config.PermissionAuto {
		return paint(color, bold+red, mode)
	}
	return paint(color, dim, mode)
}

func isModePickerText(text string) bool {
	return strings.HasPrefix(text, "/mode") && !strings.HasPrefix(text, "/model")
}
