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
	"math"
	"strconv"
	"strings"
	"time"

	"gxx/internal/agent"
)

// FormatUsage renders session, subscription or account spend, and remaining API quota.
func FormatUsage(report agent.UsageReport, color bool) string {
	var output strings.Builder
	hasSubscription := strings.TrimSpace(report.Account.Plan) != "" || len(report.Account.Windows) > 0
	writeUsageHeader(&output, color, report.Source, report.Account.Plan)
	writeAccountUsage(&output, color, report.Source, report.Account)
	writeSessionUsage(&output, color, report)
	if report.RateLimit.Known || !hasSubscription {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}
		writeRateLimit(&output, color, report.RateLimit)
	}
	return output.String()
}

func writeUsageHeader(output *strings.Builder, color bool, source, plan string) {
	source = strings.TrimSpace(source)
	plan = formatPlanName(plan)
	if source == "" && plan == "" {
		return
	}
	switch {
	case source != "" && plan != "":
		fmt.Fprintf(output, "%s  %s\n", paint(color, dim, source), paint(color, bold, plan))
	case source != "":
		fmt.Fprintf(output, "%s\n", paint(color, dim, source))
	default:
		fmt.Fprintf(output, "%s\n", paint(color, bold, plan))
	}
	output.WriteByte('\n')
}

func writeAccountUsage(output *strings.Builder, color bool, source string, account agent.AccountUsage) {
	hasSubscription := strings.TrimSpace(account.Plan) != "" || len(account.Windows) > 0
	hasMonthly := account.HasSpend || account.HasLimit || account.HasRemaining ||
		account.Requests > 0 || account.InputTokens > 0 || account.OutputTokens > 0
	if account.Error != "" && !hasSubscription && !hasMonthly {
		title := "account this month"
		if !account.PeriodStart.IsZero() {
			title = "account " + account.PeriodStart.Format("January 2006")
		} else if isSubscriptionSource(source) {
			title = "subscription"
		}
		fmt.Fprintf(output, "%s\n%s\n", paint(color, dim, title), paint(color, red, "  "+account.Error))
		return
	}
	if hasSubscription {
		writeSubscription(output, color, account)
	}
	if hasMonthly || (!hasSubscription && !account.PeriodStart.IsZero()) {
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n\n") {
			output.WriteByte('\n')
		}
		writeMonthlyAccount(output, color, account)
	}
	if account.Error != "" {
		fmt.Fprintf(output, "%s\n", paint(color, red, "  "+account.Error))
	}
}

func writeSubscription(output *strings.Builder, color bool, account agent.AccountUsage) {
	if len(account.Windows) == 0 {
		return
	}
	for _, window := range account.Windows {
		left := remainingPercent(window.UsedPercent)
		tone := quotaColor(left)
		label := paint(color, dim, fmt.Sprintf("%-10s", window.Name))
		bar := quotaBar(color, left)
		percent := paint(color, tone, fmt.Sprintf("%4s", formatPercent(left)))
		line := label + " " + bar + "  " + percent
		if reset := formatReset(window); reset != "" {
			line += "  " + paint(color, dim, reset)
		} else if window.UsedPercent == 0 {
			line += "  " + paint(color, dim, "not started")
		}
		fmt.Fprintf(output, "%s\n", line)
	}
}

func writeSessionUsage(output *strings.Builder, color bool, report agent.UsageReport) {
	if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n\n") {
		output.WriteByte('\n')
	}
	idle := report.SessionRequests == 0 &&
		report.Session.InputTokens == 0 &&
		report.Session.CachedTokens == 0 &&
		report.Session.CacheWriteTokens == 0 &&
		report.Session.OutputTokens == 0 &&
		report.Session.ReasoningTokens == 0 &&
		report.Session.TotalTokens == 0
	if idle {
		fmt.Fprintf(output, "%s  %s\n", paint(color, dim, "session"), paint(color, dim, "idle"))
		return
	}
	rows := []usageRow{
		{label: "requests", value: formatCount(report.SessionRequests)},
		{label: "input", value: formatCount(report.Session.InputTokens)},
		{label: "cached", value: formatCount(report.Session.CachedTokens)},
		{label: "cache write", value: formatCount(report.Session.CacheWriteTokens)},
		{label: "output", value: formatCount(report.Session.OutputTokens)},
		{label: "reasoning", value: formatCount(report.Session.ReasoningTokens)},
		{label: "total", value: formatCount(report.Session.TotalTokens), tone: cyan},
	}
	if report.HasSessionCost {
		rows = append(rows, usageRow{label: "cost", value: formatCostUSD(report.SessionCostUSD), tone: cyan})
	}
	writeUsageSection(output, color, "session", rows)
}

