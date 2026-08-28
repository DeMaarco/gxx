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
	"strings"
)

func formatMarkdown(color bool, value string) string {
	emit, _, _ := renderMarkdown(color, value, true, true)
	return emit
}

func renderMarkdown(color bool, value string, lineStart, flush bool) (emit, hold string, nextStart bool) {
	var output strings.Builder
	i := 0
	start := lineStart
	for i < len(value) {
		if start {
			if strings.HasPrefix(value[i:], "- ") || strings.HasPrefix(value[i:], "* ") {
				output.WriteString(paint(color, dim, "• "))
				i += 2
				start = false
				continue
			}
			if !flush && (value[i:] == "-" || value[i:] == "*") {
				return output.String(), value[i:], true
			}
		}

		switch {
		case value[i] == '`':
			rest := value[i+1:]
			if j := strings.IndexByte(rest, '`'); j >= 0 {
				output.WriteString(paint(color, yellow, rest[:j]))
				i += j + 2
				start = false
				continue
			}
			if !flush {
				return output.String(), value[i:], start
			}
			output.WriteString(rest)
			return output.String(), "", false

		case strings.HasPrefix(value[i:], "**"):
			rest := value[i+2:]
			if j := strings.Index(rest, "**"); j >= 0 {
				output.WriteString(paint(color, bold+cyan, rest[:j]))
				i += j + 4
				start = false
				continue
			}
			if !flush {
				return output.String(), value[i:], start
			}
			output.WriteString(rest)
			return output.String(), "", false

		case !flush && i == len(value)-1 && (value[i] == '*' || value[i] == '`'):
			return output.String(), value[i:], start
		}

		output.WriteByte(value[i])
		start = value[i] == '\n'
		i++
	}
	return output.String(), "", start
}
