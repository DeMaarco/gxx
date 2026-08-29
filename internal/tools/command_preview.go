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

package tools

import (
	"strings"
	"unicode"
)

var riskyCommandTokens = []string{
	"curl", "wget", "sudo", "scp", "ssh", "nc", "ncat", "netcat",
	"irm", "iwr", "iex",
	"invoke-webrequest", "invoke-restmethod", "invoke-expression",
}

func commandRiskNotes(command string) []string {
	var notes []string
	if hasParentDirectoryPath(command) {
		notes = append(notes, "warning: command includes a parent-directory path (..)")
	}
	if hasAbsolutePathToken(command) {
		notes = append(notes, "warning: command includes an absolute path")
	}
	if hasSensitivePathToken(command) {
		notes = append(notes, "warning: command may touch a sensitive path")
	}
	if hasRiskyCommandToken(command) {
		notes = append(notes, "warning: command includes a high-risk token (curl, wget, sudo, rm -rf, …)")
	}
	return notes
}

func hasParentDirectoryPath(command string) bool {
	for i := 0; i+1 < len(command); i++ {
		if command[i] != '.' || command[i+1] != '.' {
			continue
		}
		if i > 0 && command[i-1] == '.' {
			continue
		}
		if i+2 < len(command) && command[i+2] == '.' {
			continue
		}
		prevOK := i == 0 || isPathBoundary(command[i-1])
		nextOK := i+2 >= len(command) || isPathBoundary(command[i+2])
		if prevOK && nextOK {
			return true
		}
	}
	return false
}

func isPathBoundary(b byte) bool {
	return b == '/' || b == '\\' || unicode.IsSpace(rune(b)) || b == '"' || b == '\'' || b == '='
}

func hasAbsolutePathToken(command string) bool {
	for _, token := range commandTokens(command) {
		if isAbsolutePathToken(strings.Trim(token, `"'`)) {
			return true
		}
	}
	return false
}

func isAbsolutePathToken(token string) bool {
	if token == "~" || strings.HasPrefix(token, "/") || strings.HasPrefix(token, "~/") {
		return true
	}
	if strings.HasPrefix(token, `\\`) {
		return true
	}
	if len(token) >= 3 && isDriveLetter(token[0]) && token[1] == ':' && (token[2] == '\\' || token[2] == '/') {
		return true
	}
	return false
}

func isDriveLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func hasSensitivePathToken(command string) bool {
	for _, token := range commandTokens(command) {
		trimmed := strings.Trim(token, `"'`)
		if isSensitivePath(trimmed) {
			return true
		}
	}
	return false
}

func hasRiskyCommandToken(command string) bool {
	lower := strings.ToLower(command)
	if strings.Contains(lower, "rm -rf") || strings.Contains(lower, "rm -fr") {
		return true
	}
	if strings.Contains(lower, "remove-item") && strings.Contains(lower, "-recurse") {
		return true
	}
	if strings.Contains(lower, "del /s") || strings.Contains(lower, "rd /s") || strings.Contains(lower, "rmdir /s") {
		return true
	}
	for _, token := range commandTokens(command) {
		name := strings.ToLower(strings.Trim(token, `"'`))
		if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
			name = name[i+1:]
		}
		for _, risky := range riskyCommandTokens {
			if name == risky {
				return true
			}
		}
	}
	return false
}

func commandTokens(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		return unicode.IsSpace(r) || r == ';' || r == '|' || r == '&' || r == '<' || r == '>' || r == '(' || r == ')'
	})
}
