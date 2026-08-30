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

package pricing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openaiPricingURL    = "https://developers.openai.com/api/docs/pricing.md"
	anthropicPricingURL = "https://platform.claude.com/docs/en/about-claude/pricing.md"
	maxPricingBodyBytes = 2 << 20
	refreshTimeout      = 5 * time.Second
)

// RefreshIfStale re-reads official pricing when the last fetch is older than maxAge.
func (c *Catalog) RefreshIfStale(ctx context.Context, maxAge time.Duration) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	stale := c.fetched.IsZero() || time.Since(c.fetched) >= maxAge
	c.mu.Unlock()
	if !stale {
		return nil
	}
	return c.Refresh(ctx)
}

// Refresh replaces cached rates with the official OpenAI and Anthropic pages.
// Bundled snapshot rows stay when a document omits that model.
func (c *Catalog) Refresh(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, refreshTimeout)
		defer cancel()
	}

	var errs []error
	applied := false
	if rates, err := c.fetch(ctx, c.openaiURL, parseOpenAI); err != nil {
		errs = append(errs, fmt.Errorf("openai pricing: %w", err))
	} else if len(rates) > 0 {
		c.merge(rates)
		applied = true
	}
	if rates, err := c.fetch(ctx, c.claudeURL, parseAnthropic); err != nil {
		errs = append(errs, fmt.Errorf("anthropic pricing: %w", err))
	} else if len(rates) > 0 {
		c.merge(rates)
		applied = true
	}
	if applied {
		c.mu.Lock()
		c.fetched = time.Now()
		c.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (c *Catalog) merge(live map[rateKey]Rate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, rate := range live {
		c.rates[key] = rate
	}
}

func (c *Catalog) fetch(ctx context.Context, rawURL string, parse func([]byte) map[rateKey]Rate) (map[rateKey]Rate, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("pricing url is empty")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/markdown, text/plain, */*")
	request.Header.Set("User-Agent", "gxx")

	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPricingBodyBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	rates := parse(body)
	if len(rates) == 0 {
		return nil, errors.New("no model rates in document")
	}
	return rates, nil
}
