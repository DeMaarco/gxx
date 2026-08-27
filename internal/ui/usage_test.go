package ui

import (
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
)

func TestFormatUsageShowsSessionAccountAndRemainingQuota(t *testing.T) {
	text := FormatUsage(agent.UsageReport{
		Session: agent.Usage{
			InputTokens:      12340,
			OutputTokens:     4210,
			ReasoningTokens:  8100,
			CachedTokens:     4000,
			CacheWriteTokens: 800,
			TotalTokens:      24650,
		},
		SessionRequests: 3,
		RateLimit: agent.RateLimit{
			Known:             true,
			RequestsLimit:     5000,
			RequestsRemaining: 4980,
			RequestsReset:     "12s",
			TokensLimit:       200000,
			TokensRemaining:   180000,
			TokensReset:       "28s",
		},
		Account: agent.AccountUsage{
			PeriodStart:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			Requests:     142,
			InputTokens:  1200000,
			OutputTokens: 340000,
			SpendUSD:     12.4,
			HasSpend:     true,
			LimitUSD:     100,
			HasLimit:     true,
			RemainingUSD: 87.6,
			HasRemaining: true,
		},
	}, false)

	for _, expected := range []string{
		"session",
		"requests     3",
		"input        12,340",
		"cached       4,000",
		"cache write  800",
		"output       4,210",
		"reasoning    8,100",
		"total        24,650",
		"account August 2026",
		"requests     142",
		"input        1,200,000",
		"spend        $12.40",
		"limit        $100.00",
		"remaining    $87.60",
		"rate limit",
		"4,980 / 5,000  reset 12s",
		"180,000 / 200,000  reset 28s",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("usage = %q, want %q", text, expected)
		}
	}
}

func TestFormatUsageExplainsMissingAccountAndRateLimit(t *testing.T) {
	text := FormatUsage(agent.UsageReport{
		Account: agent.AccountUsage{
			Error: "organization usage requires an admin API key (OPENAI_ADMIN_KEY)",
		},
	}, false)
	if !strings.Contains(text, "OPENAI_ADMIN_KEY") {
		t.Fatalf("usage = %q, want admin key guidance", text)
	}
	if !strings.Contains(text, "unknown until a model request is made") {
		t.Fatalf("usage = %q, want rate-limit guidance", text)
	}
}
