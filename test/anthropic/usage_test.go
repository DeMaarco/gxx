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

package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gxx/internal/anthropic"
)

func TestOAuthUsageURL(t *testing.T) {
	if got := anthropic.OAuthUsageURL(""); got != "https://api.anthropic.com/api/oauth/usage" {
		t.Fatalf("empty = %q", got)
	}
	if got := anthropic.OAuthUsageURL("https://api.anthropic.com"); got != "https://api.anthropic.com/api/oauth/usage" {
		t.Fatalf("default = %q", got)
	}
	if got := anthropic.OAuthUsageURL("http://127.0.0.1:9"); got != "http://127.0.0.1:9/api/oauth/usage" {
		t.Fatalf("custom = %q", got)
	}
}

func TestParseOAuthUsageReadsWindowsAndLimits(t *testing.T) {
	account := anthropic.ParseOAuthUsage([]byte(`{
		"five_hour": {"utilization": 55, "resets_at": "2026-08-29T18:00:00Z"},
		"seven_day": {"utilization": 51, "resets_at": "2026-09-04T12:00:00Z"},
		"seven_day_opus": null,
		"limits": [
			{"kind": "session", "percent": 55},
			{"kind": "weekly_scoped", "percent": 12, "scope": {"model": {"display_name": "Fable"}}}
		],
		"extra_usage": {"is_enabled": true, "monthly_limit": 100, "used_credits": 25}
	}`))
	if account.Error != "" || len(account.Windows) != 4 {
		t.Fatalf("account = %+v", account)
	}
	if account.Windows[0].Name != "5h" || account.Windows[0].UsedPercent != 55 {
		t.Fatalf("5h = %+v", account.Windows[0])
	}
	if account.Windows[1].Name != "weekly" || account.Windows[1].UsedPercent != 51 {
		t.Fatalf("weekly = %+v", account.Windows[1])
	}
	if account.Windows[2].Name != "fable" || account.Windows[2].UsedPercent != 12 {
		t.Fatalf("fable = %+v", account.Windows[2])
	}
	if account.Windows[3].Name != "extra" || account.Windows[3].UsedPercent != 25 {
		t.Fatalf("extra = %+v", account.Windows[3])
	}
	if account.Windows[0].ResetAt.UTC() != time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC) {
		t.Fatalf("reset = %s", account.Windows[0].ResetAt)
	}
}

func TestParseOAuthUsageShowsFiveHourWhenSessionNotStarted(t *testing.T) {
	account := anthropic.ParseOAuthUsage([]byte(`{
		"five_hour": null,
		"seven_day": {"utilization": 18, "resets_at": "2026-09-04T12:00:00Z"}
	}`))
	if len(account.Windows) != 2 {
		t.Fatalf("windows = %+v", account.Windows)
	}
	if account.Windows[0].Name != "5h" || account.Windows[0].UsedPercent != 0 {
		t.Fatalf("5h = %+v", account.Windows[0])
	}
	if account.Windows[1].Name != "weekly" || account.Windows[1].UsedPercent != 18 {
		t.Fatalf("weekly = %+v", account.Windows[1])
	}
}

func TestReportFetchesClaudeSubscription(t *testing.T) {
	var path, auth, beta, app string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		auth = request.Header.Get("Authorization")
		beta = request.Header.Get("anthropic-beta")
		app = request.Header.Get("x-app")
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": 20, "resets_at": "2026-08-29T20:00:00Z"},
			"seven_day": map[string]any{"utilization": 10, "resets_at": "2026-09-04T12:00:00Z"},
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer server.Close()

	provider := anthropic.New(anthropic.StaticToken("claude-tok"), "claude-sonnet-4-6", "", time.Second)
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL)
	report := provider.Report(context.Background())
	if path != "/api/oauth/usage" {
		t.Fatalf("path = %q", path)
	}
	if auth != "Bearer claude-tok" || !strings.Contains(beta, "oauth-2025-04-20") || app != "cli" {
		t.Fatalf("headers auth=%q beta=%q app=%q", auth, beta, app)
	}
	if report.Source != "Claude" || len(report.Account.Windows) != 2 {
		t.Fatalf("report = %+v", report)
	}
}

func TestFetchOAuthUsageSurfacesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"type":"authentication_error","message":"token expired"}}`))
	}))
	defer server.Close()

	account := anthropic.FetchOAuthUsage(context.Background(), server.Client(), server.URL, "tok")
	if !strings.Contains(account.Error, "token expired") {
		t.Fatalf("error = %q", account.Error)
	}
}
