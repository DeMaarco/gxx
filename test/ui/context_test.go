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
		"15%",
		"40,000 / 272,000",
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
	if !strings.Contains(got, "12%") {
		t.Fatalf("status = %q, want 12%%", got)
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
