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

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/agent"
	"gxx/internal/caveman"
)

const unansweredToolOutput = "error: tool call was not executed"

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

func (p *Provider) compactLocked() {
	if len(p.history) < 2 {
		return
	}
	// Drop the oldest user/assistant pair while staying on a user turn.
	if p.history[0].Role != anthropicsdk.MessageParamRoleUser {
		p.history = p.history[1:]
		return
	}
	drop := 1
	if len(p.history) > 1 && p.history[1].Role == anthropicsdk.MessageParamRoleAssistant {
		drop = 2
	}
	p.history = p.history[drop:]
}

func (p *Provider) slimHistoryLocked() []anthropicsdk.MessageParam {
	staged := append([]anthropicsdk.MessageParam(nil), p.history...)
	if p.ecoToolClip <= 0 && p.ecoLevel <= 0 {
		return staged
	}
	for i := range staged {
		for j := range staged[i].Content {
			if staged[i].Content[j].OfToolResult == nil {
				continue
			}
			if p.ecoToolClip <= 0 {
				continue
			}
			clipToolResult(&staged[i].Content[j], p.ecoToolClip)
		}
	}
	return staged
}

func clipToolResult(block *anthropicsdk.ContentBlockParamUnion, maxBytes int) {
	if block == nil || block.OfToolResult == nil || maxBytes <= 0 {
		return
	}
	for i := range block.OfToolResult.Content {
		text := block.OfToolResult.Content[i].OfText
		if text == nil || len(text.Text) <= maxBytes {
			continue
		}
		text.Text = text.Text[:maxBytes] + "\n…truncated…"
	}
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
