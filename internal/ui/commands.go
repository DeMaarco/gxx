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
	{name: "/mode", help: "Select permission mode: ask, auto-writes, or auto"},
	{name: "/config", help: "Set and persist the OpenAI API key"},
	{name: "/context", help: "Show context window occupancy"},
	{name: "/usage", help: "Show token usage and remaining API quota"},
	{name: "/clear", help: "Clear in-memory conversation"},
	{name: "/exit", help: "Exit gxx (Ctrl+C twice)"},
}

var slashCommandsWithArgs = map[string]struct{}{
	"/model": {},
	"/mode":  {},
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
