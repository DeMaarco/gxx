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

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gxx/internal/agent"
)

const (
	usageFetchTimeout = 20 * time.Second
	defaultAPIBaseURL = "https://api.openai.com/v1"
	maxUsageBodyBytes = 1 << 20
)

type statusError struct {
	status  int
	message string
}

func (e statusError) Error() string {
	if strings.TrimSpace(e.message) == "" {
		return http.StatusText(e.status)
	}
	return e.message
}

type usagePage struct {
	Data []usageBucket `json:"data"`
}

type usageBucket struct {
	Results []usageResult `json:"results"`
}

type usageResult struct {
	InputTokens      int64       `json:"input_tokens"`
	OutputTokens     int64       `json:"output_tokens"`
	NumModelRequests int64       `json:"num_model_requests"`
	Amount           usageAmount `json:"amount"`
}

type usageAmount struct {
	Value float64 `json:"value"`
}

type spendLimitResponse struct {
	ThresholdAmount int64 `json:"threshold_amount"`
}

// Report returns session usage plus live organization spend and remaining quota.
func (p *Provider) Report(ctx context.Context) agent.UsageReport {
	p.mu.Lock()
	report := agent.UsageReport{
		Source:          "OpenAI API",
		Session:         p.session,
		SessionRequests: p.sessionRequests,
		RateLimit:       p.rateLimit,
	}
	apiKey := p.apiKey
	httpClient := p.httpClient
	baseURL := p.baseURL
	oauth := p.oauth
	p.mu.Unlock()

	if oauth {
		report.Source = "ChatGPT"
		token, accountID, err := p.oauthCreds(ctx)
		if err != nil {
			report.Account.Error = err.Error()
			return report
		}
		fetchContext, cancel := context.WithTimeout(ctx, usageFetchTimeout)
		defer cancel()
		report.Account = fetchChatGPTUsage(fetchContext, httpClient, baseURL, token, accountID)
		return report
	}
	if apiKey == "" {
		report.Account.Error = errOpenAIUnconfigured.Error()
		return report
	}

	fetchContext, cancel := context.WithTimeout(ctx, usageFetchTimeout)
	defer cancel()
	report.Account = fetchAccountUsage(fetchContext, httpClient, baseURL, usageAPIKey(apiKey), time.Now().UTC())
	if !report.RateLimit.Known {
		report.RateLimit = probeRateLimit(fetchContext, httpClient, baseURL, apiKey)
	}
	return report
}

func (p *Provider) oauthCreds(ctx context.Context) (token, accountID string, err error) {
	p.mu.Lock()
	source := p.tokens
	p.mu.Unlock()
	if source == nil {
		return "", "", errors.New("OpenAI is not logged in; run /login openai")
	}
	token, err = source.AccessToken(ctx)
	if err != nil {
		return "", "", err
	}
	accountID, err = source.AccountID(ctx)
	if err != nil {
		return "", "", err
	}
	return token, strings.TrimSpace(accountID), nil
}

func fetchChatGPTUsage(ctx context.Context, client *http.Client, baseURL, token, accountID string) agent.AccountUsage {
	headers := http.Header{}
	headers.Set("originator", codexOriginator)
	if accountID != "" {
		headers.Set(codexAccountHdr, accountID)
	}
	_, body, err := doUsageRequest(ctx, client, chatgptUsageURL(baseURL), "", token, nil, headers)
	if err != nil {
		return agent.AccountUsage{Error: err.Error()}
	}
	return parseChatGPTUsage(body)
}

func chatgptUsageURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" || trimmed == strings.TrimRight(codexAPIBaseURL, "/") {
		return "https://chatgpt.com/backend-api/wham/usage"
	}
	if strings.HasSuffix(trimmed, "/codex") {
		return strings.TrimSuffix(trimmed, "/codex") + "/wham/usage"
	}
	return trimmed + "/wham/usage"
}

