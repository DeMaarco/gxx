package ui

import (
	"strings"
	"testing"

	"gxx/internal/agent"
)

func TestFormatContextShowsColoredBreakdown(t *testing.T) {
	text := FormatContext(agent.ContextUsage{
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
	got := formatStatus(REPLSettings{
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
	if contextPercentColor(12) != green {
		t.Fatalf("low percent color = %q, want green", contextPercentColor(12))
	}
	if contextPercentColor(75) != yellow {
		t.Fatalf("mid percent color = %q, want yellow", contextPercentColor(75))
	}
	if contextPercentColor(95) != bold+red {
		t.Fatalf("high percent color = %q, want red", contextPercentColor(95))
	}
}
