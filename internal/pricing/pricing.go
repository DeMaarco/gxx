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

// Package pricing estimates USD cost from token usage and live official rates.
package pricing

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"gxx/internal/agent"
	"gxx/internal/config"
)

const (
	openaiLongContextTokens = 272_000
	perMillion              = 1_000_000.0
)

// Rate is USD per 1M tokens.
type Rate struct {
	Input      float64
	Cached     float64
	CacheWrite float64
	Output     float64
}

// Query is one estimate: model, fast tier, and token counts.
type Query struct {
	Model string
	Fast  bool
	Usage agent.Usage
}

type rateKey struct {
	model string
	fast  bool
	long  bool
}

// Catalog holds bundled rates and any later refresh from official docs.
type Catalog struct {
	client    *http.Client
	openaiURL string
	claudeURL string

	refreshMu sync.Mutex
	mu        sync.Mutex
	rates     map[rateKey]Rate
	fetched   time.Time
}

var defaultCatalog = New(nil)

// Default is the process-wide catalog used by the CLI.
func Default() *Catalog {
	return defaultCatalog
}

// New starts from the bundled snapshot. A nil client uses a short-timeout one.
func New(client *http.Client) *Catalog {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &Catalog{
		client:    client,
		openaiURL: openaiPricingURL,
		claudeURL: anthropicPricingURL,
		rates:     cloneRates(bundledRates()),
	}
}

// SetURLs overrides the official pricing documents. Empty keeps the current URL.
func (c *Catalog) SetURLs(openaiURL, claudeURL string) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if strings.TrimSpace(openaiURL) != "" {
		c.openaiURL = strings.TrimSpace(openaiURL)
	}
	if strings.TrimSpace(claudeURL) != "" {
		c.claudeURL = strings.TrimSpace(claudeURL)
	}
}

// Estimate returns USD for the query using the latest cached rates.
func (c *Catalog) Estimate(query Query) (float64, bool) {
	if c == nil {
		return 0, false
	}
	usage := query.Usage
	if usage.InputTokens <= 0 && usage.OutputTokens <= 0 &&
		usage.CachedTokens <= 0 && usage.CacheWriteTokens <= 0 {
		return 0, false
	}
	rate, ok := c.Lookup(query.Model, query.Fast, longContext(query))
	if !ok {
		return 0, false
	}
	return estimate(rate, usage), true
}

// Lookup returns the best matching rate for a model and billing knobs.
func (c *Catalog) Lookup(model string, fast, long bool) (Rate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return lookupRate(c.rates, model, fast, long)
}

func longContext(query Query) bool {
	return query.Usage.InputTokens > openaiLongContextTokens
}

func estimate(rate Rate, usage agent.Usage) float64 {
	if rate.Cached == 0 {
		rate.Cached = rate.Input
	}
	if rate.CacheWrite == 0 {
		rate.CacheWrite = rate.Input
	}
	cached := usage.CachedTokens
	if cached < 0 {
		cached = 0
	}
	writes := usage.CacheWriteTokens
	if writes < 0 {
		writes = 0
	}
	uncached := usage.InputTokens - cached
	if writes > 0 && writes <= uncached {
		uncached -= writes
	}
	if uncached < 0 {
		uncached = 0
	}
	output := usage.OutputTokens
	if output < 0 {
		output = 0
	}
	return (float64(uncached)*rate.Input +
		float64(cached)*rate.Cached +
		float64(writes)*rate.CacheWrite +
		float64(output)*rate.Output) / perMillion
}

func lookupRate(rates map[rateKey]Rate, model string, fast, long bool) (Rate, bool) {
	for _, id := range modelCandidates(model) {
		for _, wantFast := range preferredBool(fast) {
			for _, wantLong := range preferredBool(long) {
				if rate, ok := rates[rateKey{model: id, fast: wantFast, long: wantLong}]; ok {
					return rate, true
				}
			}
		}
	}
	return Rate{}, false
}

func preferredBool(want bool) []bool {
	if want {
		return []bool{true, false}
	}
	return []bool{false}
}

func modelCandidates(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	canonical := config.CanonicalModel(model)
	seen := map[string]struct{}{}
	var out []string
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(canonical)
	add(stripDatedSuffix(canonical))
	add(model)
	add(stripDatedSuffix(model))
	return out
}

func cloneRates(src map[rateKey]Rate) map[rateKey]Rate {
	out := make(map[rateKey]Rate, len(src))
	for key, rate := range src {
		out[key] = rate
	}
	return out
}
