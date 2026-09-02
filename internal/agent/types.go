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
	"time"
)

type EventKind string

const (
	EventTextDelta    EventKind = "text_delta"
	EventToolCall     EventKind = "tool_call"
	EventToolStarted  EventKind = "tool_started"
	EventToolProgress EventKind = "tool_progress"
	EventToolDone     EventKind = "tool_done"
	EventNotice       EventKind = "notice"
	EventUsage        EventKind = "usage"
)

// Event is a provider-agnostic progress update for terminal renderers.
type Event struct {
	Kind      EventKind
	Text      string
	ToolCall  *ToolCall
	Result    *ToolResult
	Usage     Usage
	Step      int
	Truncated bool
}

type EmitFunc func(Event)

func Emit(emit EmitFunc, event Event) {
	if emit != nil {
		emit(event)
	}
}

// ToolDefinition is the JSON-schema contract exposed to the model.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
	ReadOnly    bool
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	CallID     string        `json:"call_id"`
	Name       string        `json:"name"`
	Output     string        `json:"output"`
	IsError    bool          `json:"is_error"`
	Duration   time.Duration `json:"-"`
	DurationMS int64         `json:"duration_ms"`
	Truncated  bool          `json:"truncated"`
}

// Usage is provider-normalized token accounting for a turn or session.
// InputTokens is always the total input billable/processed (including cached
// and cache-write tokens). CachedTokens and CacheWriteTokens are subsets of
// that total when the provider reports them separately.
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.ReasoningTokens += other.ReasoningTokens
	u.CachedTokens += other.CachedTokens
	u.CacheWriteTokens += other.CacheWriteTokens
	u.TotalTokens += other.TotalTokens
}

// RateLimit is the remaining Requests API quota from the last OpenAI response.
type RateLimit struct {
	RequestsLimit     int64  `json:"requests_limit,omitempty"`
	RequestsRemaining int64  `json:"requests_remaining,omitempty"`
	RequestsReset     string `json:"requests_reset,omitempty"`
	TokensLimit       int64  `json:"tokens_limit,omitempty"`
	TokensRemaining   int64  `json:"tokens_remaining,omitempty"`
	TokensReset       string `json:"tokens_reset,omitempty"`
	Known             bool   `json:"known"`
}

// AccountUsage is organization spend and token usage for the current month.
type AccountUsage struct {
	PeriodStart  time.Time     `json:"period_start"`
	Requests     int64         `json:"requests"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	SpendUSD     float64       `json:"spend_usd"`
	HasSpend     bool          `json:"has_spend"`
	LimitUSD     float64       `json:"limit_usd"`
	HasLimit     bool          `json:"has_limit"`
	RemainingUSD float64       `json:"remaining_usd"`
	HasRemaining bool          `json:"has_remaining"`
	Plan         string        `json:"plan,omitempty"`
	Windows      []QuotaWindow `json:"windows,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// QuotaWindow is a subscription rate-limit bucket (5-hour, weekly, …).
type QuotaWindow struct {
	Name          string    `json:"name"`
	UsedPercent   float64   `json:"used_percent"`
	ResetAt       time.Time `json:"reset_at,omitempty"`
	ResetAfterSec int64     `json:"reset_after_seconds,omitempty"`
}

// UsageReport is the snapshot shown by /usage.
type UsageReport struct {
	Source          string       `json:"source,omitempty"`
	Session         Usage        `json:"session"`
	SessionRequests int64        `json:"session_requests"`
	SessionCostUSD  float64      `json:"session_cost_usd,omitempty"`
	HasSessionCost  bool         `json:"has_session_cost,omitempty"`
	RateLimit       RateLimit    `json:"rate_limit"`
	Account         AccountUsage `json:"account"`
}

// ContextUsage is the estimated occupancy of the conversation context window.
type ContextUsage struct {
	WindowTokens       int64 `json:"window_tokens"`
	UsedTokens         int64 `json:"used_tokens"`
	Percent            int   `json:"percent"`
	InstructionsTokens int64 `json:"instructions_tokens"`
	UserTokens         int64 `json:"user_tokens"`
	AssistantTokens    int64 `json:"assistant_tokens"`
	ReasoningTokens    int64 `json:"reasoning_tokens"`
	ToolTokens         int64 `json:"tool_tokens"`
}

func ContextPercent(used, window int64) int {
	if window <= 0 || used <= 0 {
		return 0
	}
	percent := int((used*100 + window - 1) / window)
	if percent < 1 {
		return 1
	}
	return percent
}

type ModelInput struct {
	UserText    string
	ToolResults []ToolResult
	// FinalStep asks for a tool-free answer. Providers must keep the tool
	// definitions in the request and forbid calls instead: withdrawing the
	// tools mid-conversation makes models write the call syntax as message
	// text.
	FinalStep bool
}

type ModelResponse struct {
	Text      string
	ToolCalls []ToolCall
	Usage     Usage
}

// Model owns the in-memory provider conversation.
type Model interface {
	Respond(context.Context, ModelInput, []ToolDefinition, EmitFunc) (ModelResponse, error)
	Reset()
	// AbsorbToolResults records tool outputs without making an API call.
	AbsorbToolResults([]ToolResult)
	// CloseOpenToolCalls writes error outputs for function calls that were never executed.
	CloseOpenToolCalls(reason string)
}

type Executor interface {
	Definitions() []ToolDefinition
	Execute(context.Context, []ToolCall, EmitFunc) []ToolResult
}

type Result struct {
	Answer      string       `json:"answer"`
	Steps       int          `json:"steps"`
	Usage       Usage        `json:"usage"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}
