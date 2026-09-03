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
	{name: "/skills", help: "List discovered Agent Skills"},
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

func unknownSlashHint(err error, token string, skills []string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if !strings.Contains(message, "unknown command") {
		return message
	}
	query := strings.TrimPrefix(strings.TrimSpace(token), "/")
	match, ok := matchSkillName(query, skills)
	if !ok {
		return message
	}
	if match == query {
		return fmt.Sprintf("usage: /%s <request>", match)
	}
	return fmt.Sprintf("unknown command %s. Did you mean /%s <request>?", token, match)
}

func rewriteSkillPrompt(line string, skills []string) (string, error) {
	names, request, err := parseSkillInvocation(line, skills)
	if err != nil {
		return "", err
	}
	return formatSkillPrompt(names, request), nil
}

func parseSkillInvocation(line string, skills []string) ([]string, string, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return nil, "", fmt.Errorf("unknown command")
	}
	if len(skills) == 0 {
		return nil, "", fmt.Errorf("unknown command %s", fields[0])
	}
	var names []string
	seen := map[string]bool{}
	i := 0
	for i < len(fields) {
		token := fields[i]
		if strings.HasPrefix(token, "/") {
			if knownSlashCommand(token) {
				break
			}
			match, ok := matchSkillName(strings.TrimPrefix(token, "/"), skills)
			if !ok {
				if len(names) == 0 {
					return nil, "", fmt.Errorf("unknown command %s", token)
				}
				break
			}
			if !seen[match] {
				seen[match] = true
				names = append(names, match)
			}
			i++
			continue
		}
		if isSkillConnector(token) && i+1 < len(fields) && strings.HasPrefix(fields[i+1], "/") {
			i++
			continue
		}
		break
	}
	if len(names) == 0 {
		return nil, "", fmt.Errorf("unknown command %s", fields[0])
	}
	request := strings.TrimSpace(strings.Join(fields[i:], " "))
	if request == "" {
		return nil, "", fmt.Errorf("usage: /%s <request>", names[0])
	}
	return names, request, nil
}

func formatSkillPrompt(names []string, request string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, `"`+name+`"`)
	}
	joined := strings.Join(quoted, " and ")
	process := "that skill's process"
	if len(names) > 1 {
		process = "those skills' process"
	}
	return "Call read_skill for " + joined + " before any other tool. Follow " + process + ", then do this request:\n\n" + request
}

func isSkillConnector(token string) bool {
	switch strings.ToLower(strings.Trim(token, ",;")) {
	case "y", "and", "e", "+":
		return true
	default:
		return false
	}
}

func matchSkillName(query string, names []string) (string, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return "", false
	}
	for _, name := range names {
		if name == query {
			return name, true
		}
	}
	best := ""
	bestDist := 3
	for _, name := range names {
		dist := levenshtein(query, name)
		if dist < bestDist || (dist == bestDist && (best == "" || name < best)) {
			bestDist = dist
			best = name
		}
	}
	if best != "" && bestDist <= 2 {
		return best, true
	}
	return "", false
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	if d := absInt(len(ra) - len(rb)); d > 2 {
		return d
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func skillNames(settings REPLSettings) []string {
	if settings.ListSkills == nil {
		return nil
	}
	entries := settings.ListSkills()
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
