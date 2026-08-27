package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	specs            map[string]toolSpec
	plan             atomic.Bool
}

func NewRegistry(ws *workspace.Workspace, approver approval.Approver, options Options) *Registry {
	r := &Registry{
		workspace:        ws,
		approver:         approver,
		maxResultBytes:   max(options.MaxResultBytes, 1024),
		maxSearchResults: max(options.MaxSearchResult, 1),
		parallelReads:    max(options.ParallelReads, 1),
		commandTimeout:   options.CommandTimeout,
		specs:            make(map[string]toolSpec),
	}
	for _, spec := range []toolSpec{
		r.listFilesSpec(),
		r.searchFilesSpec(),
		r.readFileSpec(),
		r.applyPatchSpec(),
		r.runCommandSpec(),
	} {
		r.specs[spec.definition.Name] = spec
	}
	return r
}

func (r *Registry) SetPlan(plan bool) {
	r.plan.Store(plan)
}

func (r *Registry) Definitions() []agent.ToolDefinition {
	order := []string{
		"list_files",
		"search_files",
		"read_file",
		"apply_patch",
		"run_command",
	}
	plan := r.plan.Load()
	definitions := make([]agent.ToolDefinition, 0, len(order))
	for _, name := range order {
		spec := r.specs[name]
		if plan && !spec.definition.ReadOnly {
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
	if r.plan.Load() && !spec.definition.ReadOnly {
		result := errorResult(
			call,
			fmt.Errorf("plan mode: writes and commands are disabled; press Shift+Tab to return to agent mode"),
			0,
		)
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
			result := errorResult(call, err, 0)
			agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
			return result
		}
		approved, err := r.approver.Approve(ctx, action)
		if err != nil {
			result := errorResult(call, fmt.Errorf("approval failed: %w", err), 0)
			agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
			return result
		}
		if !approved {
			result := errorResult(call, fmt.Errorf("permission denied by user"), 0)
			agent.Emit(emit, agent.Event{Kind: agent.EventToolDone, Result: &result})
			return result
		}
	}

	started := time.Now()
	agent.Emit(emit, agent.Event{Kind: agent.EventToolStarted, ToolCall: &call})

	output, err := runner(ctx)
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
	return value[:keep] + marker, true
}

func sanitize(value string) string {
	value = strings.ReplaceAll(value, "\x00", "�")
	return strings.TrimSpace(value)
}