func parseChatGPTUsage(body []byte) agent.AccountUsage {
	var payload struct {
		PlanType             string         `json:"plan_type"`
		RateLimit            map[string]any `json:"rate_limit"`
		AdditionalRateLimits []struct {
			LimitName string         `json:"limit_name"`
			RateLimit map[string]any `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return agent.AccountUsage{Error: "decode ChatGPT usage: " + err.Error()}
	}
	account := agent.AccountUsage{Plan: strings.TrimSpace(payload.PlanType)}
	account.Windows = appendChatGPTWindows(account.Windows, payload.RateLimit)
	for _, extra := range payload.AdditionalRateLimits {
		account.Windows = appendChatGPTWindows(account.Windows, extra.RateLimit)
	}
	if len(account.Windows) == 0 && account.Plan == "" {
		account.Error = "ChatGPT usage omitted subscription windows"
	}
	return account
}

func appendChatGPTWindows(windows []agent.QuotaWindow, rateLimit map[string]any) []agent.QuotaWindow {
	if len(rateLimit) == 0 {
		return windows
	}
	for _, key := range []string{"primary_window", "secondary_window"} {
		raw, _ := rateLimit[key].(map[string]any)
		window := quotaWindowFromChatGPT(raw)
		if window.Name == "" || hasQuotaWindow(windows, window.Name) {
			continue
		}
		windows = append(windows, window)
	}
	return windows
}

func hasQuotaWindow(windows []agent.QuotaWindow, name string) bool {
	for _, window := range windows {
		if window.Name == name {
			return true
		}
	}
	return false
}

func quotaWindowFromChatGPT(raw map[string]any) agent.QuotaWindow {
	if len(raw) == 0 {
		return agent.QuotaWindow{}
	}
	seconds := jsonFloat(raw["limit_window_seconds"])
	window := agent.QuotaWindow{
		Name:          windowNameFromSeconds(seconds),
		UsedPercent:   jsonFloat(raw["used_percent"]),
		ResetAfterSec: int64(jsonFloat(raw["reset_after_seconds"])),
	}
	if reset := jsonTime(raw["reset_at"]); !reset.IsZero() {
		window.ResetAt = reset
	}
	return window
}

func windowNameFromSeconds(seconds float64) string {
	switch {
	case seconds <= 0:
		return "window"
	case seconds <= 3*3600:
		return "1h"
	case seconds <= 8*3600:
		return "5h"
	case seconds <= 36*3600:
		return "daily"
	case seconds <= 10*24*3600:
		return "weekly"
	case seconds <= 45*24*3600:
		return "30d"
	default:
		days := int(seconds / (24 * 3600))
		if days > 0 {
			return fmt.Sprintf("%dd", days)
		}
		return "window"
	}
}

func jsonFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		return 0
	}
}

func jsonTime(value any) time.Time {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return time.Unix(int64(typed), 0).UTC()
		}
	case int64:
		if typed > 0 {
			return time.Unix(typed, 0).UTC()
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return time.Unix(parsed, 0).UTC()
		}
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed.UTC()
		}
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed.UTC()
		}
		if unix, err := strconv.ParseInt(raw, 10, 64); err == nil && unix > 0 {
			return time.Unix(unix, 0).UTC()
		}
	}
	return time.Time{}
}

func usageAPIKey(apiKey string) string {
	if admin := strings.TrimSpace(os.Getenv("OPENAI_ADMIN_KEY")); admin != "" {
		return admin
	}
	return apiKey
}

func fetchAccountUsage(ctx context.Context, client *http.Client, baseURL, apiKey string, now time.Time) agent.AccountUsage {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	account := agent.AccountUsage{PeriodStart: start}
	query := url.Values{
		"start_time":   {strconv.FormatInt(start.Unix(), 10)},
		"limit":        {"31"},
		"bucket_width": {"1d"},
	}

	var completions usagePage
	err := getJSON(ctx, client, baseURL, "/organization/usage/completions", apiKey, query, &completions)
	if err != nil {
		account.Error = accountFetchError(err)
		return account
	}
	account.Requests, account.InputTokens, account.OutputTokens = sumUsagePage(completions)

	var costs usagePage
	err = getJSON(ctx, client, baseURL, "/organization/costs", apiKey, query, &costs)
	if err == nil {
		account.SpendUSD = sumCostPage(costs)
		account.HasSpend = true
	} else if !unauthorized(err) {
		account.Error = err.Error()
	}

	var limit spendLimitResponse
	err = getJSON(ctx, client, baseURL, "/organization/spend_limit", apiKey, nil, &limit)
	switch {
	case err == nil:
		account.LimitUSD = float64(limit.ThresholdAmount) / 100
		account.HasLimit = true
	case notFound(err):
		// No hard spend cap is configured.
	case unauthorized(err):
		if account.Error == "" && !account.HasSpend {
			account.Error = accountFetchError(err)
		}
	default:
		if account.Error == "" {
			account.Error = err.Error()
		}
	}

	if account.HasSpend && account.HasLimit {
		remaining := account.LimitUSD - account.SpendUSD
		if remaining < 0 {
			remaining = 0
		}
		account.RemainingUSD = remaining
		account.HasRemaining = true
	}
	return account
}

func probeRateLimit(ctx context.Context, client *http.Client, baseURL, apiKey string) agent.RateLimit {
	header, err := getHeaders(ctx, client, baseURL, "/models", apiKey)
	if err != nil {
		return agent.RateLimit{}
	}
	return parseRateLimit(header)
}

func sumUsagePage(page usagePage) (requests, input, output int64) {
	for _, bucket := range page.Data {
		for _, result := range bucket.Results {
			requests += result.NumModelRequests
			input += result.InputTokens
			output += result.OutputTokens
		}
	}
	return requests, input, output
}

func sumCostPage(page usagePage) float64 {
	var total float64
	for _, bucket := range page.Data {
		for _, result := range bucket.Results {
			total += result.Amount.Value
		}
	}
	return total
}

func getJSON(ctx context.Context, client *http.Client, baseURL, path, apiKey string, query url.Values, dest any) error {
	_, body, err := doUsageRequest(ctx, client, baseURL, path, apiKey, query, nil)
	if err != nil {
		return err
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func getHeaders(ctx context.Context, client *http.Client, baseURL, path, apiKey string) (http.Header, error) {
	header, _, err := doUsageRequest(ctx, client, baseURL, path, apiKey, nil, nil)
	return header, err
}

func doUsageRequest(ctx context.Context, client *http.Client, baseURL, path, apiKey string, query url.Values, extra http.Header) (http.Header, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPIBaseURL
	}
	target := strings.TrimRight(baseURL, "/")
	if strings.TrimSpace(path) != "" {
		target += "/" + strings.TrimLeft(path, "/")
	}
	endpoint, err := url.Parse(target)
	if err != nil {
		return nil, nil, err
	}
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	for key, values := range extra {
		for _, value := range values {
			request.Header.Set(key, value)
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxUsageBodyBytes))
	if err != nil {
		return response.Header, nil, err
	}
	if response.StatusCode >= 400 {
		return response.Header, body, statusError{
			status:  response.StatusCode,
			message: parseAPIError(body),
		}
	}
	return response.Header, body, nil
}

func parseAPIError(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "request failed"
	}
	var payload struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return compactErrorText(string(body))
	}
	if message := errorMessage(payload.Error); message != "" {
		return message
	}
	if strings.TrimSpace(payload.Message) != "" {
		return payload.Message
	}
	if message := errorMessage(payload.Detail); message != "" {
		return message
	}
	if detail := strings.Trim(strings.TrimSpace(string(payload.Detail)), `"`); detail != "" {
		return compactErrorText(detail)
	}
	return compactErrorText(string(body))
}

func errorMessage(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		var message string
		if json.Unmarshal(raw, &message) == nil {
			return strings.TrimSpace(message)
		}
	case '{':
		var object struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &object) == nil {
			return strings.TrimSpace(object.Message)
		}
	}
	return ""
}

