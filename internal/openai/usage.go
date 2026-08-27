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
		Session:         p.session,
		SessionRequests: p.sessionRequests,
		RateLimit:       p.rateLimit,
	}
	apiKey := p.apiKey
	httpClient := p.httpClient
	baseURL := p.baseURL
	p.mu.Unlock()

	if apiKey == "" {
		report.Account.Error = "OpenAI API key is not configured; run /config"
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
	_, body, err := doUsageRequest(ctx, client, baseURL, path, apiKey, query)
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
	header, _, err := doUsageRequest(ctx, client, baseURL, path, apiKey, nil)
	return header, err
}

func doUsageRequest(ctx context.Context, client *http.Client, baseURL, path, apiKey string, query url.Values) (http.Header, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPIBaseURL
	}
	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/"))
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
