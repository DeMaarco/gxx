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
	"encoding/json"
	"fmt"
	"strings"

	"gxx/internal/agent"
)

const (
	maxLiveDetails = 5
)

type activityKind byte

const (
	activityContext activityKind = iota
	activityStats
)

type activityLine struct {
	kind    activityKind
	text    string
	added   int
	deleted int
	removed bool
}

func toolVerb(name string) string {
	switch strings.TrimSpace(name) {
	case "list_files":
		return "listing"
	case "search_files":
		return "searching"
	case "read_file":
		return "reading"
	case "apply_patch":
		return "writing"
	case "run_command":
		return "running"
	case "":
		return "working"
	default:
		return name
	}
}

func verbColor(name string) string {
	switch strings.TrimSpace(name) {
	case "apply_patch":
		return yellow
	case "run_command":
		return magenta
	default:
		return cyan
	}
}

func formatActivityLine(color bool, line activityLine) string {
	switch line.kind {
	case activityStats:
		return "  " + formatLineCounts(color, line.text, line.added, line.deleted, line.removed)
	default:
		return "  " + paint(color, dim, safeTerminalText(line.text))
	}
}

func formatLineCounts(color bool, path string, added, deleted int, removed bool) string {
	var parts []string
	if path != "" {
		parts = append(parts, paint(color, dim, safeTerminalText(path)))
	}
	if added > 0 {
		parts = append(parts, paint(color, green, fmt.Sprintf("+%d", added)))
	}
	if deleted > 0 {
		parts = append(parts, paint(color, red, fmt.Sprintf("-%d", deleted)))
	}
	if removed && added == 0 && deleted == 0 {
		parts = append(parts, paint(color, red, "deleted"))
	}
	return strings.Join(parts, "  ")
}

func patchActivity(raw json.RawMessage) (added, deleted int, removed bool, files []activityLine) {
	if len(raw) == 0 {
		return 0, 0, false, nil
	}
	var args struct {
		Changes []struct {
			Path    string  `json:"path"`
			Action  string  `json:"action"`
			Content *string `json:"content"`
			OldText *string `json:"old_text"`
			NewText *string `json:"new_text"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || len(args.Changes) == 0 {
		return 0, 0, false, nil
	}

	type fileStat struct {
		path    string
		added   int
		deleted int
		removed bool
	}
	order := make([]string, 0, len(args.Changes))
	byPath := map[string]*fileStat{}
	for _, change := range args.Changes {
		path := strings.TrimSpace(change.Path)
		if path == "" {
			continue
		}
		stat, ok := byPath[path]
		if !ok {
			stat = &fileStat{path: path}
			byPath[path] = stat
			order = append(order, path)
		}
		switch strings.TrimSpace(change.Action) {
		case "add":
			content := ""
			if change.Content != nil {
				content = *change.Content
			}
			stat.added += len(splitActivityLines(content))
		case "delete":
			if change.OldText != nil && *change.OldText != "" {
				stat.deleted += len(splitActivityLines(*change.OldText))
			} else {
				stat.removed = true
			}
		default:
			oldText, newText := "", ""
			if change.OldText != nil {
				oldText = *change.OldText
			}
			if change.NewText != nil {
				newText = *change.NewText
			}
			ins, del := snippetDiffCounts(oldText, newText)
			stat.added += ins
			stat.deleted += del
		}
	}

	files = make([]activityLine, 0, len(order))
	for _, path := range order {
		stat := byPath[path]
		added += stat.added
		deleted += stat.deleted
		removed = removed || stat.removed
		files = append(files, activityLine{
			kind:    activityStats,
			text:    stat.path,
			added:   stat.added,
			deleted: stat.deleted,
			removed: stat.removed,
		})
	}
	if len(files) <= 1 {
		files = nil
	}
	return added, deleted, removed, files
}

func snippetDiffCounts(oldText, newText string) (added, deleted int) {
	oldLines := splitActivityLines(oldText)
	newLines := splitActivityLines(newText)
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix &&
		suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	return len(newLines) - prefix - suffix, len(oldLines) - prefix - suffix
}

func resultDetailLines(result *agent.ToolResult) []activityLine {
	if result == nil || result.IsError {
		return nil
	}
	switch strings.TrimSpace(result.Name) {
	case "search_files":
		return searchDetailLines(result.Output)
	case "list_files":
		return listDetailLines(result.Output)
	default:
		return nil
	}
}

func searchDetailLines(output string) []activityLine {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	if strings.HasPrefix(output, "No matches") {
		return []activityLine{{kind: activityContext, text: "no matches"}}
	}
	count := 0
	for _, raw := range strings.Split(output, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "…") {
			continue
		}
		count++
	}
	if count == 0 {
		return nil
	}
	label := fmt.Sprintf("%d matches", count)
	if count == 1 {
		label = "1 match"
	}
	return []activityLine{{kind: activityContext, text: label}}
}

func listDetailLines(output string) []activityLine {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	if strings.HasPrefix(output, "No files") {
		return []activityLine{{kind: activityContext, text: "no files"}}
	}
	count := 0
	for _, raw := range strings.Split(output, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "…") {
			continue
		}
		count++
	}
	if count == 0 {
		return nil
	}
	label := fmt.Sprintf("%d files", count)
	if count == 1 {
		label = "1 file"
	}
	return []activityLine{{kind: activityContext, text: label}}
}

func splitActivityLines(value string) []string {
	if value == "" {
		return nil
	}
	if strings.HasSuffix(value, "\n") {
		value = value[:len(value)-1]
	}
	return strings.Split(value, "\n")
}

func doneExtraLines(result *agent.ToolResult, tool runningTool) []activityLine {
	if len(tool.lines) > 0 {
		return tool.lines
	}
	return resultDetailLines(result)
}
