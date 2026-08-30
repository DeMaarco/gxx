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

package pricing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/pricing"
)

func TestEstimateUsesBundledOpenAIRates(t *testing.T) {
	catalog := pricing.New(http.DefaultClient)
	got, ok := catalog.Estimate(pricing.Query{
		Model: "gpt-5.6-sol",
		Usage: agent.Usage{InputTokens: 100_000, OutputTokens: 20_000},
	})
	if !ok {
		t.Fatal("estimate should succeed for bundled sol")
	}
	if abs(got-0.8) > 0.0001 {
		t.Fatalf("sol cost = %v, want $0.80", got)
	}
}

func TestEstimateSplitsCachedAndWrites(t *testing.T) {
	catalog := pricing.New(nil)
	got, ok := catalog.Estimate(pricing.Query{
		Model: "sol",
		Usage: agent.Usage{
			InputTokens:      100_000,
			CachedTokens:     40_000,
			CacheWriteTokens: 10_000,
			OutputTokens:     0,
		},
	})
	if !ok {
		t.Fatal("estimate should succeed")
	}
	// 50k input * $4 + 40k cached * $0.40 + 10k write * $5 = 0.20 + 0.016 + 0.05
	if abs(got-0.266) > 0.0001 {
		t.Fatalf("cached cost = %v, want $0.266", got)
	}
}

func TestEstimateUsesLongContextWhenInputExceeds272k(t *testing.T) {
	catalog := pricing.New(nil)
	got, ok := catalog.Estimate(pricing.Query{
		Model: "gpt-5.6-terra",
		Usage: agent.Usage{InputTokens: 300_000, OutputTokens: 0},
	})
	if !ok {
		t.Fatal("estimate should succeed")
	}
	want := 300_000.0 / 1_000_000 * 4
	if abs(got-want) > 0.0001 {
		t.Fatalf("long terra = %v, want %v", got, want)
	}
}

func TestEstimateUsesFastRates(t *testing.T) {
	catalog := pricing.New(nil)
	got, ok := catalog.Estimate(pricing.Query{
		Model: "claude-opus-5",
		Fast:  true,
		Usage: agent.Usage{InputTokens: 1_000_000, OutputTokens: 100_000},
	})
	if !ok {
		t.Fatal("estimate should succeed")
	}
	if got != 15 {
		t.Fatalf("fast opus = %v, want $15", got)
	}
}

func TestEstimateUnknownModel(t *testing.T) {
	catalog := pricing.New(nil)
	if _, ok := catalog.Estimate(pricing.Query{
		Model: "mystery-model",
		Usage: agent.Usage{InputTokens: 10},
	}); ok {
		t.Fatal("unknown model should not estimate")
	}
}

func TestRefreshAppliesUpdatedOfficialRates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/openai.md", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`
### Standard pricing data

| Model | Short context input | Short context cached input | Short context cache writes | Short context output | Long context input | Long context cached input | Long context cache writes | Long context output |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gpt-5.6-sol | $9.00 | $0.90 | $11.00 | $40.00 | $18.00 | $1.80 | $22.00 | $60.00 |
`))
	})
	mux.HandleFunc("/claude.md", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`
## Model pricing

| Model | Base Input Tokens | 5m Cache Writes | 1h Cache Writes | Cache Hits & Refreshes | Output Tokens |
| --- | --- | --- | --- | --- | --- |
| Claude Sonnet 5 | $3 / MTok | $3.75 / MTok | $6 / MTok | $0.30 / MTok | $15 / MTok |
`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	catalog := pricing.New(server.Client())
	catalog.SetURLs(server.URL+"/openai.md", server.URL+"/claude.md")
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	sol, ok := catalog.Estimate(pricing.Query{
		Model: "gpt-5.6-sol",
		Usage: agent.Usage{InputTokens: 100_000},
	})
	if !ok || abs(sol-0.9) > 0.0001 {
		t.Fatalf("updated sol = %v ok=%v, want $0.90", sol, ok)
	}
	sonnet, ok := catalog.Estimate(pricing.Query{
		Model: "sonnet",
		Usage: agent.Usage{InputTokens: 1_000_000},
	})
	if !ok || sonnet != 3 {
		t.Fatalf("updated sonnet = %v ok=%v, want $3", sonnet, ok)
	}
}

func TestRefreshIfStaleSkipsRecentFetch(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		if strings.Contains(request.URL.Path, "openai") {
			_, _ = writer.Write([]byte(`
### Standard pricing data
| Model | Short context input | Short context cached input | Short context cache writes | Short context output | Long context input | Long context cached input | Long context cache writes | Long context output |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gpt-5.6-luna | $0.20 | $0.02 | $0.25 | $1.20 | $0.40 | $0.04 | $0.50 | $1.80 |
`))
			return
		}
		_, _ = writer.Write([]byte(`
## Model pricing
| Model | Base Input Tokens | 5m Cache Writes | 1h Cache Writes | Cache Hits & Refreshes | Output Tokens |
| --- | --- | --- | --- | --- | --- |
| Claude Haiku 4.5 | $1 / MTok | $1.25 / MTok | $2 / MTok | $0.10 / MTok | $5 / MTok |
`))
	}))
	t.Cleanup(server.Close)

	catalog := pricing.New(server.Client())
	catalog.SetURLs(server.URL+"/openai.md", server.URL+"/claude.md")
	if err := catalog.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	first := hits
	if err := catalog.RefreshIfStale(context.Background(), time.Hour); err != nil {
		t.Fatalf("stale: %v", err)
	}
	if hits != first {
		t.Fatalf("hits = %d, want unchanged %d", hits, first)
	}
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
