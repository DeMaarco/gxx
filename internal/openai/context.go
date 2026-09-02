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
	"encoding/json"

	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

func (p *Provider) ContextSnapshot() agent.ContextUsage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.contextUsage
}

func (p *Provider) refreshContextLocked() {
	p.contextUsage = p.computeContextLocked()
}

func (p *Provider) computeContextLocked() agent.ContextUsage {
	usage := agent.ContextUsage{
		WindowTokens:       int64(p.contextTokens),
		InstructionsTokens: p.calibrate(estimateTokens(len(p.instructions))),
	}
	for _, item := range p.history {
		tokens := p.calibrate(estimateJSON(item))
		switch itemKind(item) {
		case "user":
			usage.UserTokens += tokens
		case "assistant":
			usage.AssistantTokens += tokens
		case "reasoning":
			usage.ReasoningTokens += tokens
		default:
			usage.ToolTokens += tokens
		}
	}
	usage.UsedTokens = usage.InstructionsTokens +
		usage.UserTokens +
		usage.AssistantTokens +
		usage.ReasoningTokens +
		usage.ToolTokens
	usage.Percent = agent.ContextPercent(usage.UsedTokens, usage.WindowTokens)
	return usage
}

func (p *Provider) overBudget(items []responses.ResponseInputItemUnionParam) bool {
	if p.contextTokens <= 0 {
		return len(items) > fallbackHistoryItems
	}
	return p.calibrate(historyTokens(items, p.instructions)) > int64(p.contextTokens)
}

func (p *Provider) overTarget(items []responses.ResponseInputItemUnionParam) bool {
	if p.contextTokens <= 0 {
		return len(items) > fallbackHistoryItems
	}
	return p.calibrate(historyTokens(items, p.instructions)) > p.compactTarget()
}

func (p *Provider) calibrate(tokens int64) int64 {
	factor := p.tokenFactor
	if factor <= 0 {
		factor = 1.0
	}
	return int64(float64(tokens) * factor)
}

func (p *Provider) updateTokenFactorLocked(staged []responses.ResponseInputItemUnionParam) {
	est := historyTokens(staged, p.instructions)
	if est <= 0 || p.lastInputTokens <= 0 {
		return
	}
	observed := float64(p.lastInputTokens) / float64(est)
	if observed < 0.5 {
		observed = 0.5
	} else if observed > 2.0 {
		observed = 2.0
	}
	old := p.tokenFactor
	if old <= 0 {
		old = 1.0
	}
	p.tokenFactor = 0.3*observed + 0.7*old
}

func itemKind(item responses.ResponseInputItemUnionParam) string {
	switch {
	case item.OfMessage != nil:
		if item.OfMessage.Role == responses.EasyInputMessageRoleAssistant {
			return "assistant"
		}
		return "user"
	case item.OfOutputMessage != nil:
		return "assistant"
	case item.OfReasoning != nil:
		return "reasoning"
	default:
		return "tools"
	}
}

func estimateJSON(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return estimateTokens(len(data))
}

func estimateTokens(bytes int) int64 {
	if bytes <= 0 {
		return 0
	}
	return int64(bytes / 4)
}
