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

package skills

import (
	"errors"
	"strings"
	"unicode"
)

// parseSkillMD extracts required frontmatter fields and the markdown body.
// It uses a minimal line parser (no YAML dependency) and ignores optional fields.
func parseSkillMD(data []byte, dirName string) (name, description, body string, err error) {
	front, body, err := splitFrontmatter(string(data))
	if err != nil {
		return "", "", "", err
	}
	fields := parseFrontmatterFields(front)
	name = strings.TrimSpace(fields["name"])
	description = strings.TrimSpace(fields["description"])
	if err := validateName(name); err != nil {
		return "", "", "", err
	}
	if name != dirName {
		return "", "", "", errors.New("skill name does not match directory")
	}
	if err := validateDescription(description); err != nil {
		return "", "", "", err
	}
	return name, description, strings.TrimSpace(body), nil
}

func splitFrontmatter(text string) (front, body string, err error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	if !strings.HasPrefix(text, "---") {
		return "", "", errors.New("missing frontmatter")
	}
	rest := text[3:]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	} else {
		return "", "", errors.New("missing frontmatter")
	}
	closing := "\n---"
	idx := strings.Index(rest, closing)
	crlfClosing := "\r\n---"
	crlfIdx := strings.Index(rest, crlfClosing)
	switch {
	case idx < 0 && crlfIdx < 0:
		return "", "", errors.New("unclosed frontmatter")
	case crlfIdx >= 0 && (idx < 0 || crlfIdx < idx):
		front = rest[:crlfIdx]
		body = rest[crlfIdx+len(crlfClosing):]
	default:
		front = rest[:idx]
		body = rest[idx+len(closing):]
	}
	if strings.HasPrefix(body, "\r\n") {
		body = body[2:]
	} else if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return front, body, nil
}

func parseFrontmatterFields(front string) map[string]string {
	fields := make(map[string]string)
	lines := strings.Split(front, "\n")
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		// Nested YAML (metadata, etc.) is ignored.
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t' || line[0] == '-') {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fields[key] = unquoteYAMLScalar(strings.TrimSpace(value))
	}
	return fields
}

func unquoteYAMLScalar(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		inner := value[1 : len(value)-1]
		if value[0] == '"' {
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			inner = strings.ReplaceAll(inner, `\\`, `\`)
		}
		return inner
	}
	return value
}

func validateName(name string) error {
	if name == "" {
		return errors.New("skill name is required")
	}
	if len(name) > maxNameLen {
		return errors.New("skill name exceeds 64 characters")
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return errors.New("skill name cannot start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		return errors.New("skill name cannot contain consecutive hyphens")
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return errors.New("skill name must be lowercase letters, digits, and hyphens")
	}
	return nil
}

func validateDescription(description string) error {
	if description == "" {
		return errors.New("skill description is required")
	}
	if len(description) > maxDescriptionLen {
		return errors.New("skill description exceeds 1024 characters")
	}
	for _, r := range description {
		if r == 0 || !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return errors.New("skill description contains invalid characters")
		}
	}
	return nil
}
