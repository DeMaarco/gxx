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

package anthropic

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/agent"
	"gxx/internal/caveman"
)

const (
	compactNotice           = "Earlier conversation was compacted by gxx to fit the context window. Continue from the remaining history."
	unansweredToolOutput    = "error: tool call was not executed"
	compactSummaryMaxBytes  = 4 * 1024
	compactSummaryClipRunes = 160
	emergencyToolClipBytes  = 512
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
	p.closeOpenToolCallsLocked(reason)
	p.refreshContextLocked()
}

func (p *Provider) appendToolResultsLocked(results []agent.ToolResult) {
	open := unmatchedToolIDs(p.history)
	var blocks []anthropicsdk.ContentBlockParamUnion
	for _, result := range results {
		if !open[result.CallID] {
			continue
		}
		blocks = append(blocks, anthropicsdk.NewToolResultBlock(result.CallID, result.Output, result.IsError))
		delete(open, result.CallID)
	}
	if len(blocks) == 0 {
		return
	}
	p.history = append(p.history, anthropicsdk.NewUserMessage(blocks...))
}

func (p *Provider) closeOpenToolCallsLocked(reason string) int {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = unansweredToolOutput
	}
	open := unmatchedToolIDs(p.history)
	if len(open) == 0 {
		return 0
	}
	blocks := make([]anthropicsdk.ContentBlockParamUnion, 0, len(open))
	for callID := range open {
		blocks = append(blocks, anthropicsdk.NewToolResultBlock(callID, reason, true))
	}
	p.history = append(p.history, anthropicsdk.NewUserMessage(blocks...))
	return len(blocks)
}

func unmatchedToolIDs(history []anthropicsdk.MessageParam) map[string]bool {
	open := make(map[string]bool)
	for _, message := range history {
		for _, block := range message.Content {
			if block.OfToolUse != nil {
				if id := strings.TrimSpace(block.OfToolUse.ID); id != "" {
					open[id] = true
				}
			}
			if block.OfToolResult != nil {
				delete(open, strings.TrimSpace(block.OfToolResult.ToolUseID))
			}
		}
	}
	return open
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
	projected := cloneMessages(p.history)
	projected = append(projected, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(userText)))
	return p.overTarget(projected)
}

