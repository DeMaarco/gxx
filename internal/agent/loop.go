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

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrMaxSteps = errors.New("agent reached the maximum number of steps")

const (
	repeatedToolNotice    = "Repeated call with the same arguments; previous result still applies.\n"
	toolLoopFailThreshold = 3
	toolLoopNotice        = "Same tool call failed repeatedly with the same error; the previous result still applies — change arguments or approach."
)

// Loop coordinates model responses and local tool execution.
type Loop struct {
	Model    Model
	Executor Executor
	MaxSteps int
	// Overview, if set, returns a cheap workspace snapshot prepended to each
	// user turn so the model does not need to list the root first.
	Overview func(context.Context) string
	// ProjectContext, if set, returns AGENTS.md quoted data prepended after
	// Overview and before SkillsContext / the user's text on each turn.
	ProjectContext func() string
	// SkillsContext, if set, returns the skill catalog prepended after
	// Overview and ProjectContext and before the user's text on each turn.
	SkillsContext func() string
}

func (l *Loop) Run(ctx context.Context, prompt string, emit EmitFunc) (Result, error) {
	var result Result

	if l.Model == nil {
		return result, errors.New("agent model is nil")
	}
	if l.Executor == nil {
		return result, errors.New("tool executor is nil")
	}
	if l.MaxSteps < 1 {
		return result, errors.New("max steps must be at least 1")
	}
	if strings.TrimSpace(prompt) == "" {
		return result, errors.New("prompt cannot be empty")
	}
	parts := make([]string, 0, 4)
	if l.Overview != nil {
		if snap := strings.TrimSpace(l.Overview(ctx)); snap != "" {
			parts = append(parts, snap)
		}
	}
	if l.ProjectContext != nil {
		if project := strings.TrimSpace(l.ProjectContext()); project != "" {
			parts = append(parts, project)
		}
	}
	if l.SkillsContext != nil {
		if catalog := strings.TrimSpace(l.SkillsContext()); catalog != "" {
			parts = append(parts, catalog)
		}
	}
	parts = append(parts, prompt)
	prompt = strings.Join(parts, "\n\n")

	input := ModelInput{UserText: prompt}
	definitions := l.Executor.Definitions()
	readOnly := readOnlyToolSet(definitions)
	cache := make(map[string]ToolResult)
	var streak failStreak

	for step := 1; step <= l.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		// Reserve the final model step for a tool-free answer so every
		// executed function call has a matching output in provider history.
		input.FinalStep = step == l.MaxSteps
		response, err := l.Model.Respond(ctx, input, definitions, emit)
		if err != nil {
			return result, fmt.Errorf("model response: %w", err)
		}
		result.Steps = step
		result.Usage.Add(response.Usage)
		Emit(emit, Event{Kind: EventUsage, Usage: result.Usage, Step: step})

		if len(response.ToolCalls) == 0 {
			result.Answer = response.Text
			return result, nil
		}
		if step == l.MaxSteps {
			l.Model.CloseOpenToolCalls(
				"error: tool call was not executed because the agent reached the maximum number of steps",
			)
			return result, ErrMaxSteps
		}

		for i := range response.ToolCalls {
			call := response.ToolCalls[i]
			Emit(emit, Event{Kind: EventToolCall, ToolCall: &call, Step: step})
		}

		toolResults := l.executeTools(ctx, response.ToolCalls, emit, cache, readOnly, &streak)
		result.ToolResults = append(result.ToolResults, toolResults...)
		if err := ctx.Err(); err != nil {
			l.Model.AbsorbToolResults(toolResults)
			return result, err
		}
		input = ModelInput{ToolResults: toolResults}
	}

	return result, ErrMaxSteps
}

