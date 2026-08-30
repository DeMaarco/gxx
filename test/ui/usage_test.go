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
	"time"

	"gxx/internal/ui"

	"gxx/internal/agent"
)

func TestFormatUsageShowsSessionAccountAndRemainingQuota(t *testing.T) {
	text := ui.FormatUsage(agent.UsageReport{
		Session: agent.Usage{
			InputTokens:      12340,
			OutputTokens:     4210,
			ReasoningTokens:  8100,
			CachedTokens:     4000,
			CacheWriteTokens: 800,
			TotalTokens:      24650,
		},
		SessionRequests: 3,
		SessionCostUSD:  1.23,
		HasSessionCost:  true,
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
		"cost         $1.23",
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

func TestFormatCompactTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{n: 0, want: "0"},
		{n: 999, want: "999"},
		{n: 1000, want: "1k"},
		{n: 1240, want: "1.2k"},
		{n: 12400, want: "12.4k"},
		{n: 1_000_000, want: "1M"},
		{n: 1_500_000, want: "1.5M"},
		{n: -12000, want: "-12k"},
	}
	for _, test := range tests {
		if got := ui.FormatCompactTokens(test.n); got != test.want {
			t.Fatalf("formatCompactTokens(%d) = %q, want %q", test.n, got, test.want)
		}
	}
}

func TestFormatTurnUsageOmitsZeroTotals(t *testing.T) {
	if got := ui.FormatTurnUsage(false, agent.Usage{}); got != "" {
		t.Fatalf("empty usage = %q, want empty", got)
	}
	got := ui.FormatTurnUsage(false, agent.Usage{InputTokens: 8100, OutputTokens: 4300, TotalTokens: 12400})
	if got != "12.4k tok · 8.1k in · 4.3k out" {
		t.Fatalf("turn usage = %q", got)
	}
}

func TestFormatCostUSD(t *testing.T) {
	tests := []struct {
		n    float64
		want string
	}{
		{n: 0.0042, want: "$0.0042"},
		{n: 0.042, want: "$0.042"},
		{n: 1.2, want: "$1.20"},
	}
	for _, test := range tests {
		if got := ui.FormatCostUSD(test.n); got != test.want {
			t.Fatalf("formatCostUSD(%v) = %q, want %q", test.n, got, test.want)
		}
	}
}

func TestFormatUsageShowsSubscriptionRemaining(t *testing.T) {
	text := ui.FormatUsage(agent.UsageReport{
		Source: "ChatGPT",
		Account: agent.AccountUsage{
			Plan: "Plus",
			Windows: []agent.QuotaWindow{
				{Name: "5h", UsedPercent: 55, ResetAfterSec: 2*3600 + 12*60},
				{Name: "weekly", UsedPercent: 51, ResetAfterSec: 5*24*3600 + 16*3600},
				{Name: "opus week", UsedPercent: 92, ResetAfterSec: 3600},
			},
		},
	}, false)
	for _, expected := range []string{
		"ChatGPT  Plus",
		"5h         █████████░░░░░░░░░░░   45%  reset 2h 12m",
		"weekly     ██████████░░░░░░░░░░   49%  reset 5d 16h",
		"opus week  ██░░░░░░░░░░░░░░░░░░    8%  reset 1h",
		"session  idle",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("usage = %q, want %q", text, expected)
		}
	}
	if strings.Contains(text, "account this month") {
		t.Fatalf("subscription usage showed monthly spend: %q", text)
	}
	if strings.Contains(text, "unknown until a model request is made") {
		t.Fatalf("subscription usage showed API rate-limit hint: %q", text)
	}
}

func TestFormatUsageShowsClaudeWindows(t *testing.T) {
	text := ui.FormatUsage(agent.UsageReport{
		Source: "Claude",
		Account: agent.AccountUsage{
			Windows: []agent.QuotaWindow{
				{Name: "5h", UsedPercent: 0},
				{Name: "weekly", UsedPercent: 20, ResetAfterSec: 2 * 24 * 3600},
			},
		},
	}, false)
	for _, expected := range []string{
		"Claude",
		"5h         ████████████████████  100%  not started",
		"weekly     ████████████████░░░░   80%  reset 2d",
		"session  idle",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("usage = %q, want %q", text, expected)
		}
	}
}

func TestFormatUsageColorsQuotaAndPlan(t *testing.T) {
	text := ui.FormatUsage(agent.UsageReport{
		Source: "ChatGPT",
		Account: agent.AccountUsage{
			Plan: "go",
			Windows: []agent.QuotaWindow{
				{Name: "weekly", UsedPercent: 0, ResetAfterSec: 30 * 24 * 3600},
				{Name: "5h", UsedPercent: 92, ResetAfterSec: 3600},
			},
		},
	}, true)
	if !strings.Contains(text, "ChatGPT") || !strings.Contains(text, "Go") {
		t.Fatalf("usage = %q, want ChatGPT Go", text)
	}
	if !strings.Contains(text, ui.ColorGreen) {
		t.Fatalf("usage = %q, want green for healthy quota", text)
	}
	if !strings.Contains(text, ui.ColorRed) {
		t.Fatalf("usage = %q, want red for low quota", text)
	}
	if !strings.Contains(text, ui.ColorBold) {
		t.Fatalf("usage = %q, want bold plan", text)
	}
}

func TestFormatUsageExplainsMissingAccountAndRateLimit(t *testing.T) {
	text := ui.FormatUsage(agent.UsageReport{
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