func (p *Provider) compactLocked(emit agent.EmitFunc) {
	original := len(p.history)
	p.history = dropOldThinking(p.history)
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

func emergencyFit(
	messages []anthropicsdk.MessageParam,
	window int,
	instructions string,
) []anthropicsdk.MessageParam {
	messages = dropAllThinking(messages)
	messages = clipAllToolOutputs(messages, emergencyToolClipBytes)
	if window > 0 {
		messages = dropOldTurns(messages, int64(window)/2, instructions)
	}
	return messages
}

func slimInput(messages []anthropicsdk.MessageParam, level, keep, clip int) []anthropicsdk.MessageParam {
	messages = dropOldThinking(cloneMessages(messages))
	if level <= 0 {
		return messages
	}
	messages = compressInputProse(messages, level)
	switch {
	case level >= 3:
		messages = dropAllThinking(messages)
		messages = clipOldToolOutputs(messages, keep, clip)
		messages = clipOldUserMessages(messages, 1, 400)
	case level >= 2:
		messages = keepLatestThinking(messages)
		messages = clipOldToolOutputs(messages, keep, clip)
	default:
		messages = clipOldToolOutputs(messages, keep, clip)
	}
	return messages
}

func compressInputProse(messages []anthropicsdk.MessageParam, level int) []anthropicsdk.MessageParam {
	lastTurn := lastUserTurnIndex(messages)
	out := cloneMessages(messages)
	for index := range out {
		if index == lastTurn {
			continue
		}
		if text := userTurnText(out[index]); text != "" {
			compressed := caveman.Compress(text, level)
			if compressed != text {
				out[index] = replaceUserTurnText(out[index], compressed)
			}
		}
		for blockIdx := range out[index].Content {
			block := out[index].Content[blockIdx]
			if block.OfToolResult == nil {
				continue
			}
			text := toolResultOutput(block)
			compressed := caveman.Compress(text, level)
			if compressed != text {
				out[index].Content[blockIdx] = replaceToolResultOutput(block, compressed)
			}
		}
	}
	return out
}

func clipOldToolOutputs(messages []anthropicsdk.MessageParam, keep, maxBytes int) []anthropicsdk.MessageParam {
	if maxBytes <= 0 {
		return messages
	}
	if keep < 0 {
		keep = 0
	}
	lastTurn := lastUserTurnIndex(messages)
	type ref struct{ msg, block int }
	indexes := make([]ref, 0, 8)
	for msgIdx, message := range messages {
		if lastTurn >= 0 && msgIdx > lastTurn {
			continue
		}
		for blockIdx, block := range message.Content {
			if block.OfToolResult != nil {
				indexes = append(indexes, ref{msgIdx, blockIdx})
			}
		}
	}
	clipCount := len(indexes) - keep
	if clipCount <= 0 {
		return messages
	}
	out := cloneMessages(messages)
	for _, index := range indexes[:clipCount] {
		block := out[index.msg].Content[index.block]
		text := toolResultOutput(block)
		if len(text) <= maxBytes {
			continue
		}
		clipped := clipBytes(text, maxBytes)
		if maxBytes > 24 {
			clipped = clipBytes(text, maxBytes-18) + "\n… [eco clipped]"
		}
		out[index.msg].Content[index.block] = replaceToolResultOutput(block, clipped)
	}
	return out
}

func clipAllToolOutputs(messages []anthropicsdk.MessageParam, maxBytes int) []anthropicsdk.MessageParam {
	if maxBytes <= 0 {
		return messages
	}
	out := cloneMessages(messages)
	for msgIdx := range out {
		for blockIdx := range out[msgIdx].Content {
			block := out[msgIdx].Content[blockIdx]
			if block.OfToolResult == nil {
				continue
			}
			text := toolResultOutput(block)
			if len(text) <= maxBytes {
				continue
			}
			clipped := clipBytes(text, maxBytes)
			if maxBytes > 24 {
				clipped = clipBytes(text, maxBytes-22) + "\n… [context clipped]"
			}
			out[msgIdx].Content[blockIdx] = replaceToolResultOutput(block, clipped)
		}
	}
	return out
}

func clipOldUserMessages(messages []anthropicsdk.MessageParam, keep, maxRunes int) []anthropicsdk.MessageParam {
	if maxRunes <= 0 {
		return messages
	}
	indexes := userTurnIndexes(messages)
	clipCount := len(indexes) - keep
	if clipCount <= 0 {
		return messages
	}
	out := cloneMessages(messages)
	for _, index := range indexes[:clipCount] {
		text := userTurnText(out[index])
		if text == "" || utf8.RuneCountInString(text) <= maxRunes {
			continue
		}
		out[index] = replaceUserTurnText(out[index], clipRunes(text, maxRunes))
	}
	return out
}

func dropAllThinking(messages []anthropicsdk.MessageParam) []anthropicsdk.MessageParam {
	return filterThinking(messages, func(int, int) bool { return false })
}

func keepLatestThinking(messages []anthropicsdk.MessageParam) []anthropicsdk.MessageParam {
	lastMsg, lastBlock := -1, -1
	for msgIdx, message := range messages {
		for blockIdx, block := range message.Content {
			if isThinkingBlock(block) {
				lastMsg, lastBlock = msgIdx, blockIdx
			}
		}
	}
	if lastMsg < 0 {
		return messages
	}
	return filterThinking(messages, func(msgIdx, blockIdx int) bool {
		return msgIdx == lastMsg && blockIdx == lastBlock
	})
}

func dropOldThinking(messages []anthropicsdk.MessageParam) []anthropicsdk.MessageParam {
	lastTurn := lastUserTurnIndex(messages)
	return filterThinking(messages, func(msgIdx, _ int) bool {
		return lastTurn >= 0 && msgIdx >= lastTurn
	})
}

func filterThinking(messages []anthropicsdk.MessageParam, keep func(msgIdx, blockIdx int) bool) []anthropicsdk.MessageParam {
	out := make([]anthropicsdk.MessageParam, 0, len(messages))
	for msgIdx, message := range messages {
		content := make([]anthropicsdk.ContentBlockParamUnion, 0, len(message.Content))
		for blockIdx, block := range message.Content {
			if isThinkingBlock(block) && !keep(msgIdx, blockIdx) {
				continue
			}
			content = append(content, block)
		}
		if len(content) == 0 && len(message.Content) > 0 {
			// Drop emptied assistant thinking-only messages.
			continue
		}
		out = append(out, anthropicsdk.MessageParam{
			Role:    message.Role,
			Content: content,
		})
	}
	return out
}

func isThinkingBlock(block anthropicsdk.ContentBlockParamUnion) bool {
	return block.OfThinking != nil || block.OfRedactedThinking != nil
}

// dropOldTurns cuts only at user-turn boundaries (messages with text, not
// pure tool_result messages) so tool_use / tool_result pairs stay adjacent.
func dropOldTurns(
	messages []anthropicsdk.MessageParam,
	target int64,
	instructions string,
) []anthropicsdk.MessageParam {
	users := userTurnIndexes(messages)
	if len(users) <= 1 {
		return messages
	}

	best := messages
	for _, start := range users[1:] {
		notice := anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(summarizeDropped(messages[:start])))
		candidate := append([]anthropicsdk.MessageParam{notice}, messages[start:]...)
		best = candidate
		if historyTokens(candidate, instructions) <= target {
			return candidate
		}
	}
	return best
}

