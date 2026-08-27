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
	"fmt"
	"strings"
)

const (
	diffContextLines = 2
	maxDiffLCSCells  = 1_000_000
)

func compactDiff(path, oldValue, newValue string) string {
	oldLines := splitDiffLines(oldValue)
	newLines := splitDiffLines(newValue)
	ops := diffOps(oldLines, newLines)
	hunks := diffHunks(ops, oldLines, newLines, diffContextLines)
	var output strings.Builder
	fmt.Fprintf(&output, "--- %s\n+++ %s", path, path)
	if len(hunks) == 0 {
		return output.String()
	}
	for _, hunk := range hunks {
		fmt.Fprintf(
			&output,
			"\n@@ -%s +%s @@\n%s",
			hunkHeader(hunk.oldStart, hunk.oldCount),
			hunkHeader(hunk.newStart, hunk.newCount),
			strings.TrimSuffix(hunk.text, "\n"),
		)
	}
	return output.String()
}

type diffHunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	text     string
}

func hunkHeader(start, count int) string {
	if count == 0 {
		if start <= 0 {
			return "0,0"
		}
		return fmt.Sprintf("%d,0", start)
	}
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func splitDiffLines(value string) []string {
	if value == "" {
		return nil
	}
	if strings.HasSuffix(value, "\n") {
		value = value[:len(value)-1]
	}
	return strings.Split(value, "\n")
}

type diffOp byte

const (
	opEqual diffOp = iota
	opDelete
	opInsert
)

func diffOps(oldLines, newLines []string) []diffOp {
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
	oldMid := oldLines[prefix : len(oldLines)-suffix]
	newMid := newLines[prefix : len(newLines)-suffix]

	var middle []diffOp
	if int64(len(oldMid))*int64(len(newMid)) <= maxDiffLCSCells {
		middle = lcsOps(oldMid, newMid)
	} else {
		for range oldMid {
			middle = append(middle, opDelete)
		}
		for range newMid {
			middle = append(middle, opInsert)
		}
	}

	ops := make([]diffOp, 0, prefix+len(middle)+suffix)
	for range prefix {
		ops = append(ops, opEqual)
	}
	ops = append(ops, middle...)
	for range suffix {
		ops = append(ops, opEqual)
	}
	return ops
}

func lcsOps(oldLines, newLines []string) []diffOp {
	n, m := len(oldLines), len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			switch {
			case oldLines[i-1] == newLines[j-1]:
				dp[i][j] = dp[i-1][j-1] + 1
			case dp[i-1][j] >= dp[i][j-1]:
				dp[i][j] = dp[i-1][j]
			default:
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	var reverse []diffOp
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && oldLines[i-1] == newLines[j-1]:
			reverse = append(reverse, opEqual)
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			reverse = append(reverse, opInsert)
			j--
		default:
			reverse = append(reverse, opDelete)
			i--
		}
	}
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}
	return reverse
}

func diffHunks(ops []diffOp, oldLines, newLines []string, context int) []diffHunk {
	type span struct{ start, end int }
	var changes []span
	for i := 0; i < len(ops); {
		if ops[i] == opEqual {
			i++
			continue
		}
		start := i
		for i < len(ops) && ops[i] != opEqual {
			i++
		}
		changes = append(changes, span{start: start, end: i})
	}
	if len(changes) == 0 {
		return nil
	}

	var regions []span
	for _, change := range changes {
		start := max(change.start-context, 0)
		end := min(change.end+context, len(ops))
		if n := len(regions); n > 0 && start <= regions[n-1].end {
			regions[n-1].end = end
			continue
		}
		regions = append(regions, span{start: start, end: end})
	}

	oldAt := make([]int, len(ops)+1)
	newAt := make([]int, len(ops)+1)
	oi, ni := 0, 0
	for i, op := range ops {
		oldAt[i] = oi
		newAt[i] = ni
		switch op {
		case opEqual, opDelete:
			oi++
		}
		switch op {
		case opEqual, opInsert:
			ni++
		}
	}
	oldAt[len(ops)] = oi
	newAt[len(ops)] = ni

	hunks := make([]diffHunk, 0, len(regions))
	for _, region := range regions {
		var text strings.Builder
		oldCount, newCount := 0, 0
		oi, ni := oldAt[region.start], newAt[region.start]
		for i := region.start; i < region.end; i++ {
			switch ops[i] {
			case opEqual:
				fmt.Fprintf(&text, " %s\n", oldLines[oi])
				oi++
				ni++
				oldCount++
				newCount++
			case opDelete:
				fmt.Fprintf(&text, "-%s\n", oldLines[oi])
				oi++
				oldCount++
			case opInsert:
				fmt.Fprintf(&text, "+%s\n", newLines[ni])
				ni++
				newCount++
			}
		}
		oldStart := oldAt[region.start] + 1
		newStart := newAt[region.start] + 1
		if oldCount == 0 {
			oldStart = oldAt[region.start]
		}
		if newCount == 0 {
			newStart = newAt[region.start]
		}
		hunks = append(hunks, diffHunk{
			oldStart: oldStart,
			oldCount: oldCount,
			newStart: newStart,
			newCount: newCount,
			text:     text.String(),
		})
	}
	return hunks
}
