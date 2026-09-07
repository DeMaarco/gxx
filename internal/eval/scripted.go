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

package eval

import (
	"context"
	"encoding/json"
	"errors"

	"gxx/internal/agent"
	"gxx/internal/budget"
)

// ScriptedBackend exercises orchestration, tools and graders without an API.
// It is not an evaluation of model quality and never invents model usage.
type ScriptedBackend struct {
	Responses    []agent.ModelResponse
	Instructions string
	Inputs       []agent.ModelInput
	index        int
}

func (s *ScriptedBackend) Respond(ctx context.Context, input agent.ModelInput, _ []agent.ToolDefinition, _ agent.EmitFunc) (agent.ModelResponse, error) {
	if err := ctx.Err(); err != nil {
		return agent.ModelResponse{}, err
	}
	s.Inputs = append(s.Inputs, input)
	if s.index >= len(s.Responses) {
		return agent.ModelResponse{}, errors.New("offline script exhausted")
	}
	r := s.Responses[s.index]
	s.index++
	return r, nil
}
func (s *ScriptedBackend) Reset()                                     { s.index = 0; s.Inputs = nil }
func (*ScriptedBackend) AbsorbToolResults([]agent.ToolResult)         {}
func (*ScriptedBackend) CloseOpenToolCalls(string)                    {}
func (*ScriptedBackend) SetModel(string)                              {}
func (*ScriptedBackend) SetEffort(string)                             {}
func (*ScriptedBackend) SetContext(string)                            {}
func (*ScriptedBackend) SetFast(bool)                                 {}
func (*ScriptedBackend) SetTokenBudget(int, int, int, int, int, bool) {}
func (s *ScriptedBackend) SetInstructions(v string)                   { s.Instructions = v }
func (*ScriptedBackend) Report(context.Context) agent.UsageReport     { return agent.UsageReport{} }
func (*ScriptedBackend) ContextSnapshot() agent.ContextUsage          { return agent.ContextUsage{} }
func (*ScriptedBackend) ExportHistory() (string, json.RawMessage, error) {
	return "simulation", nil, nil
}
func (*ScriptedBackend) ImportHistory(string, json.RawMessage) error {
	return errors.New("simulation does not import provider history")
}
func (*ScriptedBackend) Compact(ctx context.Context, emit agent.EmitFunc, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	agent.Emit(emit, agent.Event{Kind: agent.EventNotice, Text: budget.CompactNotice + " Simulation only."})
	return nil
}
