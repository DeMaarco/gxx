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
	"io"
	"strings"

	"gxx/internal/agent"
)

const contextBarWidth = 20

type contextSlice struct {
	name   string
	tokens int64
	code   string
}

func (s REPLSettings) contextUsage() agent.ContextUsage {
	if s.FetchContext != nil {
		return s.FetchContext()
	}
	return agent.ContextUsage{}
}

func paintContextPercent(color bool, percent int) string {
	return paint(color, contextPercentColor(percent), fmt.Sprintf("%d%%", percent))
}

func contextPercentColor(percent int) string {
	switch {
	case percent >= 90:
		return bold + red
	case percent >= 70:
		return yellow
	default:
		return green
	}
}

func contextSlices(usage agent.ContextUsage) []contextSlice {
	free := usage.WindowTokens - usage.UsedTokens
	if free < 0 {
		free = 0
	}
	return []contextSlice{
		{name: "instructions", tokens: usage.InstructionsTokens, code: cyan},
		{name: "user", tokens: usage.UserTokens, code: green},
		{name: "assistant", tokens: usage.AssistantTokens, code: magenta},
		{name: "reasoning", tokens: usage.ReasoningTokens, code: yellow},
		{name: "tools", tokens: usage.ToolTokens, code: blue},
		{name: "free", tokens: free, code: dim},
	}
}

// FormatContext renders a colored occupancy breakdown of the context window.
func FormatContext(usage agent.ContextUsage, color bool) string {
	var output strings.Builder
	window := usage.WindowTokens
	if window <= 0 {
		window = usage.UsedTokens
	}
	fmt.Fprintf(
		&output,
		"%s  %s / %s\n",
		paintContextPercent(color, usage.Percent),
		paint(color, dim, formatCount(usage.UsedTokens)),
		paint(color, dim, formatCount(window)),
	)
	output.WriteString(stackedContextBar(color, usage) + "\n")
	denom := window
	if usage.UsedTokens > denom {
		denom = usage.UsedTokens
	}
	for _, slice := range contextSlices(usage) {
		percent := 0
		if denom > 0 {
			percent = int((slice.tokens * 100) / denom)
		}
		fmt.Fprintf(
			&output,
			"%s  %s  %s\n",
			paint(color, slice.code, fmt.Sprintf("%-13s", slice.name)),
			paint(color, dim, fmt.Sprintf("%8s", formatCount(slice.tokens))),
			paint(color, slice.code, fmt.Sprintf("%3d%%", percent)),
		)
	}
	return output.String()
}

func stackedContextBar(color bool, usage agent.ContextUsage) string {
	slices := contextSlices(usage)
	tokens := make([]int64, len(slices))
	total := int64(0)
	for i, slice := range slices {
		tokens[i] = slice.tokens
		total += slice.tokens
	}
	if total <= 0 {
		return paint(color, dim, strings.Repeat("░", contextBarWidth))
	}
	cells := distributeCells(tokens, contextBarWidth)
	var bar strings.Builder
	for i, count := range cells {
		block := "█"
		if slices[i].name == "free" {
			block = "░"
		}
		bar.WriteString(paint(color, slices[i].code, strings.Repeat(block, count)))
	}
	return bar.String()
}

func distributeCells(parts []int64, width int) []int {
	cells := make([]int, len(parts))
	if width <= 0 {
		return cells
	}
	var total int64
	for _, part := range parts {
		if part > 0 {
			total += part
		}
	}
	if total <= 0 {
		return cells
	}
	assigned := 0
	type remainder struct {
		index int
		frac  int64
	}
	leftovers := make([]remainder, 0, len(parts))
	for i, part := range parts {
		if part <= 0 {
			continue
		}
		exact := part * int64(width)
		cells[i] = int(exact / total)
		assigned += cells[i]
		leftovers = append(leftovers, remainder{index: i, frac: exact % total})
	}
	for assigned < width && len(leftovers) > 0 {
		best := 0
		for i := 1; i < len(leftovers); i++ {
			if leftovers[i].frac > leftovers[best].frac {
				best = i
			}
		}
		cells[leftovers[best].index]++
		assigned++
		leftovers[best].frac = 0
	}
	return cells
}

func printContext(writer io.Writer, color bool, usage agent.ContextUsage) {
	text := strings.TrimRight(FormatContext(usage, color), "\n")
	_, _ = fmt.Fprintln(writer, text)
}
