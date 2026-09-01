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

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/workspace"
)

const maxToolCallsPerBatch = 32

type Options struct {
	MaxResultBytes  int
	MaxSearchResult int
	ParallelReads   int
	CommandTimeout  time.Duration
	ImageTimeout    time.Duration
	GenerateImage   func(context.Context, ImageRequest) (ImageResult, error)
}

type toolSpec struct {
	definition agent.ToolDefinition
	preview    func(json.RawMessage) (approval.Action, error)
	run        func(context.Context, json.RawMessage) (string, error)
	prepare    func(json.RawMessage) (approval.Action, toolRun, error)
}

type toolRun func(context.Context) (string, error)

type Registry struct {
	workspace        *workspace.Workspace
	approver         approval.Approver
	maxResultBytes   int
	maxSearchResults int
	parallelReads    int
	commandTimeout   time.Duration
	imageTimeout     time.Duration
	generateImage    func(context.Context, ImageRequest) (ImageResult, error)
	specs            map[string]toolSpec
	plan             atomic.Bool
	ask              atomic.Bool
}

func NewRegistry(ws *workspace.Workspace, approver approval.Approver, options Options) *Registry {
	r := &Registry{
		workspace:        ws,
		approver:         approver,
		maxResultBytes:   max(options.MaxResultBytes, 1024),
		maxSearchResults: max(options.MaxSearchResult, 1),
		parallelReads:    max(options.ParallelReads, 1),
		commandTimeout:   options.CommandTimeout,
		imageTimeout:     options.ImageTimeout,
		generateImage:    options.GenerateImage,
		specs:            make(map[string]toolSpec),
	}
	for _, spec := range []toolSpec{
		r.listFilesSpec(),
		r.searchFilesSpec(),
		r.readFileSpec(),
		r.gitStatusSpec(),
		r.gitDiffSpec(),
		r.gitLogSpec(),
		r.applyPatchSpec(),
		r.generateImageSpec(),
		r.runCommandSpec(),
	} {
		r.specs[spec.definition.Name] = spec
	}
	return r
}

func (r *Registry) SetPlan(plan bool) {
	r.plan.Store(plan)
	if plan {
		r.ask.Store(false)
	}
}

func (r *Registry) Plan() bool {
	return r.plan.Load()
}

// SetAsk enables ask mode: only read-only tools run. Ask and plan are exclusive.
func (r *Registry) SetAsk(ask bool) {
	r.ask.Store(ask)
	if ask {
		r.plan.Store(false)
	}
}

func (r *Registry) Ask() bool {
	return r.ask.Load()
}

func (r *Registry) readOnlySession() bool {
	return r.plan.Load() || r.ask.Load()
}

func (r *Registry) SetMaxResultBytes(n int) {
	r.maxResultBytes = max(n, 1024)
}

func (r *Registry) SetGenerateImage(fn func(context.Context, ImageRequest) (ImageResult, error)) {
	r.generateImage = fn
}

func (r *Registry) Definitions() []agent.ToolDefinition {
	order := []string{
		"list_files",
		"search_files",
		"read_file",
		"git_status",
		"git_diff",
		"git_log",
		"apply_patch",
		"generate_image",
		"run_command",
	}
	readOnly := r.readOnlySession()
	hasGit := r.workspace != nil && r.workspace.HasGit()
	definitions := make([]agent.ToolDefinition, 0, len(order))
	for _, name := range order {
		spec := r.specs[name]
		if readOnly && !spec.definition.ReadOnly {
			continue
		}
		if !hasGit && (name == "git_status" || name == "git_diff" || name == "git_log") {
			continue
		}
		definitions = append(definitions, spec.definition)
	}
	return definitions
}

// Execute runs consecutive read-only calls concurrently while keeping all
// mutations and commands serialized relative to reads.
func (r *Registry) Execute(ctx context.Context, calls []agent.ToolCall, emit agent.EmitFunc) []agent.ToolResult {
	results := make([]agent.ToolResult, len(calls))
	executionLimit := min(len(calls), maxToolCallsPerBatch)

	for start := 0; start < executionLimit; {
		spec, exists := r.specs[calls[start].Name]
		if !exists || !spec.definition.ReadOnly {
			results[start] = r.executeOne(ctx, calls[start], emit)
			start++
			continue
		}

		end := start
		for end < executionLimit {
			next, ok := r.specs[calls[end].Name]
			if !ok || !next.definition.ReadOnly {
				break
			}
			end++
		}
		r.executeReadBatch(ctx, calls[start:end], results[start:end], emit)
		start = end
	}
	for index := executionLimit; index < len(calls); index++ {
		results[index] = errorResult(
			calls[index],
			fmt.Errorf("tool call limit exceeded; maximum is %d per model step", maxToolCallsPerBatch),
			0,
		)
		result := results[index]
		agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
	}

	return results
}

