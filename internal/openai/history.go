package openai

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

const (
	compactNotice        = "Earlier conversation was compacted by gxx to fit the context window. Continue from the remaining history."
	unansweredToolOutput = "error: tool call was not executed"
)

func (p *Provider) AbsorbToolResults(results []agent.ToolResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.appendToolResultsLocked(results)
	p.refreshContextLocked()
}

func (p *Provider) CloseOpenToolCalls(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeOpenFunctionCallsLocked(reason)
	p.refreshContextLocked()
}

func (p *Provider) appendToolResultsLocked(results []agent.ToolResult) {
	open := unmatchedCallIDs(p.history)
	for _, result := range results {
		if !open[result.CallID] {
			continue
		}
		p.history = append(p.history, responses.ResponseInputItemParamOfFunctionCallOutput(
			result.CallID,
			result.Output,
		))
		delete(open, result.CallID)
	}
}

func (p *Provider) closeOpenFunctionCallsLocked(reason string) int {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = unansweredToolOutput
	}
	closed := 0
	for callID := range unmatchedCallIDs(p.history) {
		p.history = append(p.history, responses.ResponseInputItemParamOfFunctionCallOutput(
			callID,
			reason,
		))
		closed++
	}
	return closed
}

func unmatchedCallIDs(items []responses.ResponseInputItemUnionParam) map[string]bool {
	open := make(map[string]bool)
	for _, item := range items {
		id, isCall, isOutput := functionCallID(item)
		if id == "" {
			continue
		}
		if isCall {
			open[id] = true
			continue
		}
		if isOutput {
			delete(open, id)
		}
	}
	return open
}

func functionCallID(item responses.ResponseInputItemUnionParam) (id string, isCall, isOutput bool) {
	data, err := json.Marshal(item)
	if err != nil {
		return "", false, false
	}
	var parsed struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", false, false
	}
	switch parsed.Type {
	case "function_call":
		return parsed.CallID, true, false
	case "function_call_output":
		return parsed.CallID, false, true
	default:
		return "", false, false
	}
}

func (p *Provider) shouldCompact(userText string) bool {
	projected := append([]responses.ResponseInputItemUnionParam(nil), p.history...)
	projected = append(projected, responses.ResponseInputItemParamOfMessage(
		userText,
		responses.EasyInputMessageRoleUser,
	))
	return p.overBudget(projected)
}

func (p *Provider) compactLocked(emit agent.EmitFunc) {
	original := len(p.history)
	p.history = dropOldReasoning(p.history)
	if p.overBudget(p.history) {
		p.history = dropOldTurns(p.history, p.compactTarget(), p.instructions)
	}
	if len(p.history) == original {
		return
	}
	agent.Emit(emit, agent.Event{
		Kind: agent.EventNotice,
		Text: compactNotice,
	})
}

func (p *Provider) compactTarget() int64 {
	if p.contextTokens <= 0 {
		return int64(fallbackHistoryItems / 2)
	}
	return int64(p.contextTokens) * 2 / 3
}

func dropOldReasoning(items []responses.ResponseInputItemUnionParam) []responses.ResponseInputItemUnionParam {
	lastUser := -1
	for index, item := range items {
		if itemKind(item) == "user" {
			lastUser = index
		}
	}
	kept := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for index, item := range items {
		if item.OfReasoning != nil && (lastUser < 0 || index < lastUser) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func dropOldTurns(
	items []responses.ResponseInputItemUnionParam,
	target int64,
	instructions string,
) []responses.ResponseInputItemUnionParam {
	users := userIndexes(items)
	if len(users) <= 1 {
		return items
	}

	notice := responses.ResponseInputItemParamOfMessage(
		compactNotice,
		responses.EasyInputMessageRoleUser,
	)
	best := items
	for _, start := range users[1:] {
		candidate := append([]responses.ResponseInputItemUnionParam{notice}, items[start:]...)
		best = candidate
		if historyTokens(candidate, instructions) <= target {
			return candidate
		}
	}
	return best
}

func userIndexes(items []responses.ResponseInputItemUnionParam) []int {
	indexes := make([]int, 0, 8)
	for index, item := range items {
		if itemKind(item) == "user" {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func historyTokens(items []responses.ResponseInputItemUnionParam, instructions string) int64 {
	used := estimateTokens(len(instructions))
	for _, item := range items {
		used += estimateJSON(item)
	}
	return used
}