func compactErrorText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 180 {
		return value[:180] + "…"
	}
	return value
}

func parseRateLimit(header http.Header) agent.RateLimit {
	limit := agent.RateLimit{
		RequestsLimit:     headerInt(header, "x-ratelimit-limit-requests"),
		RequestsRemaining: headerInt(header, "x-ratelimit-remaining-requests"),
		RequestsReset:     strings.TrimSpace(header.Get("x-ratelimit-reset-requests")),
		TokensLimit:       headerInt(header, "x-ratelimit-limit-tokens"),
		TokensRemaining:   headerInt(header, "x-ratelimit-remaining-tokens"),
		TokensReset:       strings.TrimSpace(header.Get("x-ratelimit-reset-tokens")),
	}
	limit.Known = header.Get("x-ratelimit-remaining-requests") != "" ||
		header.Get("x-ratelimit-remaining-tokens") != ""
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

func accountFetchError(err error) string {
	if unauthorized(err) {
		return "organization usage requires an admin API key (OPENAI_ADMIN_KEY)"
	}
	return err.Error()
}

func unauthorized(err error) bool {
	status := apiStatus(err)
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func notFound(err error) bool {
	return apiStatus(err) == http.StatusNotFound
}

func apiStatus(err error) int {
	var status statusError
	if errors.As(err, &status) {
		return status.status
	}
	return 0
}