func summarizeDropped(messages []anthropicsdk.MessageParam) string {
	var users []string
	var tools []string
	seenTool := make(map[string]bool)
	var errors []string
	for _, message := range messages {
		if text := userTurnText(message); text != "" {
			if strings.HasPrefix(text, compactNotice) {
				continue
			}
			users = append(users, clipRunes(text, compactSummaryClipRunes))
		}
		for _, block := range message.Content {
			if name := toolUseName(block); name != "" {
				if !seenTool[name] {
					seenTool[name] = true
					tools = append(tools, name)
				}
				continue
			}
			if output := toolResultOutput(block); strings.HasPrefix(output, "error:") {
				errors = append(errors, clipRunes(output, compactSummaryClipRunes))
			}
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

func userTurnIndexes(messages []anthropicsdk.MessageParam) []int {
	indexes := make([]int, 0, 8)
	for index, message := range messages {
		if isUserTurn(message) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func lastUserTurnIndex(messages []anthropicsdk.MessageParam) int {
	indexes := userTurnIndexes(messages)
	if len(indexes) == 0 {
		return -1
	}
	return indexes[len(indexes)-1]
}

// isUserTurn is true for user messages that start a conversational turn
// (contain text). Pure tool_result user messages are not turn boundaries so
// compaction never separates tool_use from its adjacent tool_result.
func isUserTurn(message anthropicsdk.MessageParam) bool {
	if message.Role != anthropicsdk.MessageParamRoleUser {
		return false
	}
	return userTurnText(message) != ""
}

func userTurnText(message anthropicsdk.MessageParam) string {
	if message.Role != anthropicsdk.MessageParamRoleUser {
		return ""
	}
	var parts []string
	for _, block := range message.Content {
		if block.OfText != nil {
			if text := strings.TrimSpace(block.OfText.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func replaceUserTurnText(message anthropicsdk.MessageParam, text string) anthropicsdk.MessageParam {
	content := make([]anthropicsdk.ContentBlockParamUnion, 0, len(message.Content))
	replaced := false
	for _, block := range message.Content {
		if block.OfText != nil && !replaced {
			content = append(content, anthropicsdk.NewTextBlock(text))
			replaced = true
			continue
		}
		if block.OfText != nil {
			continue
		}
		content = append(content, block)
	}
	if !replaced {
		content = append([]anthropicsdk.ContentBlockParamUnion{anthropicsdk.NewTextBlock(text)}, content...)
	}
	return anthropicsdk.MessageParam{Role: message.Role, Content: content}
}

func toolUseName(block anthropicsdk.ContentBlockParamUnion) string {
	if block.OfToolUse == nil {
		return ""
	}
	return strings.TrimSpace(block.OfToolUse.Name)
}

func toolResultOutput(block anthropicsdk.ContentBlockParamUnion) string {
	if block.OfToolResult == nil {
		return ""
	}
	var parts []string
	for _, content := range block.OfToolResult.Content {
		if content.OfText != nil {
			parts = append(parts, content.OfText.Text)
		}
	}
	return strings.Join(parts, "")
}

func replaceToolResultOutput(block anthropicsdk.ContentBlockParamUnion, output string) anthropicsdk.ContentBlockParamUnion {
	if block.OfToolResult == nil {
		return block
	}
	isError := block.OfToolResult.IsError.Or(false)
	return anthropicsdk.NewToolResultBlock(block.OfToolResult.ToolUseID, output, isError)
}

func cloneMessages(messages []anthropicsdk.MessageParam) []anthropicsdk.MessageParam {
	if len(messages) == 0 {
		return nil
	}
	data, err := json.Marshal(messages)
	if err != nil {
		out := make([]anthropicsdk.MessageParam, len(messages))
		copy(out, messages)
		return out
	}
	var out []anthropicsdk.MessageParam
	if err := json.Unmarshal(data, &out); err != nil {
		out = make([]anthropicsdk.MessageParam, len(messages))
		copy(out, messages)
		return out
	}
	return out
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

func toolParams(definitions []agent.ToolDefinition, eco int) []anthropicsdk.ToolUnionParam {
	if len(definitions) == 0 {
		return nil
	}
	params := make([]anthropicsdk.ToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		description := definition.Description
		parameters := definition.Parameters
		if eco > 0 {
			description = caveman.Compress(description, eco)
			if compressed, ok := caveman.CompressDescriptions(cloneJSON(parameters), eco).(map[string]any); ok {
				parameters = compressed
			}
		}
		tool := anthropicsdk.ToolUnionParamOfTool(toolInputSchema(parameters), definition.Name)
		if tool.OfTool != nil && description != "" {
			tool.OfTool.Description = anthropicsdk.String(description)
		}
		params = append(params, tool)
	}
	return params
}

func toolInputSchema(parameters map[string]any) anthropicsdk.ToolInputSchemaParam {
	schema := anthropicsdk.ToolInputSchemaParam{}
	if parameters == nil {
		return schema
	}
	if props, ok := parameters["properties"]; ok {
		schema.Properties = props
	}
	switch required := parameters["required"].(type) {
	case []string:
		schema.Required = required
	case []any:
		for _, item := range required {
			if value, ok := item.(string); ok {
				schema.Required = append(schema.Required, value)
			}
		}
	}
	return schema
}

func cloneJSON(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if json.Unmarshal(data, &cloned) != nil {
		return value
	}
	return cloned
}

func assistantToolCalls(message anthropicsdk.Message) []agent.ToolCall {
	var calls []agent.ToolCall
	for _, block := range message.Content {
		if block.Type != "tool_use" {
			continue
		}
		tool := block.AsToolUse()
		arguments, err := json.Marshal(tool.Input)
		if err != nil {
			arguments = []byte("{}")
		}
		calls = append(calls, agent.ToolCall{
			ID:        tool.ID,
			Name:      tool.Name,
			Arguments: arguments,
		})
	}
	return calls
}

func assistantText(message anthropicsdk.Message) string {
	var parts []string
	for _, block := range message.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "")
}

// toolPairsIntact is true when no tool_result appears without a prior tool_use
// in the slice. Open tool_use at the end (mid tool-loop) is allowed.
func toolPairsIntact(messages []anthropicsdk.MessageParam) bool {
	open := make(map[string]bool)
	for _, message := range messages {
		for _, block := range message.Content {
			if block.OfToolUse != nil {
				if id := strings.TrimSpace(block.OfToolUse.ID); id != "" {
					open[id] = true
				}
			}
			if block.OfToolResult != nil {
				id := strings.TrimSpace(block.OfToolResult.ToolUseID)
				if !open[id] {
					return false
				}
				delete(open, id)
			}
		}
	}
	return true
}
