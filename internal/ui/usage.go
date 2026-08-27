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
	"strconv"
	"strings"

	"gxx/internal/agent"
)

// FormatUsage renders session, account, and remaining API quota.
func FormatUsage(report agent.UsageReport, color bool) string {
	var output strings.Builder
	writeUsageSection(&output, color, "session", []usageRow{
		{label: "requests", value: formatCount(report.SessionRequests)},
		{label: "input", value: formatCount(report.Session.InputTokens)},
		{label: "cached", value: formatCount(report.Session.CachedTokens)},
		{label: "cache write", value: formatCount(report.Session.CacheWriteTokens)},
		{label: "output", value: formatCount(report.Session.OutputTokens)},
		{label: "reasoning", value: formatCount(report.Session.ReasoningTokens)},
		{label: "total", value: formatCount(report.Session.TotalTokens)},
	})
	output.WriteByte('\n')
	writeAccountUsage(&output, color, report.Account)
	output.WriteByte('\n')
	writeRateLimit(&output, color, report.RateLimit)
	return output.String()
}

func writeAccountUsage(output *strings.Builder, color bool, account agent.AccountUsage) {
	title := "account this month"
	if !account.PeriodStart.IsZero() {
		title = "account " + account.PeriodStart.Format("January 2006")
	}
	if account.Error != "" && !account.HasSpend && account.Requests == 0 {
		fmt.Fprintf(output, "%s\n%s\n", paint(color, dim, title), paint(color, dim, "  "+account.Error))
		return
	}

	rows := []usageRow{
		{label: "requests", value: formatCount(account.Requests)},
		{label: "input", value: formatCount(account.InputTokens)},
		{label: "output", value: formatCount(account.OutputTokens)},
	}
	if account.HasSpend {
		rows = append(rows, usageRow{label: "spend", value: formatUSD(account.SpendUSD)})
	}
	if account.HasLimit {
		rows = append(rows, usageRow{label: "limit", value: formatUSD(account.LimitUSD)})
	}
	if account.HasRemaining {
		rows = append(rows, usageRow{
			label: "remaining",
			value: formatUSD(account.RemainingUSD),
			alert: account.RemainingUSD <= 0,
		})
	} else if account.HasSpend && !account.HasLimit {
		rows = append(rows, usageRow{label: "remaining", value: "no spend limit"})
	}
	writeUsageSection(output, color, title, rows)
	if account.Error != "" {
		fmt.Fprintf(output, "%s\n", paint(color, dim, "  "+account.Error))
	}
}

func writeRateLimit(output *strings.Builder, color bool, limit agent.RateLimit) {
	if !limit.Known {
		fmt.Fprintf(
			output,
			"%s\n%s\n",
			paint(color, dim, "rate limit"),
			paint(color, dim, "  unknown until a model request is made"),
		)
		return
	}
	writeUsageSection(output, color, "rate limit", []usageRow{
		{label: "requests", value: formatQuota(limit.RequestsRemaining, limit.RequestsLimit, limit.RequestsReset)},
		{label: "tokens", value: formatQuota(limit.TokensRemaining, limit.TokensLimit, limit.TokensReset)},
	})
}

type usageRow struct {
	label string
	value string
	alert bool
}

func writeUsageSection(output *strings.Builder, color bool, title string, rows []usageRow) {
	fmt.Fprintf(output, "%s\n", paint(color, dim, title))
	for _, row := range rows {
		line := fmt.Sprintf("  %-12s %s", row.label, row.value)
		if row.alert {
			fmt.Fprintf(output, "%s\n", paint(color, red, line))
			continue
		}
		fmt.Fprintf(output, "%s\n", paint(color, dim, line))
	}
}

func formatQuota(remaining, limit int64, reset string) string {
	value := formatCount(remaining) + " / " + formatCount(limit)
	reset = strings.TrimSpace(reset)
	if reset != "" {
		value += "  reset " + reset
	}
	return value
}

func formatUSD(value float64) string {
	return fmt.Sprintf("$%.2f", value)
}

func formatCount(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	digits := strconv.FormatInt(n, 10)
	var grouped strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	return sign + grouped.String()
}

func formatCompactTokens(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	if n < 1000 {
		return sign + strconv.FormatInt(n, 10)
	}
	unit := "k"
	value := float64(n) / 1000
	if n >= 1_000_000 {
		unit = "M"
		value = float64(n) / 1_000_000
	}
	if value == float64(int64(value)) {
		return sign + strconv.FormatInt(int64(value), 10) + unit
	}
	return sign + fmt.Sprintf("%.1f%s", value, unit)
}

func printUsage(writer io.Writer, color bool, report agent.UsageReport) {
	text := strings.TrimRight(FormatUsage(report, color), "\n")
	_, _ = fmt.Fprintln(writer, text)
}
