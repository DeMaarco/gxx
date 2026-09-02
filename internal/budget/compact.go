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

package budget

import (
	"strings"
	"unicode/utf8"
)

const (
	CompactNotice          = "Earlier conversation was compacted by gxx to fit the context window. Continue from the remaining history."
	SummaryMaxBytes        = 4 * 1024
	SummaryClipRunes       = 160
	EmergencyToolClipBytes = 512
)

// LastStrings returns the last n strings (or all if shorter).
func LastStrings(values []string, n int) []string {
	if n <= 0 || len(values) <= n {
		return values
	}
	return values[len(values)-n:]
}

// ClipRunes truncates value to at most limit runes and appends an ellipsis.
func ClipRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}

// ClipBytes truncates value to at most limit bytes on a rune boundary.
func ClipBytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

// FormatDroppedSummary builds the heuristic compact notice body.
func FormatDroppedSummary(users, tools, errors []string) string {
	users = LastStrings(users, 3)
	errors = LastStrings(errors, 5)

	var builder strings.Builder
	builder.WriteString(CompactNotice)
	if len(users) > 0 {
		builder.WriteString("\nPrior user requests:")
		for _, user := range users {
			builder.WriteString("\n- ")
			builder.WriteString(user)
		}
	}
	if len(tools) > 0 {
		builder.WriteString("\nTools used: ")
		builder.WriteString(strings.Join(tools, ", "))
	}
	if len(errors) > 0 {
		builder.WriteString("\nRecent tool errors:")
		for _, message := range errors {
			builder.WriteString("\n- ")
			builder.WriteString(message)
		}
	}
	return ClipBytes(builder.String(), SummaryMaxBytes)
}