func (r *Registry) executeReadBatch(
	ctx context.Context,
	calls []agent.ToolCall,
	results []agent.ToolResult,
	emit agent.EmitFunc,
) {
	limit := make(chan struct{}, r.parallelReads)
	var group sync.WaitGroup
	for i := range calls {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				results[index] = errorResult(calls[index], ctx.Err(), 0)
				return
			}
			results[index] = r.executeOne(ctx, calls[index], emit)
		}(i)
	}
	group.Wait()
}

func (r *Registry) executeOne(ctx context.Context, call agent.ToolCall, emit agent.EmitFunc) agent.ToolResult {
	spec, exists := r.specs[call.Name]
	if !exists {
		result := errorResult(call, fmt.Errorf("unknown tool %q", call.Name), 0)
		agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
		return result
	}
	if r.readOnlySession() && !spec.definition.ReadOnly {
		message := "ask mode is read-only; press Shift+Tab to switch to plan or agent"
		if r.plan.Load() {
			message = "plan mode: writes and commands are disabled; press Shift+Tab to switch to agent"
		}
		result := errorResult(call, fmt.Errorf("%s", message), 0)
		agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
		return result
	}

	runner := func(runContext context.Context) (string, error) {
		return spec.run(runContext, call.Arguments)
	}
	if !spec.definition.ReadOnly {
		if r.approver == nil {
			result := errorResult(call, fmt.Errorf("permission denied: no interactive approver"), 0)
			agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
			return result
		}
		var action approval.Action
		var err error
		if spec.prepare != nil {
			action, runner, err = spec.prepare(call.Arguments)
		} else {
			action, err = spec.preview(call.Arguments)
		}
		if err != nil {
			return emitFailedTool(emit, call, err)
		}
		decision, err := r.approver.Approve(ctx, action)
		if err != nil {
			return emitFailedTool(emit, call, fmt.Errorf("approval failed: %w", err))
		}
		if !decision.Approved {
			return emitFailedTool(emit, call, fmt.Errorf("permission denied by user"))
		}
	}

	started := time.Now()
	agent.Emit(emit, agent.Event{Kind: agent.EventToolStarted, ToolCall: &call})

	output, err := runner(withToolContext(ctx, emit, call))
	duration := time.Since(started)
	if err != nil {
		if strings.TrimSpace(output) != "" {
			err = fmt.Errorf("%s\n%w", strings.TrimSpace(output), err)
		}
		result := errorResult(call, err, duration)
		result.Output, result.Truncated = truncate(result.Output, r.maxResultBytes)
		agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result, Truncated: result.Truncated})
		return result
	}

	output = sanitize(output)
	output, truncated := truncate(output, r.maxResultBytes)
	result := agent.ToolResult{
		CallID:     call.ID,
		Name:       call.Name,
		Output:     output,
		Duration:   duration,
		DurationMS: duration.Milliseconds(),
		Truncated:  truncated,
	}
	agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result, Truncated: truncated})
	return result
}

func emitFailedTool(emit agent.EmitFunc, call agent.ToolCall, err error) agent.ToolResult {
	result := errorResult(call, err, 0)
	agent.Emit(emit, agent.Event{Kind: agent.EventToolStarted, ToolCall: &call})
	agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
	return result
}

func errorResult(call agent.ToolCall, err error, duration time.Duration) agent.ToolResult {
	message := "tool failed"
	if err != nil {
		message = "error: " + err.Error()
	}
	return agent.ToolResult{
		CallID:     call.ID,
		Name:       call.Name,
		Output:     sanitize(message),
		IsError:    true,
		Duration:   duration,
		DurationMS: duration.Milliseconds(),
	}
}

func truncate(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	const marker = "\n… output truncated by gxx"
	keep := max(limit-len(marker), 0)
	return cutAtRune(value, keep) + marker, true
}

// cutAtRune trims value to at most limit bytes, backing up to a character
// boundary. Cutting mid-rune leaves the model reading a replacement character
// where a source file had an accent.
func cutAtRune(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func sanitize(value string) string {
	value = strings.ReplaceAll(value, "\x00", "�")
	return strings.TrimSpace(value)
}
