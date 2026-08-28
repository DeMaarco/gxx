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
	"strings"
	"unicode/utf8"

	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

const (
	compactNotice           = "Earlier conversation was compacted by gxx to fit the context window. Continue from the remaining history."
	unansweredToolOutput    = "error: tool call was not executed"
	compactSummaryMaxBytes  = 4 * 1024
	compactSummaryClipRunes = 160
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
	extra := int64(0)
	if userText != "" {
		extra = estimateTokens(len(userText))
	}
	if p.lastInputTokens > 0 && p.contextTokens > 0 && p.lastInputTokens+extra > p.compactTarget() {
		return true
	}
	if userText == "" {
		return p.overTarget(p.history)
	}
	projected := append([]responses.ResponseInputItemUnionParam(nil), p.history...)
	projected = append(projected, responses.ResponseInputItemParamOfMessage(
		userText,
		responses.EasyInputMessageRoleUser,
	))
	return p.overTarget(projected)
}

func (p *Provider) compactLocked(emit agent.EmitFunc) {
	original := len(p.history)
	p.history = dropOldReasoning(p.history)
	if p.overTarget(p.history) || (p.lastInputTokens > 0 && p.lastInputTokens > p.compactTarget()) {
		p.history = dropOldTurns(p.history, p.compactTarget(), p.instructions)
	}
	if len(p.history) == original {
		return
	}
	p.lastInputTokens = historyTokens(p.history, p.instructions)
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

	best := items
	for _, start := range users[1:] {
		notice := responses.ResponseInputItemParamOfMessage(
			summarizeDropped(items[:start]),
			responses.EasyInputMessageRoleUser,
		)
		candidate := append([]responses.ResponseInputItemUnionParam{notice}, items[start:]...)
		best = candidate
		if historyTokens(candidate, instructions) <= target {
			return candidate
		}
	}
	return best
}

func summarizeDropped(items []responses.ResponseInputItemUnionParam) string {
	var users []string
	var tools []string
	seenTool := make(map[string]bool)
	var errors []string
	for _, item := range items {
		if text := userMessageText(item); text != "" {
			if strings.HasPrefix(text, compactNotice) {
				continue
			}
			users = append(users, clipRunes(text, compactSummaryClipRunes))
			continue
		}
		if name := functionCallName(item); name != "" {
			if !seenTool[name] {
				seenTool[name] = true
				tools = append(tools, name)
			}
			continue
		}
		if output := functionCallOutput(item); strings.HasPrefix(output, "error:") {
			errors = append(errors, clipRunes(output, compactSummaryClipRunes))
		}
	}
	users = lastStrings(users, 3)
	errors = lastStrings(errors, 5)

	var builder strings.Builder
	builder.WriteString(compactNotice)
	if len(users) > 0 {
		builder.WriteString("\nPrior user requests:")
		for _, user := range users {
			builder.WriteString("\n- ")
			builder.WriteString(user)
		}
	}
	if len(tools) > 0 {
		builder.WriteString("\nTools used: ")
		builder.WriteString(strings.Join(tools, ", "))
	}
	if len(errors) > 0 {
		builder.WriteString("\nRecent tool errors:")
		for _, message := range errors {
			builder.WriteString("\n- ")
			builder.WriteString(message)
		}
	}
	return clipBytes(builder.String(), compactSummaryMaxBytes)
}

func userMessageText(item responses.ResponseInputItemUnionParam) string {
	if itemKind(item) != "user" {
		return ""
	}
	return inputItemText(item)
}

func functionCallName(item responses.ResponseInputItemUnionParam) string {
	if item.OfFunctionCall != nil {
		return item.OfFunctionCall.Name
	}
	data, err := json.Marshal(item)
	if err != nil {
		return ""
	}
	var parsed struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &parsed) != nil || parsed.Type != "function_call" {
		return ""
	}
	return parsed.Name
}

func functionCallOutput(item responses.ResponseInputItemUnionParam) string {
	data, err := json.Marshal(item)
	if err != nil {
		return ""
	}
	var parsed struct {
		Type   string `json:"type"`
		Output string `json:"output"`
	}
	if json.Unmarshal(data, &parsed) != nil || parsed.Type != "function_call_output" {
		return ""
	}
	return parsed.Output
}

func inputItemText(item responses.ResponseInputItemUnionParam) string {
	data, err := json.Marshal(item)
	if err != nil {
		return ""
	}
	var parsed struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return ""
	}
	var text string
	if json.Unmarshal(parsed.Content, &text) == nil {
		return strings.TrimSpace(text)
	}
	return ""
}

func lastStrings(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[len(values)-n:]
}

func clipRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}

func clipBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	keep := limit
	for keep > 0 && !utf8.RuneStart(value[keep]) {
		keep--
	}
	return value[:keep]
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
