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

const barWidth = 32

func sectionRule(color bool) string {
	return paint(color, dim, strings.Repeat("─", barWidth))
}

func formatSectionTitle(color bool, title string) string {
	return paint(color, bold, title) + "\n" + sectionRule(color)
}

func formatBar(color bool, filled, width int, tone string) string {
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return paint(color, tone, strings.Repeat("█", filled)) +
		paint(color, dim, strings.Repeat("░", width-filled))
}

func formatBarPercent(color bool, percent float64) string {
	filled := int(percent / 100 * float64(barWidth))
	if percent > 0 && filled < 1 {
		filled = 1
	}
	return formatBar(color, filled, barWidth, quotaColor(percent))
}
