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
	"context"
	"encoding/json"
)

// Backend is a provider conversation plus the session knobs the CLI runtime
// already uses for OpenAI. Anthropic implements the same surface.
type Backend interface {
	Model
	SetModel(string)
	SetEffort(string)
	SetContext(string)
	SetFast(bool)
	SetTokenBudget(ecoLevel, compactNumer, compactDenom, toolKeep, toolClip int, includeReasoning bool)
	SetInstructions(string)
	Report(context.Context) UsageReport
	ContextSnapshot() ContextUsage
	ExportHistory() (provider string, history json.RawMessage, err error)
	ImportHistory(provider string, history json.RawMessage) error
}
