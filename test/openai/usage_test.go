package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gxx/internal/openai"
)

func TestParseRateLimitReadsRemainingQuota(t *testing.T) {
	header := make(http.Header)
	header.Set("x-ratelimit-limit-requests", "5000")
	header.Set("x-ratelimit-remaining-requests", "4980")
	header.Set("x-ratelimit-reset-requests", "12s")
	header.Set("x-ratelimit-limit-tokens", "200000")
	header.Set("x-ratelimit-remaining-tokens", "180000")
	header.Set("x-ratelimit-reset-tokens", "28s")

	limit := openai.ParseRateLimit(header)
	if !limit.Known ||
		limit.RequestsLimit != 5000 ||
		limit.RequestsRemaining != 4980 ||
		limit.RequestsReset != "12s" ||
		limit.TokensLimit != 200000 ||
		limit.TokensRemaining != 180000 ||
		limit.TokensReset != "28s" {
		t.Fatalf("rate limit = %+v", limit)
	}
	if openai.ParseRateLimit(http.Header{}).Known {
		t.Fatal("empty headers should be unknown")
	}
}

func TestParseAPIErrorAcceptsStringOrObject(t *testing.T) {
	if got := openai.ParseAPIError([]byte(`{"error":"You have insufficient permissions for this operation."}`)); got != "You have insufficient permissions for this operation." {
		t.Fatalf("string error = %q", got)
	}
	if got := openai.ParseAPIError([]byte(`{"error":{"message":"forbidden","type":"invalid_request_error"}}`)); got != "forbidden" {
		t.Fatalf("object error = %q", got)
	}
}

func TestFetchAccountUsageSumsSpendAndRemainingQuota(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = append(seen, request.URL.Path)
		switch request.URL.Path {
		case "/organization/usage/completions":
			if request.URL.Query().Get("start_time") == "" {
				t.Errorf("missing start_time")
			}
			writeJSON(t, writer, map[string]any{
				"object":   "page",
				"has_more": false,
				"data": []any{
					map[string]any{
						"object":     "bucket",
						"start_time": 1,
						"end_time":   2,
						"results": []any{map[string]any{
							"object":             "organization.usage.completions.result",
							"input_tokens":       100,
							"output_tokens":      20,
							"num_model_requests": 3,
						}},
					},
					map[string]any{
						"object":     "bucket",
						"start_time": 2,
						"end_time":   3,
						"results": []any{map[string]any{
							"object":             "organization.usage.completions.result",
							"input_tokens":       50,
							"output_tokens":      10,
							"num_model_requests": 1,
						}},
					},
				},
			})
		case "/organization/costs":
			writeJSON(t, writer, map[string]any{
				"object":   "page",
				"has_more": false,
				"data": []any{
					map[string]any{
						"object":     "bucket",
						"start_time": 1,
						"end_time":   2,
						"results": []any{map[string]any{
							"object": "organization.costs.result",
							"amount": map[string]any{"value": 12.4, "currency": "usd"},
						}},
					},
				},
			})
		case "/organization/spend_limit":
			writeJSON(t, writer, map[string]any{
				"object":           "organization.spend_limit",
				"threshold_amount": 10000,
				"currency":         "USD",
				"interval":         "month",
				"enforcement":      map[string]any{"status": "enforcing"},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	account := openai.FetchAccountUsage(context.Background(), server.Client(), server.URL, "test-key", now)
	if account.Error != "" {
		t.Fatalf("account error = %q", account.Error)
	}
	if account.Requests != 4 || account.InputTokens != 150 || account.OutputTokens != 30 {
		t.Fatalf("tokens = %+v", account)
	}
	if !account.HasSpend || account.SpendUSD != 12.4 {
		t.Fatalf("spend = %+v", account)
	}
	if !account.HasLimit || account.LimitUSD != 100 {
		t.Fatalf("limit = %+v", account)
	}
	if !account.HasRemaining || account.RemainingUSD != 87.6 {
		t.Fatalf("remaining = %+v", account)
	}
	if account.PeriodStart != time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("period = %s", account.PeriodStart)
	}
	if len(seen) != 3 {
		t.Fatalf("paths = %#v", seen)
	}
}

func TestFetchAccountUsageExplainsMissingAdminAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":{"message":"forbidden","type":"invalid_request_error","code":"","param":""}}`))
	}))
	defer server.Close()

	account := openai.FetchAccountUsage(context.Background(), server.Client(), server.URL, "test-key", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(account.Error, "OPENAI_ADMIN_KEY") {
		t.Fatalf("error = %q, want admin key guidance", account.Error)
	}
}

func TestFetchAccountUsageHandlesStringErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":"You have insufficient permissions for this operation."}`))
	}))
	defer server.Close()

	account := openai.FetchAccountUsage(context.Background(), server.Client(), server.URL, "test-key", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(account.Error, "OPENAI_ADMIN_KEY") {
		t.Fatalf("error = %q, want admin key guidance", account.Error)
	}
	if strings.Contains(account.Error, "cannot unmarshal") {
		t.Fatalf("error leaked SDK decode failure: %q", account.Error)
	}
}

func TestReportProbesRateLimitBeforeFirstModelRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/models" {
			writer.Header().Set("x-ratelimit-limit-requests", "5000")
			writer.Header().Set("x-ratelimit-remaining-requests", "4999")
			writer.Header().Set("x-ratelimit-reset-requests", "6s")
			writer.Header().Set("x-ratelimit-limit-tokens", "200000")
			writer.Header().Set("x-ratelimit-remaining-tokens", "199000")
			writeJSON(t, writer, map[string]any{"object": "list", "data": []any{}})
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":"You have insufficient permissions for this operation."}`))
	}))
	defer server.Close()

	provider := openai.New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL)

	report := provider.Report(context.Background())
	if !strings.Contains(report.Account.Error, "OPENAI_ADMIN_KEY") {
		t.Fatalf("account error = %q", report.Account.Error)
	}
	if strings.Contains(report.Account.Error, "cannot unmarshal") {
		t.Fatalf("account error leaked decode failure: %q", report.Account.Error)
	}
	if !report.RateLimit.Known ||
		report.RateLimit.RequestsRemaining != 4999 ||
		report.RateLimit.TokensRemaining != 199000 {
		t.Fatalf("rate limit = %+v", report.RateLimit)
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode JSON: %v", err)
	}
}
