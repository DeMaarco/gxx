package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrMaxSteps = errors.New("agent reached the maximum number of steps")

// Loop coordinates model responses and local tool execution.
type Loop struct {
	Model    Model
	Executor Executor
	MaxSteps int
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

	input := ModelInput{UserText: prompt}
	definitions := l.Executor.Definitions()

	for step := 1; step <= l.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		availableTools := definitions
		if step == l.MaxSteps {
			// Reserve the final model step for a tool-free answer so every
			// executed function call has a matching output in provider history.
			availableTools = nil
		}
		response, err := l.Model.Respond(ctx, input, availableTools, emit)
		if err != nil {
			return result, fmt.Errorf("model response: %w", err)
		}
		result.Steps = step
		result.Usage.Add(response.Usage)

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

		toolResults := l.Executor.Execute(ctx, response.ToolCalls, emit)
		result.ToolResults = append(result.ToolResults, toolResults...)
		if err := ctx.Err(); err != nil {
			l.Model.AbsorbToolResults(toolResults)
			return result, err
		}
		input = ModelInput{ToolResults: toolResults}
	}

	return result, ErrMaxSteps
}

func (l *Loop) Reset() {
	if l.Model != nil {
		l.Model.Reset()
	}
}
