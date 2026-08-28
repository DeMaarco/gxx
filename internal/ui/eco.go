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

package ui

import (
	"fmt"
	"strings"

	"gxx/internal/config"
)

type ecoCommand struct {
	Toggle bool
	Level  int
}

func parseEcoCommand(line string) (ecoCommand, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "/eco" {
		return ecoCommand{}, fmt.Errorf("not an eco command")
	}
	if len(fields) == 1 {
		return ecoCommand{Toggle: true}, nil
	}
	if len(fields) > 2 {
		return ecoCommand{}, fmt.Errorf("unexpected eco argument %q", fields[2])
	}
	switch strings.ToLower(fields[1]) {
	case "off", "0", "false":
		return ecoCommand{Level: config.EcoOff}, nil
	case "1", "light", "lite":
		return ecoCommand{Level: 1}, nil
	case "2", "medium", "full":
		return ecoCommand{Level: 2}, nil
	case "3", "max", "ultra":
		return ecoCommand{Level: 3}, nil
	default:
		return ecoCommand{}, fmt.Errorf("eco must be lite, full, ultra, or off")
	}
}

func ecoLabel(level int) string {
	switch level {
	case 2:
		return "eco full"
	case 3:
		return "eco ultra"
	case 1:
		return "eco lite"
	default:
		return "eco off"
	}
}

func formatEcoStatus(settings REPLSettings) string {
	if settings.Eco <= 0 {
		return "eco off"
	}
	return ecoLabel(settings.Eco) + " · caveman input · model unchanged"
}

func ecoHelp(level int) string {
	switch level {
	case 1:
		return "lite · drop filler/hedging · compress tool prose · keep articles"
	case 2:
		return "full · drop articles · caveman · compress tools and old prompts"
	case 3:
		return "ultra · strip extra phrases · no reasoning replay · smallest payloads"
	default:
		return "off · send full conversation input"
	}
}
