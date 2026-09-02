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

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gxx/internal/agent"
)

const (
	usageFetchTimeout = 20 * time.Second
	maxUsageBodyBytes = 1 << 20
)

type oauthUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
	Percent     *float64 `json:"percent"`
}

type oauthUsageLimit struct {
	Kind     string          `json:"kind"`
	Percent  *float64        `json:"percent"`
	ResetsAt string          `json:"resets_at"`
	Scope    oauthUsageScope `json:"scope"`
}

type oauthUsageScope struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
}

type oauthUsagePayload struct {
	FiveHour       *oauthUsageWindow `json:"five_hour"`
	SevenDay       *oauthUsageWindow `json:"seven_day"`
	SevenDayOpus   *oauthUsageWindow `json:"seven_day_opus"`
	SevenDaySonnet *oauthUsageWindow `json:"seven_day_sonnet"`
	SevenDayFable  *oauthUsageWindow `json:"seven_day_fable"`
	Limits         []oauthUsageLimit `json:"limits"`
	ExtraUsage     *oauthExtraUsage  `json:"extra_usage"`
}

type oauthExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit float64  `json:"monthly_limit"`
	UsedCredits  float64  `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

func fetchOAuthUsage(ctx context.Context, client *http.Client, baseURL, token string) agent.AccountUsage {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthUsageURL(baseURL), nil)
	if err != nil {
		return agent.AccountUsage{Error: err.Error()}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", oauthBetaHeader)
	request.Header.Set("x-app", oauthAppHeader)
	request.Header.Set("User-Agent", "gxx")

	response, err := client.Do(request)
	if err != nil {
		return agent.AccountUsage{Error: err.Error()}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxUsageBodyBytes))
	if err != nil {
		return agent.AccountUsage{Error: err.Error()}
	}
	if response.StatusCode >= 400 {
		return agent.AccountUsage{Error: oauthUsageError(response.StatusCode, body)}
	}
	return parseOAuthUsage(body)
}

func oauthUsageURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" || trimmed == defaultAPIBaseURL {
		return defaultAPIBaseURL + "/api/oauth/usage"
	}
	return trimmed + "/api/oauth/usage"
}

func parseOAuthUsage(body []byte) agent.AccountUsage {
	var payload oauthUsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return agent.AccountUsage{Error: "decode Claude usage: " + err.Error()}
	}
	account := agent.AccountUsage{}
	account.Windows = appendWindow(account.Windows, "5h", payload.FiveHour)
	account.Windows = appendWindow(account.Windows, "weekly", payload.SevenDay)
	if !hasWindow(account.Windows, "5h") && hasWindow(account.Windows, "weekly") {
		account.Windows = append([]agent.QuotaWindow{{Name: "5h", UsedPercent: 0}}, account.Windows...)
	}
	account.Windows = appendWindow(account.Windows, "opus week", payload.SevenDayOpus)
	account.Windows = appendWindow(account.Windows, "sonnet week", payload.SevenDaySonnet)
	account.Windows = appendWindow(account.Windows, "fable week", payload.SevenDayFable)
	for _, limit := range payload.Limits {
		name := windowNameFromLimit(limit)
		if name == "" || hasWindow(account.Windows, name) {
			continue
		}
		account.Windows = appendWindow(account.Windows, name, &oauthUsageWindow{
			Utilization: limit.Percent,
			Percent:     limit.Percent,
			ResetsAt:    limit.ResetsAt,
		})
	}
	if window := extraUsageWindow(payload.ExtraUsage); window.Name != "" {
		account.Windows = append(account.Windows, window)
	}
	if len(account.Windows) == 0 {
		account.Error = "Claude usage omitted subscription windows"
	}
	return account
}

func appendWindow(windows []agent.QuotaWindow, name string, raw *oauthUsageWindow) []agent.QuotaWindow {
	if raw == nil {
		return windows
	}
	used, ok := windowUsedPercent(*raw)
	if !ok {
		return windows
	}
	window := agent.QuotaWindow{Name: name, UsedPercent: used}
	if reset := parseResetAt(raw.ResetsAt); !reset.IsZero() {
		window.ResetAt = reset
	}
	return append(windows, window)
}

func windowUsedPercent(raw oauthUsageWindow) (float64, bool) {
	if raw.Utilization != nil {
		return *raw.Utilization, true
	}
	if raw.Percent != nil {
		return *raw.Percent, true
	}
	return 0, false
}

func extraUsageWindow(extra *oauthExtraUsage) agent.QuotaWindow {
	if extra == nil || !extra.IsEnabled {
		return agent.QuotaWindow{}
	}
	used := 0.0
	switch {
	case extra.Utilization != nil:
		used = *extra.Utilization
	case extra.MonthlyLimit > 0:
		used = extra.UsedCredits / extra.MonthlyLimit * 100
	}
	return agent.QuotaWindow{Name: "extra", UsedPercent: used}
}

func windowNameFromLimit(limit oauthUsageLimit) string {
	switch strings.TrimSpace(limit.Kind) {
	case "session":
		return "5h"
	case "weekly_all":
		return "weekly"
	case "weekly_scoped":
		if name := strings.ToLower(strings.TrimSpace(limit.Scope.Model.DisplayName)); name != "" {
			return name
		}
		return "scoped"
	default:
		return ""
	}
}

func hasWindow(windows []agent.QuotaWindow, name string) bool {
	for _, window := range windows {
		if window.Name == name {
			return true
		}
	}
	return false
}

func parseResetAt(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func oauthUsageError(status int, body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return fmt.Sprintf("Claude usage: HTTP %d", status)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if message := strings.TrimSpace(payload.Error.Message); message != "" {
			return message
		}
		if message := strings.TrimSpace(payload.Message); message != "" {
			return message
		}
	}
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 180 {
		text = text[:180] + "…"
	}
	return text
}

func parseRateLimit(header http.Header) agent.RateLimit {
	limit := agent.RateLimit{
		RequestsLimit:     headerInt(header, "anthropic-ratelimit-requests-limit"),
		RequestsRemaining: headerInt(header, "anthropic-ratelimit-requests-remaining"),
		RequestsReset:     strings.TrimSpace(header.Get("anthropic-ratelimit-requests-reset")),
		TokensLimit:       headerInt(header, "anthropic-ratelimit-tokens-limit"),
		TokensRemaining:   headerInt(header, "anthropic-ratelimit-tokens-remaining"),
		TokensReset:       strings.TrimSpace(header.Get("anthropic-ratelimit-tokens-reset")),
	}
	limit.Known = header.Get("anthropic-ratelimit-requests-remaining") != "" ||
		header.Get("anthropic-ratelimit-tokens-remaining") != ""
	return limit
}

func headerInt(header http.Header, key string) int64 {
	value := strings.TrimSpace(header.Get(key))
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}