func writeMonthlyAccount(output *strings.Builder, color bool, account agent.AccountUsage) {
	title := "account this month"
	if !account.PeriodStart.IsZero() {
		title = "account " + account.PeriodStart.Format("January 2006")
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
		left := 0.0
		if account.HasLimit && account.LimitUSD > 0 {
			left = remainingPercent(100 - account.RemainingUSD/account.LimitUSD*100)
		} else if account.RemainingUSD > 0 {
			left = 100
		}
		rows = append(rows, usageRow{
			label: "remaining",
			value: formatUSD(account.RemainingUSD),
			alert: account.RemainingUSD <= 0,
			tone:  quotaColor(left),
		})
	} else if account.HasSpend && !account.HasLimit {
		rows = append(rows, usageRow{label: "remaining", value: "no spend limit"})
	}
	writeUsageSection(output, color, title, rows)
}

func isSubscriptionSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "ChatGPT", "Claude":
		return true
	default:
		return false
	}
}

func remainingPercent(used float64) float64 {
	left := 100 - used
	if left < 0 {
		return 0
	}
	if left > 100 {
		return 100
	}
	return left
}

func formatPercent(value float64) string {
	rounded := math.Round(value*10) / 10
	if rounded == float64(int64(rounded)) {
		return fmt.Sprintf("%.0f%%", rounded)
	}
	return fmt.Sprintf("%.1f%%", rounded)
}

func formatReset(window agent.QuotaWindow) string {
	remaining := time.Duration(window.ResetAfterSec) * time.Second
	if remaining <= 0 && !window.ResetAt.IsZero() {
		remaining = time.Until(window.ResetAt)
	}
	if remaining <= 0 {
		return ""
	}
	return "reset " + formatResetDuration(remaining)
}

func formatPlanName(plan string) string {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return ""
	}
	key := strings.ToLower(strings.ReplaceAll(plan, "_", " "))
	key = strings.Join(strings.Fields(key), " ")
	names := map[string]string{
		"free":       "Free",
		"go":         "Go",
		"plus":       "Plus",
		"pro":        "Pro",
		"prolite":    "Pro lite",
		"pro lite":   "Pro lite",
		"team":       "Team",
		"business":   "Business",
		"enterprise": "Enterprise",
		"edu":        "Edu",
		"education":  "Edu",
		"edu plus":   "Edu Plus",
		"edu pro":    "Edu Pro",
	}
	if name, ok := names[key]; ok {
		return name
	}
	return strings.ToUpper(plan[:1]) + plan[1:]
}

func quotaColor(left float64) string {
	switch {
	case left <= 10:
		return bold + red
	case left <= 30:
		return yellow
	default:
		return green
	}
}

func quotaBar(color bool, left float64) string {
	filled := int(math.Round(left / 100 * float64(contextBarWidth)))
	if left > 0 && filled < 1 {
		filled = 1
	}
	if filled > contextBarWidth {
		filled = contextBarWidth
	}
	if filled < 0 {
		filled = 0
	}
	return paint(color, quotaColor(left), strings.Repeat("█", filled)) +
		paint(color, dim, strings.Repeat("░", contextBarWidth-filled))
}

func formatResetDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	days := int(value.Hours()) / 24
	hours := int(value.Hours()) % 24
	minutes := int(value.Minutes()) % 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		if minutes < 1 {
			return "1m"
		}
		return fmt.Sprintf("%dm", minutes)
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
	tone  string
}

func writeUsageSection(output *strings.Builder, color bool, title string, rows []usageRow) {
	fmt.Fprintf(output, "%s\n", paint(color, dim, title))
	for _, row := range rows {
		label := paint(color, dim, fmt.Sprintf("  %-12s", row.label))
		tone := row.tone
		if row.alert {
			tone = red
		}
		fmt.Fprintf(output, "%s %s\n", label, paint(color, tone, row.value))
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

func formatCostUSD(value float64) string {
	switch {
	case value < 0.01:
		return fmt.Sprintf("$%.4f", value)
	case value < 1:
		return fmt.Sprintf("$%.3f", value)
	default:
		return fmt.Sprintf("$%.2f", value)
	}
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
