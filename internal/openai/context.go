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
		InstructionsTokens: estimateTokens(len(p.instructions)),
	}
	for _, item := range p.history {
		tokens := estimateJSON(item)
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
	return historyTokens(items, p.instructions) > int64(p.contextTokens)
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
