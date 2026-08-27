package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeModel struct {
	responses       []ModelResponse
	inputs          []ModelInput
	definitionCount []int
	resetCount      int
}

func (m *fakeModel) Respond(
	_ context.Context,
	input ModelInput,
	definitions []ToolDefinition,
	_ EmitFunc,
) (ModelResponse, error) {
	m.inputs = append(m.inputs, input)
	m.definitionCount = append(m.definitionCount, len(definitions))
	if len(m.responses) == 0 {
		return ModelResponse{}, errors.New("unexpected model call")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func (m *fakeModel) Reset() {
	m.resetCount++
}

type fakeExecutor struct {
	definitions []ToolDefinition
	results     []ToolResult
	calls       [][]ToolCall
}

func (e *fakeExecutor) Definitions() []ToolDefinition {
	return e.definitions
}

func (e *fakeExecutor) Execute(
	_ context.Context,
	calls []ToolCall,
	_ EmitFunc,
) []ToolResult {
	e.calls = append(e.calls, calls)
	return e.results
}

func TestLoopExecutesToolsAndReturnsFinalAnswer(t *testing.T) {
	call := ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.md"}`)}
	model := &fakeModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{call}, Usage: Usage{TotalTokens: 10}},
		{Text: "Done.", Usage: Usage{TotalTokens: 5}},
	}}
	executor := &fakeExecutor{
		definitions: []ToolDefinition{{Name: "read_file", ReadOnly: true}},
		results:     []ToolResult{{CallID: "call-1", Name: "read_file", Output: "contents"}},
	}
	loop := &Loop{Model: model, Executor: executor, MaxSteps: 3}

	result, err := loop.Run(context.Background(), "Inspect the README", nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Answer != "Done." {
		t.Fatalf("Answer = %q, want Done.", result.Answer)
	}
	if result.Steps != 2 || result.Usage.TotalTokens != 15 {
		t.Fatalf("result = %+v", result)
	}
	if len(executor.calls) != 1 || len(executor.calls[0]) != 1 {
		t.Fatalf("executor calls = %#v", executor.calls)
	}
	if len(model.inputs) != 2 || model.inputs[0].UserText == "" || len(model.inputs[1].ToolResults) != 1 {
		t.Fatalf("model inputs = %#v", model.inputs)
	}
}

func TestLoopReservesLastStepForToolFreeAnswer(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{}`)}}},
		{Text: "Final"},
	}}
	executor := &fakeExecutor{
		definitions: []ToolDefinition{{Name: "read_file", ReadOnly: true}},
		results:     []ToolResult{{CallID: "call-1", Name: "read_file", Output: "ok"}},
	}
	loop := &Loop{Model: model, Executor: executor, MaxSteps: 2}

	if _, err := loop.Run(context.Background(), "question", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := model.definitionCount; len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("definition counts = %v, want [1 0]", got)
	}
}

func TestLoopStopsIfModelCallsToolOnFinalStep(t *testing.T) {
	model := &fakeModel{responses: []ModelResponse{{
		ToolCalls: []ToolCall{{ID: "call-1", Name: "unexpected", Arguments: json.RawMessage(`{}`)}},
	}}}
	executor := &fakeExecutor{definitions: []ToolDefinition{{Name: "unexpected"}}}
	loop := &Loop{Model: model, Executor: executor, MaxSteps: 1}

	_, err := loop.Run(context.Background(), "question", nil)
	if !errors.Is(err, ErrMaxSteps) {
		t.Fatalf("Run() error = %v, want ErrMaxSteps", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor was called on final step: %#v", executor.calls)
	}
}

func TestLoopResetDelegatesToModel(t *testing.T) {
	model := &fakeModel{}
	loop := &Loop{Model: model}
	loop.Reset()
	if model.resetCount != 1 {
		t.Fatalf("reset count = %d, want 1", model.resetCount)
	}
}

func TestLoopHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := &fakeModel{responses: []ModelResponse{{Text: "unused"}}}
	loop := &Loop{Model: model, Executor: &fakeExecutor{}, MaxSteps: 2}

	_, err := loop.Run(ctx, "question", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(model.inputs) != 0 {
		t.Fatalf("model was called after cancellation: %#v", model.inputs)
	}
}
