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

// Package eval runs isolated development evaluations using the production agent.
package eval

import (
	"time"

	"gxx/internal/agent"
	"gxx/internal/config"
)

const Version = 1

type Suite struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

type Case struct {
	ID             string                `json:"id"`
	Category       string                `json:"category"`
	Fixture        map[string]string     `json:"fixture"`
	Git            bool                  `json:"git,omitempty"`
	InitialChanges map[string]string     `json:"initial_changes,omitempty"`
	Mode           string                `json:"mode"`
	Permission     string                `json:"permission"`
	Requires       []string              `json:"requires,omitempty"`
	Turns          []Turn                `json:"turns"`
	Checks         []Check               `json:"checks"`
	Rubric         []string              `json:"rubric,omitempty"`
	Script         []agent.ModelResponse `json:"script"` // Offline only; never sent to a live model.
}

type Turn struct {
	Prompt       string `json:"prompt"`
	CompactAfter bool   `json:"compact_after,omitempty"`
}

type Check struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Want    string `json:"want,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Min     int    `json:"min,omitempty"`
	Command string `json:"command,omitempty"`
}

type Matrix struct {
	Version        int             `json:"version"`
	Configurations []Configuration `json:"configurations"`
}

type Configuration struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
	Context  string `json:"context"`
	Eco      int    `json:"eco"`
}

type Options struct {
	Live        bool
	Trials      int
	MaxTokens   int64
	MaxRequests int
	CaseTimeout time.Duration
	MaxSteps    int
	Trace       bool
	Revision    string
	Credentials config.Config
	// BackendFactory is for tests. Live CLI runs always use production providers.
	BackendFactory func(config.Config, Case) (agent.Backend, error)
}

type Report struct {
	Version       int       `json:"version"`
	Mode          string    `json:"mode"`
	Revision      string    `json:"revision"`
	SuiteHash     string    `json:"suite_hash"`
	Started       time.Time `json:"started"`
	MaxTokens     int64     `json:"max_tokens"`
	MaxRequests   int       `json:"max_requests"`
	Trials        int       `json:"trials"`
	MaxSteps      int       `json:"max_steps_per_turn"`
	CaseTimeoutMS int64     `json:"case_timeout_ms"`
	Attempts      []Attempt `json:"attempts"`
}

type Attempt struct {
	Case          string        `json:"case"`
	Category      string        `json:"category"`
	Configuration Configuration `json:"configuration"`
	Trial         int           `json:"trial"`
	Status        string        `json:"status"` // passed, failed, review_required, skipped, interrupted
	Reason        string        `json:"reason,omitempty"`
	DurationMS    int64         `json:"duration_ms"`
	Usage         agent.Usage   `json:"usage"`
	CostUSD       *float64      `json:"estimated_cost_usd,omitempty"`
	Requests      int           `json:"requests"`
	UnknownUsage  int           `json:"requests_without_reported_usage"`
	ToolCalls     int           `json:"tool_calls"`
	Compactions   int           `json:"compactions"`
	Checks        []CheckResult `json:"checks,omitempty"`
	Rubric        []string      `json:"manual_review_rubric,omitempty"`
	Answers       []string      `json:"answers,omitempty"`
	Trace         []TraceEvent  `json:"trace,omitempty"`
}

type CheckResult struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Passed bool   `json:"passed"`
}

// TraceEvent omits provider reasoning, headers, account details and raw errors.
type TraceEvent struct {
	Kind       agent.EventKind `json:"kind"`
	Step       int             `json:"step"`
	Tool       string          `json:"tool,omitempty"`
	CallID     string          `json:"call_id,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	Truncated  bool            `json:"truncated,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
}

func modeName(live bool) string {
	if live {
		return "live"
	}
	return "offline-simulation"
}
