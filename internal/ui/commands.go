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
)

type slashCommand struct {
	name string
	help string
}

var slashCommands = []slashCommand{
	{name: "/help", help: "Show this help"},
	{name: "/model", help: "Select model, context size, effort, and fast"},
	{name: "/eco", help: "Caveman input saver: lite, full, ultra"},
	{name: "/compact", help: "Summarize older turns to free context"},
	{name: "/mode", help: "Select permission mode for agent: ask, auto-writes, or auto"},
	{name: "/config", help: "Set the OpenAI API key (same as /login api)"},
	{name: "/login", help: "Connect one account: openai, claude, or api"},
	{name: "/logout", help: "Clear the connected account"},
	{name: "/context", help: "Show context window occupancy"},
	{name: "/usage", help: "Show token usage, estimated cost, and remaining API quota"},
	{name: "/history", help: "Open saved conversations"},
	{name: "/clear", help: "Clear in-memory conversation"},
	{name: "/exit", help: "Exit gxx (Ctrl+C twice)"},
}

var slashCommandsWithArgs = map[string]struct{}{
	"/model":   {},
	"/mode":    {},
	"/eco":     {},
	"/compact": {},
	"/login":   {},
	"/logout":  {},
}

func matchingCommands(prefix string) []slashCommand {
	if prefix == "" || !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \t") {
		return nil
	}
	matches := make([]slashCommand, 0, len(slashCommands))
	for _, command := range slashCommands {
		if strings.HasPrefix(command.name, prefix) {
			matches = append(matches, command)
		}
	}
	return matches
}

func lookupSlashCommand(line string) (string, []string, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil, nil
	}
	name := fields[0]
	if name == "/quit" {
		name = "/exit"
	}
	if !knownSlashCommand(name) {
		return name, fields[1:], fmt.Errorf("unknown command %s", fields[0])
	}
	if len(fields) > 1 {
		if _, ok := slashCommandsWithArgs[name]; !ok {
			return name, fields[1:], fmt.Errorf("unexpected argument for %s", name)
		}
	}
	return name, fields[1:], nil
}

func knownSlashCommand(name string) bool {
	if name == "/exit" {
		return true
	}
	for _, command := range slashCommands {
		if command.name == name {
			return true
		}
	}
	return false
}
