package ui

import "strings"

type slashCommand struct {
	name string
	help string
}

var slashCommands = []slashCommand{
	{name: "/help", help: "Show this help"},
	{name: "/model", help: "Select model, context size, effort, and fast"},
	{name: "/config", help: "Set and persist the OpenAI API key"},
	{name: "/clear", help: "Clear in-memory conversation"},
	{name: "/exit", help: "Exit gxx"},
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
