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
	"testing"

	"gxx/internal/agent"
	"gxx/internal/pricing"
)

func TestParseOpenAIReadsStandardAndFast(t *testing.T) {
	catalog := pricing.New(nil)
	catalog.ImportDocuments([]byte(`
### Standard pricing data

| Model | Short context input | Short context cached input | Short context cache writes | Short context output | Long context input | Long context cached input | Long context cache writes | Long context output |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gpt-5.6-sol | $4.00 | $0.40 | $5.00 | $20.00 | $8.00 | $0.80 | $10.00 | $30.00 |

### Batch pricing data

| Model | Short context input | Short context cached input | Short context cache writes | Short context output | Long context input | Long context cached input | Long context cache writes | Long context output |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gpt-5.6-sol | $2.00 | $0.20 | $2.50 | $10.00 | $4.00 | $0.40 | $5.00 | $15.00 |

### Fast pricing data

| Model | Short context input | Short context cached input | Short context cache writes | Short context output | Long context input | Long context cached input | Long context cache writes | Long context output |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| gpt-5.6-sol | $8.00 | $0.80 | $10.00 | $40.00 | $16.00 | $1.60 | $20.00 | $60.00 |
`), nil)

	if got, ok := catalog.Estimate(pricing.Query{
		Model: "gpt-5.6-sol",
		Usage: agent.Usage{InputTokens: 100_000},
	}); !ok || abs(got-0.4) > 0.0001 {
		t.Fatalf("standard = %v ok=%v, want $0.40 (not batch $0.20)", got, ok)
	}
	if got, ok := catalog.Estimate(pricing.Query{
		Model: "gpt-5.6-sol",
		Fast:  true,
		Usage: agent.Usage{InputTokens: 100_000},
	}); !ok || abs(got-0.8) > 0.0001 {
		t.Fatalf("fast = %v ok=%v, want $0.80", got, ok)
	}
	if got, ok := catalog.Estimate(pricing.Query{
		Model: "gpt-5.6-sol",
		Usage: agent.Usage{InputTokens: 300_000},
	}); !ok || abs(got-2.4) > 0.0001 {
		t.Fatalf("long = %v ok=%v, want $2.40", got, ok)
	}
}

func TestParseAnthropicReadsModelAndFastTables(t *testing.T) {
	catalog := pricing.New(nil)
	catalog.ImportDocuments(nil, []byte(`
## Model pricing

| Model | Base Input Tokens | 5m Cache Writes | 1h Cache Writes | Cache Hits & Refreshes | Output Tokens |
| --- | --- | --- | --- | --- | --- |
| Claude Sonnet 5 | $7 / MTok | $8.75 / MTok | $14 / MTok | $0.70 / MTok | $35 / MTok |
| Claude Mythos 5 (limited availability) | $11 / MTok | $13.75 / MTok | $22 / MTok | $1.10 / MTok | $55 / MTok |

## Cloud platform pricing

| Concept | Details |
| --- | --- |
| Billing unit | Claude Consumption Unit (CCU) |

### Fast mode pricing

| Model | Input | Output |
| --- | --- | --- |
| Claude Opus 5 / Claude Opus 4.8 | $12 / MTok | $60 / MTok |
`))

	if got, ok := catalog.Estimate(pricing.Query{
		Model: "claude-sonnet-5",
		Usage: agent.Usage{InputTokens: 1_000_000},
	}); !ok || got != 7 {
		t.Fatalf("sonnet = %v ok=%v, want $7 from parsed doc", got, ok)
	}
	if got, ok := catalog.Estimate(pricing.Query{
		Model: "mythos",
		Usage: agent.Usage{InputTokens: 1_000_000},
	}); !ok || got != 11 {
		t.Fatalf("mythos = %v ok=%v, want $11", got, ok)
	}
	if got, ok := catalog.Estimate(pricing.Query{
		Model: "claude-opus-5",
		Fast:  true,
		Usage: agent.Usage{InputTokens: 1_000_000},
	}); !ok || got != 12 {
		t.Fatalf("opus fast = %v ok=%v, want $12", got, ok)
	}
	if got, ok := catalog.Estimate(pricing.Query{
		Model: "claude-opus-4-8",
		Fast:  true,
		Usage: agent.Usage{InputTokens: 1_000_000},
	}); !ok || got != 12 {
		t.Fatalf("opus 4.8 fast = %v ok=%v, want $12", got, ok)
	}
}
