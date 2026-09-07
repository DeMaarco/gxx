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

package eval

import (
	"context"
	"errors"
	"sync"

	"gxx/internal/agent"
	"gxx/internal/pricing"
)

var ErrBudget = errors.New("evaluation request or token budget exhausted")

// Meter includes retry attempts and summaries. Tokens are checked after usage
// is reported; the last in-flight response can exceed the remaining token cap.
type Meter struct {
	mu          sync.Mutex
	maxTokens   int64
	maxRequests int
	usage       agent.Usage
	requests    int
	unknown     int
	cost        float64
	costKnown   bool
}

type Measurement struct {
	Usage     agent.Usage
	Requests  int
	Unknown   int
	Cost      float64
	CostKnown bool
}

func NewMeter(tokens int64, requests int) *Meter {
	return &Meter{maxTokens: tokens, maxRequests: requests, costKnown: true}
}

func (m *Meter) Exhausted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exhausted()
}

func (m *Meter) exhausted() bool {
	return (m.maxTokens > 0 && m.usage.TotalTokens >= m.maxTokens) || (m.maxRequests > 0 && m.requests >= m.maxRequests)
}

func (m *Meter) Before(ctx context.Context, request agent.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exhausted() {
		return ErrBudget
	}
	m.requests++
	return nil
}

func (m *Meter) After(request agent.Request, usage agent.Usage, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	m.usage.Add(usage)
	if usage.TotalTokens == 0 {
		m.unknown++
		m.costKnown = false
	}
	if cost, known := pricing.Default().Estimate(pricing.Query{Model: request.Model, Usage: usage}); known {
		m.cost += cost
	} else {
		m.costKnown = false
	}
}

func (m *Meter) Snapshot() Measurement {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Measurement{m.usage, m.requests, m.unknown, m.cost, m.costKnown}
}

// trialMeter shares the global gate, but accounts each attempt independently.
type trialMeter struct{ global, local *Meter }

func (m trialMeter) Before(ctx context.Context, r agent.Request) error {
	if err := m.global.Before(ctx, r); err != nil {
		return err
	}
	return m.local.Before(ctx, r)
}
func (m trialMeter) After(r agent.Request, u agent.Usage, err error) {
	m.global.After(r, u, err)
	m.local.After(r, u, err)
}