func (l *Loop) executeTools(
	ctx context.Context,
	calls []ToolCall,
	emit EmitFunc,
	cache map[string]ToolResult,
	readOnly map[string]bool,
	streak *failStreak,
) []ToolResult {
	if len(calls) == 0 {
		return nil
	}

	unique := make([]ToolCall, 0, len(calls))
	seen := make(map[string]bool, len(calls))
	keys := make([]string, len(calls))
	cached := make([]bool, len(calls))

	for i, call := range calls {
		key := toolCallKey(call)
		keys[i] = key
		if readOnly[call.Name] {
			if _, ok := cache[key]; ok {
				cached[i] = true
				continue
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, call)
	}

	executedByID := make(map[string]ToolResult, len(unique))
	batchByKey := make(map[string]ToolResult, len(unique))
	if len(unique) > 0 {
		executed := l.Executor.Execute(ctx, unique, emit)
		for _, result := range executed {
			if result.CallID != "" {
				executedByID[result.CallID] = result
			}
		}
		for i, call := range unique {
			result, ok := executedByID[call.ID]
			if !ok && i < len(executed) {
				result = executed[i]
				if result.CallID == "" {
					result.CallID = call.ID
				}
				ok = true
			}
			if !ok {
				result = ToolResult{
					CallID:  call.ID,
					Name:    call.Name,
					Output:  "error: tool produced no result",
					IsError: true,
				}
			}
			executedByID[call.ID] = result
			key := toolCallKey(call)
			batchByKey[key] = result
			if readOnly[call.Name] {
				cache[key] = result
			}
		}
	}

	results := make([]ToolResult, 0, len(calls))
	for i, call := range calls {
		var result ToolResult
		switch {
		case cached[i]:
			result = reusedToolResult(call, cache[keys[i]])
			emitReusedTool(emit, call, result)
		default:
			if got, ok := executedByID[call.ID]; ok {
				result = got
			} else if prev, ok := batchByKey[keys[i]]; ok {
				result = reusedToolResult(call, prev)
				emitReusedTool(emit, call, result)
			} else if prev, ok := cache[keys[i]]; ok {
				result = reusedToolResult(call, prev)
				emitReusedTool(emit, call, result)
			} else {
				result = ToolResult{
					CallID:  call.ID,
					Name:    call.Name,
					Output:  "error: tool produced no result",
					IsError: true,
				}
			}
		}
		results = append(results, result)
		if streak != nil {
			streak.observe(keys[i], result, emit)
		}
	}
	if mutatedWorkspace(unique, readOnly) {
		clear(cache)
	}
	return results
}

type failStreak struct {
	key     string
	output  string
	count   int
	noticed bool
}

func (s *failStreak) observe(key string, result ToolResult, emit EmitFunc) {
	if s == nil {
		return
	}
	if !result.IsError {
		s.key = ""
		s.output = ""
		s.count = 0
		return
	}
	out := strings.TrimPrefix(result.Output, repeatedToolNotice)
	if key == s.key && sameFailOutput(out, s.output) {
		s.count++
	} else {
		s.key = key
		s.output = out
		s.count = 1
	}
	if s.count >= toolLoopFailThreshold && !s.noticed {
		s.noticed = true
		Emit(emit, Event{Kind: EventNotice, Text: toolLoopNotice})
	}
}

func sameFailOutput(a, b string) bool {
	if a == b {
		return true
	}
	const n = 200
	if len(a) > n {
		a = a[:n]
	}
	if len(b) > n {
		b = b[:n]
	}
	return a == b
}

func readOnlyToolSet(definitions []ToolDefinition) map[string]bool {
	out := make(map[string]bool, len(definitions))
	for _, def := range definitions {
		if def.ReadOnly {
			out[def.Name] = true
		}
	}
	return out
}

func mutatedWorkspace(calls []ToolCall, readOnly map[string]bool) bool {
	for _, call := range calls {
		if !readOnly[call.Name] {
			return true
		}
	}
	return false
}

func emitReusedTool(emit EmitFunc, call ToolCall, result ToolResult) {
	Emit(emit, Event{Kind: EventToolStarted, ToolCall: &call})
	Emit(emit, Event{Kind: EventToolDone, Result: &result, Truncated: result.Truncated})
}

func reusedToolResult(call ToolCall, prev ToolResult) ToolResult {
	return ToolResult{
		CallID:    call.ID,
		Name:      call.Name,
		Output:    repeatedToolNotice + prev.Output,
		IsError:   prev.IsError,
		Truncated: prev.Truncated,
	}
}

func toolCallKey(call ToolCall) string {
	return call.Name + "\n" + compactJSON(call.Arguments)
}

func compactJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return string(trimmed)
	}
	return buf.String()
}

func (l *Loop) Reset() {
	if l.Model != nil {
		l.Model.Reset()
	}
}
