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

package caveman

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	Lite  = 1
	Full  = 2
	Ultra = 3
)

var (
	fillers       = regexp.MustCompile(`(?i)\b(?:just|really|basically|actually|simply|quite|very|essentially|literally)\b`)
	pleasantries  = regexp.MustCompile(`(?i)\b(?:please|kindly|thank you|thanks|sure|certainly|of course|happy to|i'?d be happy)\b[,.]?\s*`)
	hedges        = regexp.MustCompile(`(?i)\b(?:perhaps|maybe|might|could potentially|would like to|i think|in my opinion|it seems|it appears)\b\s*`)
	leaders       = regexp.MustCompile(`(?im)^(?:i'?ll|i will|i can|i'?d|you can|we will|we can|let me|let'?s)\s+`)
	articles      = regexp.MustCompile(`(?i)\b(?:a|an|the) ([a-z])`)
	extraPhrases  = regexp.MustCompile(`(?i)\b(?:in order to|as well as|in addition to|due to the fact that|at this point in time|furthermore|moreover)\b\s*`)
	spaces        = regexp.MustCompile(`[ \t]{2,}`)
	spacePunct    = regexp.MustCompile(`\s+([,.;:!?])`)
	newlines      = regexp.MustCompile(`\n{3,}`)
	sentenceStart = regexp.MustCompile(`(^|[.!?]\s+)([a-z])`)
	sentinel      = regexp.MustCompile("\x1eCAV([0-9]+)\x1e")

	protected = []*regexp.Regexp{
		regexp.MustCompile("(?s)```.*?```"),
		regexp.MustCompile("`[^`\n]+`"),
		regexp.MustCompile(`(?i)\bhttps?://\S+`),
		regexp.MustCompile(`\b[\w.-]*[\\/][\w./\\-]+`),
		regexp.MustCompile(`\b[A-Z][A-Za-z0-9]*(?:_[A-Z][A-Za-z0-9]*)+\b`),
		regexp.MustCompile(`\b\w+\.\w+(?:\.\w+)*\(\)?`),
		regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\s*\([^)]*\)`),
		regexp.MustCompile(`\b\d+\.\d+\.\d+\b`),
	}
)

// Compress shortens prose the way Caveman does: drop filler, keep code,
// paths, URLs, and identifiers. level is 1 lite, 2 full, 3 ultra.
func Compress(text string, level int) string {
	if level <= 0 || text == "" {
		return text
	}
	if level > Ultra {
		level = Ultra
	}
	return withProtected(text, func(plain string) string {
		return compressProse(plain, level)
	})
}

func compressProse(text string, level int) string {
	s := text
	s = leaders.ReplaceAllString(s, "")
	s = pleasantries.ReplaceAllString(s, "")
	s = hedges.ReplaceAllString(s, "")
	s = fillers.ReplaceAllString(s, "")
	if level >= Full {
		s = articles.ReplaceAllString(s, "$1")
	}
	if level >= Ultra {
		s = extraPhrases.ReplaceAllString(s, "")
	}
	s = spaces.ReplaceAllString(s, " ")
	s = spacePunct.ReplaceAllString(s, "$1")
	s = newlines.ReplaceAllString(s, "\n\n")
	s = sentenceStart.ReplaceAllStringFunc(s, func(match string) string {
		parts := sentenceStart.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return parts[1] + strings.ToUpper(parts[2])
	})
	return strings.TrimSpace(s)
}

func withProtected(text string, transform func(string) string) string {
	segments := make([]string, 0, 8)
	working := text
	for _, pattern := range protected {
		working = pattern.ReplaceAllStringFunc(working, func(match string) string {
			i := len(segments)
			segments = append(segments, match)
			return fmt.Sprintf("\x1eCAV%d\x1e", i)
		})
	}
	out := transform(working)
	for pass := 0; pass < 8; pass++ {
		if !strings.Contains(out, "\x1eCAV") {
			break
		}
		out = sentinel.ReplaceAllStringFunc(out, func(match string) string {
			parts := sentinel.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			index, err := strconv.Atoi(parts[1])
			if err != nil || index < 0 || index >= len(segments) {
				return match
			}
			return segments[index]
		})
	}
	return out
}

// CompressDescriptions walks JSON-like maps and compresses "description" strings.
func CompressDescriptions(value any, level int) any {
	if level <= 0 {
		return value
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if key == "description" {
				if text, ok := item.(string); ok {
					out[key] = Compress(text, level)
					continue
				}
			}
			out[key] = CompressDescriptions(item, level)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = CompressDescriptions(item, level)
		}
		return out
	default:
		return value
	}
}
