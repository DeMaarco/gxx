package agent

import (
	"context"
	"encoding/json"
	"time"
)

type EventKind string

const (
	EventTextDelta   EventKind = "text_delta"
	EventToolCall    EventKind = "tool_call"
	EventToolStarted EventKind = "tool_started"
	EventToolDone    EventKind = "tool_done"
	EventNotice      EventKind = "notice"
)

// Event is a provider-agnostic progress update for terminal renderers.
type Event struct {
	Kind      EventKind
	Text      string
	ToolCall  *ToolCall
	Result    *ToolResult
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

type Usage struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.ReasoningTokens += other.ReasoningTokens
	u.TotalTokens += other.TotalTokens
}

type ModelInput struct {
	UserText    string
	ToolResults []ToolResult
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
