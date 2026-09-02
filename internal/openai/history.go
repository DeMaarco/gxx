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
	"fmt"
	"strings"
	"unicode/utf8"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
	"gxx/internal/caveman"
)

func decodeOpenAIHistory(raw json.RawMessage) ([]responses.ResponseInputItemUnionParam, error) {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode openai history: %w", err)
	}
	decoded := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for _, item := range items {
		if _, ok := item["type"]; !ok {
			switch {
			case item["role"] != nil:
				item["type"] = "message"
			case item["output"] != nil && item["call_id"] != nil:
				item["type"] = "function_call_output"
			case item["name"] != nil && item["arguments"] != nil && item["call_id"] != nil:
				item["type"] = "function_call"
			default:
				return nil, fmt.Errorf("decode openai history: unknown item shape")
			}
		}
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("decode openai history: %w", err)
		}
		var union responses.ResponseInputItemUnionParam
		if err := json.Unmarshal(data, &union); err != nil {
			return nil, fmt.Errorf("decode openai history: %w", err)
		}
		decoded = append(decoded, union)
	}
	return decoded, nil
}

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
		p.history = append(p.history, functionCallOutputParam(result.CallID, result.Output))
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
		p.history = append(p.history, functionCallOutputParam(callID, reason))
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
	case "function_call", "custom_tool_call", "shell_call", "local_shell_call", "apply_patch_call":
		return parsed.CallID, true, false
	case "function_call_output", "custom_tool_call_output", "shell_call_output", "local_shell_call_output", "apply_patch_call_output":
		return parsed.CallID, false, true
	default:
		return "", false, false
	}
}

func functionCallOutputParam(callID, output string) responses.ResponseInputItemUnionParam {
	item := responses.ResponseInputItemParamOfFunctionCallOutput(output)
	if callID != "" && item.OfFunctionCallOutput != nil {
		item.OfFunctionCallOutput.CallID = openaisdk.String(callID)
	}
	return item
}

