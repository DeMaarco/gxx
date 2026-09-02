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

package ui_test

import (
	"strings"
	"testing"

	"gxx/internal/ui"

	"gxx/internal/agent"
)

func TestFormatContextShowsColoredBreakdown(t *testing.T) {
	text := ui.FormatContext(agent.ContextUsage{
		WindowTokens:       272_000,
		UsedTokens:         40_000,
		Percent:            15,
		InstructionsTokens: 2_000,
		UserTokens:         10_000,
		AssistantTokens:    20_000,
		ReasoningTokens:    5_000,
		ToolTokens:         3_000,
	}, false)
	for _, expected := range []string{
		"Context window",
		"15% used",
		"40,000 / 272,000 tokens",
		"instructions",
		"user",
		"assistant",
		"reasoning",
		"tools",
		"free",
		"232,000",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("context = %q, want %q", text, expected)
		}
	}
}

func TestFormatStatusShowsContextPercent(t *testing.T) {
	got := ui.FormatStatus(ui.REPLSettings{
		Model:          "gpt-5.6-sol",
		PermissionMode: "ask",
		Effort:         "medium",
		Context:        "272k",
		FetchContext: func() agent.ContextUsage {
			return agent.ContextUsage{Percent: 12}
		},
	})
	if !strings.Contains(got, "(12%)") {
		t.Fatalf("status = %q, want (12%%)", got)
	}
}

func TestPaintContextPercentUsesWarmColorsWhenFull(t *testing.T) {
	if ui.ContextPercentColor(12) != ui.ColorGreen {
		t.Fatalf("low percent color = %q, want green", ui.ContextPercentColor(12))
	}
	if ui.ContextPercentColor(75) != ui.ColorYellow {
		t.Fatalf("mid percent color = %q, want yellow", ui.ContextPercentColor(75))
	}
	if ui.ContextPercentColor(95) != ui.ColorBold+ui.ColorRed {
		t.Fatalf("high percent color = %q, want red", ui.ContextPercentColor(95))
	}
}
