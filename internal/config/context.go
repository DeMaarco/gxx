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

package config

import (
	"fmt"
	"strings"
)

// Context size labels match published API windows where possible.
// OpenAI: 272k is the long-context pricing breakpoint; 1m is the full window.
// Claude 1M models also offer 300k as a cheaper local compact budget.
// Artificial sizes like 32k/128k are not offered.
const (
	Context200k = "200k"
	Context272k = "272k"
	Context300k = "300k"
	Context1m   = "1m"
)

// ContextSizes is the union of known labels. Prefer ContextSizesForModel.
var ContextSizes = []string{Context200k, Context272k, Context300k, Context1m}

var contextTokens = map[string]int{
	Context200k: 200_000,
	Context272k: 272_000,
	Context300k: 300_000,
	Context1m:   1_050_000,
}

// ContextSizesForModel returns the official context budgets for a model.
// OpenAI gpt-5.6: 272k (standard / pricing breakpoint) and 1.05M (full window).
// Claude 1M models: 300k (cost-saving local budget) and 1m (full window).
// Claude Haiku: 200k only.
func ContextSizesForModel(model string) []string {
	model = CanonicalModel(model)
	switch {
	case IsOpenAIModel(model):
		return []string{Context272k, Context1m}
	case IsClaudeModel(model):
		if strings.Contains(strings.ToLower(model), "haiku") {
			return []string{Context200k}
		}
		return []string{Context300k, Context1m}
	default:
		return []string{Context272k}
	}
}

// DefaultContextForModel is the first / preferred budget for a model.
func DefaultContextForModel(model string) string {
	sizes := ContextSizesForModel(model)
	if len(sizes) == 0 {
		return DefaultContext
	}
	return sizes[0]
}

// ClampContextForModel maps a label onto one allowed for the model.
func ClampContextForModel(model, value string) string {
	sizes := ContextSizesForModel(model)
	if len(sizes) == 0 {
		return DefaultContext
	}
	normalized, err := NormalizeContext(value)
	if err != nil {
		return sizes[0]
	}
	for _, size := range sizes {
		if size == normalized {
			return normalized
		}
	}
	// Legacy / cross-provider: pick the closest allowed size by token count.
	want := contextTokens[normalized]
	best := sizes[0]
	bestDist := absInt(contextTokenCount(model, best) - want)
	for _, size := range sizes[1:] {
		dist := absInt(contextTokenCount(model, size) - want)
		if dist < bestDist {
			best = size
			bestDist = dist
		}
	}
	return best
}

// ValidateContextForModel rejects labels the model does not support.
func ValidateContextForModel(model, value string) error {
	normalized, err := NormalizeContext(value)
	if err != nil {
		return err
	}
	for _, size := range ContextSizesForModel(model) {
		if size == normalized {
			return nil
		}
	}
	return fmt.Errorf(
		"context %s is not supported by %s (use %s)",
		normalized,
		CanonicalModel(model),
		strings.Join(ContextSizesForModel(model), ", "),
	)
}

func NormalizeContext(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "200k", "200000":
		return Context200k, nil
	case "272k", "272000":
		return Context272k, nil
	case "300k", "300000":
		return Context300k, nil
	case "1m", "1050k", "1.05m", "1050000", "1000000", "1000k":
		return Context1m, nil
	case "32k", "32000", "128k", "128000":
		return "", fmt.Errorf("context %s is no longer supported; use model-native sizes", strings.TrimSpace(value))
	case "":
		return "", fmt.Errorf("context cannot be empty")
	default:
		return "", fmt.Errorf("context must be one of %s", strings.Join(ContextSizes, ", "))
	}
}

func isRemovedContextLabel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "32k", "32000", "128k", "128000":
		return true
	default:
		return false
	}
}

// ContextTokens returns the OpenAI-oriented token count for a label.
func ContextTokens(value string) int {
	label, err := NormalizeContext(value)
	if err != nil {
		return contextTokens[DefaultContext]
	}
	return contextTokens[label]
}

// ContextTokensForModel returns the API window size for a model + label.
func ContextTokensForModel(model, value string) int {
	label := ClampContextForModel(model, value)
	return contextTokenCount(model, label)
}

// ContextTokensFor returns a provider-scoped window for a size label.
// Prefer ContextTokensForModel when the model ID is known.
func ContextTokensFor(provider, value string) int {
	if provider == ProviderAnthropic {
		return ContextTokensForModel(DefaultClaudeModel, value)
	}
	return ContextTokensForModel(DefaultModel, value)
}

func contextTokenCount(model, label string) int {
	if IsClaudeModel(model) {
		switch label {
		case Context200k:
			return 200_000
		case Context300k:
			return 300_000
		case Context1m:
			return 1_000_000
		case Context272k:
			return 300_000
		}
	}
	if n, ok := contextTokens[label]; ok {
		return n
	}
	return contextTokens[DefaultContext]
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