func (p *Provider) shouldCompact(userText string) bool {
	extra := int64(0)
	if userText != "" {
		extra = estimateTokens(len(userText))
	}
	if p.lastInputTokens > 0 && p.contextTokens > 0 {
		projected := p.lastInputTokens + extra
		if projected > int64(p.contextTokens) || projected > p.compactTarget() {
			return true
		}
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
	p.history = dropOldPrograms(dropOldReasoning(p.history))
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
	numer := p.compactNumer
	denom := p.compactDenom
	if numer <= 0 || denom <= 0 {
		numer, denom = 2, 3
	}
	return int64(p.contextTokens) * int64(numer) / int64(denom)
}

const emergencyToolClipBytes = 512

func emergencyFit(
	items []responses.ResponseInputItemUnionParam,
	window int,
	instructions string,
) []responses.ResponseInputItemUnionParam {
	items = dropAllReasoning(items)
	items = dropAllPrograms(items)
	items = clipAllToolOutputs(items, emergencyToolClipBytes)
	if window > 0 {
		items = dropOldTurns(items, int64(window)/2, instructions)
	}
	return items
}

func clipAllToolOutputs(items []responses.ResponseInputItemUnionParam, maxBytes int) []responses.ResponseInputItemUnionParam {
	if maxBytes <= 0 {
		return items
	}
	out := append([]responses.ResponseInputItemUnionParam(nil), items...)
	for index := range out {
		id, _, isOutput := functionCallID(out[index])
		if !isOutput {
			continue
		}
		text := functionCallOutput(out[index])
		if len(text) <= maxBytes {
			continue
		}
		clipped := clipBytes(text, maxBytes)
		if maxBytes > 24 {
			clipped = clipBytes(text, maxBytes-22) + "\n… [context clipped]"
		}
		out[index] = functionCallOutputParam(id, clipped)
	}
	return out
}

func slimInput(items []responses.ResponseInputItemUnionParam, level, keep, clip int) []responses.ResponseInputItemUnionParam {
	items = dropOldPrograms(dropOldReasoning(items))
	if level <= 0 {
		return items
	}
	items = compressInputProse(items, level)
	switch {
	case level >= 3:
		items = dropAllReasoning(items)
		items = clipOldToolOutputs(items, keep, clip)
		items = clipOldUserMessages(items, 1, 400)
	case level >= 2:
		items = keepLatestReasoning(items)
		items = clipOldToolOutputs(items, keep, clip)
	default:
		items = clipOldToolOutputs(items, keep, clip)
	}
	return items
}

func compressInputProse(items []responses.ResponseInputItemUnionParam, level int) []responses.ResponseInputItemUnionParam {
	lastUser := -1
	for index, item := range items {
		if itemKind(item) == "user" {
			lastUser = index
		}
	}
	out := append([]responses.ResponseInputItemUnionParam(nil), items...)
	for index, item := range out {
		if index == lastUser {
			continue
		}
		if text := userMessageText(item); text != "" {
			compressed := caveman.Compress(text, level)
			if compressed != text {
				out[index] = responses.ResponseInputItemParamOfMessage(
					compressed,
					responses.EasyInputMessageRoleUser,
				)
			}
			continue
		}
		id, _, isOutput := functionCallID(item)
		if !isOutput {
			continue
		}
		text := functionCallOutput(item)
		compressed := caveman.Compress(text, level)
		if compressed != text {
			out[index] = functionCallOutputParam(id, compressed)
		}
	}
	return out
}

func clipOldToolOutputs(items []responses.ResponseInputItemUnionParam, keep, maxBytes int) []responses.ResponseInputItemUnionParam {
	if maxBytes <= 0 {
		return items
	}
	if keep < 0 {
		keep = 0
	}
	lastUser := -1
	for index, item := range items {
		if itemKind(item) == "user" {
			lastUser = index
		}
	}
	indexes := make([]int, 0, 8)
	for index, item := range items {
		if lastUser >= 0 && index > lastUser {
			continue
		}
		if _, _, isOutput := functionCallID(item); isOutput {
			indexes = append(indexes, index)
		}
	}
	clipCount := len(indexes) - keep
	if clipCount <= 0 {
		return items
	}
	out := append([]responses.ResponseInputItemUnionParam(nil), items...)
	for _, index := range indexes[:clipCount] {
		id, _, _ := functionCallID(out[index])
		text := functionCallOutput(out[index])
		if len(text) <= maxBytes {
			continue
		}
		clipped := clipBytes(text, maxBytes)
		if maxBytes > 24 {
			clipped = clipBytes(text, maxBytes-18) + "\n… [eco clipped]"
		}
		out[index] = functionCallOutputParam(id, clipped)
	}
	return out
}

func clipOldUserMessages(items []responses.ResponseInputItemUnionParam, keep, maxRunes int) []responses.ResponseInputItemUnionParam {
	if maxRunes <= 0 {
		return items
	}
	indexes := userIndexes(items)
	clipCount := len(indexes) - keep
	if clipCount <= 0 {
		return items
	}
	out := append([]responses.ResponseInputItemUnionParam(nil), items...)
	for _, index := range indexes[:clipCount] {
		text := userMessageText(out[index])
		if text == "" || utf8.RuneCountInString(text) <= maxRunes {
			continue
		}
		out[index] = responses.ResponseInputItemParamOfMessage(
			clipRunes(text, maxRunes),
			responses.EasyInputMessageRoleUser,
		)
	}
	return out
}

func isProgramItem(item responses.ResponseInputItemUnionParam) bool {
	return item.OfProgram != nil || item.OfProgramOutput != nil
}

func dropAllPrograms(items []responses.ResponseInputItemUnionParam) []responses.ResponseInputItemUnionParam {
	kept := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for _, item := range items {
		if isProgramItem(item) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func dropOldPrograms(items []responses.ResponseInputItemUnionParam) []responses.ResponseInputItemUnionParam {
	lastUser := -1
	for index, item := range items {
		if itemKind(item) == "user" {
			lastUser = index
		}
	}
	kept := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for index, item := range items {
		if isProgramItem(item) && (lastUser < 0 || index < lastUser) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func dropAllReasoning(items []responses.ResponseInputItemUnionParam) []responses.ResponseInputItemUnionParam {
	kept := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for _, item := range items {
		if item.OfReasoning != nil {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func keepLatestReasoning(items []responses.ResponseInputItemUnionParam) []responses.ResponseInputItemUnionParam {
	last := -1
	for index, item := range items {
		if item.OfReasoning != nil {
			last = index
		}
	}
	if last < 0 {
		return items
	}
	kept := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for index, item := range items {
		if item.OfReasoning != nil && index != last {
			continue
		}
		kept = append(kept, item)
	}
	return kept
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
