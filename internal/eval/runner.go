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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/budget"
	"gxx/internal/config"
	"gxx/internal/session"
	"gxx/internal/tools"
	"gxx/internal/workspace"
)

func Run(ctx context.Context, suite Suite, matrix Matrix, options Options) (Report, error) {
	if err := Validate(suite, matrix, options); err != nil {
		return Report{}, err
	}
	if options.Trials == 0 {
		options.Trials = 3
	}
	if options.CaseTimeout == 0 {
		options.CaseTimeout = 2 * time.Minute
	}
	if options.MaxSteps == 0 {
		options.MaxSteps = config.DefaultMaxSteps
	}
	encoded, _ := json.Marshal(suite)
	hash := sha256.Sum256(encoded)
	report := Report{Version: Version, Mode: modeName(options.Live), Revision: options.Revision,
		SuiteHash: hex.EncodeToString(hash[:]), Started: time.Now().UTC(), MaxTokens: options.MaxTokens, MaxRequests: options.MaxRequests,
		Trials: options.Trials, MaxSteps: options.MaxSteps, CaseTimeoutMS: options.CaseTimeout.Milliseconds()}
	meter := NewMeter(options.MaxTokens, options.MaxRequests)
	for _, configuration := range matrix.Configurations {
		for _, task := range suite.Cases {
			for trial := 1; trial <= options.Trials; trial++ {
				attempt := Attempt{Case: task.ID, Category: task.Category, Configuration: configuration, Trial: trial}
				switch {
				case ctx.Err() != nil:
					attempt.Status, attempt.Reason = "skipped", "canceled"
				case meter.Exhausted():
					attempt.Status, attempt.Reason = "skipped", "budget_exhausted"
				default:
					attempt = runAttempt(ctx, task, options, meter, attempt)
				}
				report.Attempts = append(report.Attempts, attempt)
			}
		}
	}
	redactReport(&report, options.Credentials)
	return report, nil
}

func runAttempt(ctx context.Context, task Case, options Options, meter *Meter, attempt Attempt) (out Attempt) {
	start := time.Now()
	defer func() { out.DurationMS = time.Since(start).Milliseconds() }()
	attempt.Status = "failed"
	requirements := append([]string(nil), task.Requires...)
	if task.Git {
		requirements = append(requirements, "git")
	}
	for _, binary := range requirements {
		if _, err := exec.LookPath(binary); err != nil {
			attempt.Status, attempt.Reason = "skipped", "dependency_unavailable:"+binary
			return attempt
		}
	}
	root, err := os.MkdirTemp("", "gxx-eval-")
	if err != nil {
		attempt.Reason = "workspace_setup_failed"
		return attempt
	}
	defer os.RemoveAll(root)
	ctx, cancel := context.WithTimeout(ctx, options.CaseTimeout)
	defer cancel()
	if err := prepareFixture(ctx, root, task); err != nil {
		attempt.Reason = "fixture_setup_failed"
		return attempt
	}
	ws, err := workspace.New(root)
	if err != nil {
		attempt.Reason = "workspace_setup_failed"
		return attempt
	}
	defer ws.Close()
	settings := runtimeConfig(root, attempt.Configuration, task.Permission, options)
	if err := settings.ValidateInteractive(); err != nil {
		attempt.Reason = "invalid_configuration"
		return attempt
	}
	attempt.Configuration.Model, attempt.Configuration.Provider, attempt.Configuration.Context = settings.Model, settings.Provider, settings.Context
	if options.Live {
		if err := settings.Validate(); err != nil {
			attempt.Status, attempt.Reason = "skipped", "credentials_unavailable"
			return attempt
		}
	}
	var backend agent.Backend
	if options.BackendFactory != nil {
		backend, err = options.BackendFactory(settings, task)
	} else if !options.Live {
		backend = &ScriptedBackend{Responses: task.Script}
	}
	if err != nil {
		attempt.Reason = "backend_setup_failed"
		return attempt
	}
	core, err := session.New(settings, ws, approval.NewPolicy(task.Permission, nil), session.Options{
		Backend: backend, IsolateSkills: true, Mode: task.Mode, Eco: attempt.Configuration.Eco,
	})
	if err != nil {
		attempt.Reason = "backend_setup_failed"
		return attempt
	}
	local := NewMeter(0, 0)
	ctx = agent.WithRequestObserver(ctx, trialMeter{meter, local})
	var results []agent.ToolResult
	var emitMu sync.Mutex
	emit := func(event agent.Event) {
		emitMu.Lock()
		defer emitMu.Unlock()
		if event.Kind == agent.EventToolCall {
			attempt.ToolCalls++
		}
		if event.Kind == agent.EventNotice && strings.HasPrefix(event.Text, budget.CompactNotice) {
			attempt.Compactions++
		}
		if options.Trace && (event.Kind == agent.EventToolCall || event.Kind == agent.EventToolDone || event.Kind == agent.EventNotice) {
			entry := TraceEvent{Kind: event.Kind, Step: event.Step}
			if event.ToolCall != nil {
				entry.Tool = event.ToolCall.Name
				entry.CallID = event.ToolCall.ID
			}
			if event.Result != nil {
				entry.CallID = event.Result.CallID
				entry.Tool, entry.IsError, entry.Truncated, entry.DurationMS = event.Result.Name, toolFailed(*event.Result), event.Result.Truncated, event.Result.DurationMS
			}
			attempt.Trace = append(attempt.Trace, entry)
		}
	}
	for _, turn := range task.Turns {
		result, runErr := core.Loop.Run(ctx, turn.Prompt, emit)
		results = append(results, result.ToolResults...)
		attempt.Answers = append(attempt.Answers, result.Answer)
		if runErr != nil {
			err = runErr
			break
		}
		if turn.CompactAfter {
			if err = core.Backend.Compact(ctx, emit, ""); err != nil {
				break
			}
		}
	}
	measurement := local.Snapshot()
	attempt.Usage, attempt.Requests, attempt.UnknownUsage = measurement.Usage, measurement.Requests, measurement.Unknown
	if options.Live && measurement.CostKnown && measurement.Requests > 0 {
		cost := measurement.Cost
		attempt.CostUSD = &cost
	}
	attempt.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		attempt.Reason = classifyError(err)
		if errors.Is(err, ErrBudget) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			attempt.Status = "interrupted"
		}
		return attempt
	}
	allPassed := true
	for _, check := range task.Checks {
		result := grade(ctx, ws, task, check, attempt.Answers, results)
		attempt.Checks = append(attempt.Checks, result)
		allPassed = allPassed && result.Passed
	}
	attempt.DurationMS = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		attempt.Status, attempt.Reason = "interrupted", classifyError(ctx.Err())
		return attempt
	}
	if !allPassed {
		attempt.Reason = "checks_failed"
		return attempt
	}
	attempt.Status = "passed"
	if options.Live && len(task.Rubric) > 0 {
		attempt.Status, attempt.Rubric = "review_required", append([]string(nil), task.Rubric...)
	}
	return attempt
}

func runtimeConfig(root string, c Configuration, permission string, options Options) config.Config {
	return config.Config{Workspace: root, Provider: c.Provider, Model: c.Model, Effort: c.Effort, Context: c.Context,
		PermissionMode: permission, MaxSteps: options.MaxSteps, MaxToolResultBytes: config.DefaultMaxToolResultBytes,
		MaxSearchResults: config.DefaultMaxSearchResults, ParallelReads: config.DefaultParallelReads,
		CommandTimeout: min(options.CaseTimeout, 30*time.Second), APITimeout: options.CaseTimeout,
		APIKey: options.Credentials.APIKey, OpenAITokens: options.Credentials.OpenAITokens, ClaudeTokens: options.Credentials.ClaudeTokens}
}

func prepareFixture(ctx context.Context, root string, task Case) error {
	write := func(files map[string]string) error {
		for name, body := range files {
			path := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				return err
			}
		}
		return nil
	}
	if err := write(task.Fixture); err != nil {
		return err
	}
	if task.Git {
		for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=gxx-eval", "-c", "user.email=eval@localhost", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=", "commit", "-qm", "fixture"}} {
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Dir = root
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}
	return write(task.InitialChanges)
}

func grade(ctx context.Context, ws *workspace.Workspace, task Case, check Check, answers []string, results []agent.ToolResult) CheckResult {
	result := CheckResult{Kind: check.Kind, Target: check.Path}
	answer := strings.Join(answers, "\n")
	switch check.Kind {
	case "final_answer_contains":
		if len(answers) > 0 {
			result.Passed = strings.Contains(strings.ToLower(answers[len(answers)-1]), strings.ToLower(check.Want))
		}
	case "answer_contains":
		result.Passed = strings.Contains(strings.ToLower(answer), strings.ToLower(check.Want))
	case "answer_not_contains":
		result.Passed = !strings.Contains(strings.ToLower(answer), strings.ToLower(check.Want))
	case "file_contains", "file_equals", "file_unchanged":
		body, err := ws.ReadRegularFile(check.Path, 4<<20)
		if err != nil {
			break
		}
		want := check.Want
		if check.Kind == "file_unchanged" {
			want = task.Fixture[check.Path]
			if changed, ok := task.InitialChanges[check.Path]; ok {
				want = changed
			}
		}
		if check.Kind == "file_contains" {
			result.Passed = strings.Contains(string(body), want)
		} else {
			result.Passed = string(body) == want
		}
	case "file_absent":
		_, err := ws.Stat(check.Path)
		result.Passed = errors.Is(err, os.ErrNotExist)
	case "tool_called", "tool_failed", "tool_succeeded":
		count := 0
		for _, r := range results {
			if r.Name == check.Tool && (check.Kind == "tool_called" || (check.Kind == "tool_failed" && toolFailed(r)) || (check.Kind == "tool_succeeded" && !toolFailed(r))) {
				count++
			}
		}
		result.Target, result.Passed = check.Tool, count >= max(1, check.Min)
	case "command":
		registry := tools.NewRegistry(ws, approval.NewPolicy(config.PermissionAuto, nil), tools.Options{MaxResultBytes: 4096, MaxSearchResult: 10, ParallelReads: 1, CommandTimeout: 30 * time.Second})
		args, _ := json.Marshal(map[string]any{"command": check.Command})
		r := registry.Execute(ctx, []agent.ToolCall{{ID: "grader", Name: "run_command", Arguments: args}}, nil)
		result.Target, result.Passed = check.Command, len(r) == 1 && !toolFailed(r[0])
	}
	return result
}

func classifyError(err error) string {
	switch {
	case errors.Is(err, ErrBudget):
		return "budget_exhausted"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, agent.ErrMaxSteps):
		return "max_steps"
	default:
		return "agent_error"
	}
}

func redactReport(report *Report, credentials config.Config) {
	secrets := []string{credentials.APIKey, credentials.OpenAITokens.AccessToken, credentials.OpenAITokens.RefreshToken, credentials.ClaudeTokens.AccessToken, credentials.ClaudeTokens.RefreshToken}
	// Redact string values before encoding, so even unusual credential contents
	// cannot corrupt JSON numbers or turn a failed decode into an unredacted report.
	var visit func(reflect.Value)
	visit = func(value reflect.Value) {
		switch value.Kind() {
		case reflect.Pointer:
			if !value.IsNil() {
				visit(value.Elem())
			}
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				if value.Field(i).CanSet() {
					visit(value.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < value.Len(); i++ {
				visit(value.Index(i))
			}
		case reflect.String:
			text := value.String()
			for _, secret := range secrets {
				if secret != "" {
					text = strings.ReplaceAll(text, secret, "[REDACTED]")
				}
			}
			if value.CanSet() {
				value.SetString(text)
			}
		}
	}
	visit(reflect.ValueOf(report))
}

func Validate(suite Suite, matrix Matrix, options Options) error {
	if suite.Version != Version || matrix.Version != Version {
		return errors.New("unsupported suite or matrix version")
	}
	if len(suite.Cases) == 0 || len(matrix.Configurations) == 0 {
		return errors.New("suite and matrix must not be empty")
	}
	if options.Trials < 0 || options.CaseTimeout < 0 || options.MaxSteps < 0 || options.MaxSteps > config.MaxStepsLimit || options.MaxTokens < 0 || options.MaxRequests < 0 {
		return errors.New("invalid evaluation limits")
	}
	if options.Live && (options.MaxTokens <= 0 || options.MaxRequests <= 0) {
		return errors.New("live evaluations require positive token and request budgets")
	}
	ids := map[string]bool{}
	for _, c := range matrix.Configurations {
		if c.ID == "" || ids[c.ID] {
			return errors.New("configuration IDs must be nonempty and unique")
		}
		ids[c.ID] = true
		if c.Model == "" || c.Provider != config.ProviderForModel(c.Model) {
			return fmt.Errorf("configuration %s: model/provider mismatch", c.ID)
		}
		if err := config.ValidateEffort(c.Effort); err != nil {
			return err
		}
		if _, err := config.NormalizeContext(c.Context); err != nil {
			return err
		}
		if c.Eco < 0 || c.Eco > 3 {
			return errors.New("eco must be between 0 and 3")
		}
	}
	ids = map[string]bool{}
	for _, task := range suite.Cases {
		if task.ID == "" || ids[task.ID] || len(task.Turns) == 0 || len(task.Checks) == 0 {
			return errors.New("cases need unique IDs, turns and checks")
		}
		ids[task.ID] = true
		if task.Mode != "agent" && task.Mode != "ask" && task.Mode != "plan" {
			return errors.New("invalid case mode")
		}
		if _, err := config.CanonicalPermission(task.Permission); err != nil {
			return err
		}
		for _, files := range []map[string]string{task.Fixture, task.InitialChanges} {
			for path := range files {
				if !fixturePath(path) {
					return fmt.Errorf("invalid fixture path %q", path)
				}
			}
		}
		for _, turn := range task.Turns {
			if strings.TrimSpace(turn.Prompt) == "" {
				return errors.New("turn prompt is empty")
			}
		}
		if !options.Live && options.BackendFactory == nil && len(task.Script) == 0 {
			return errors.New("offline cases require a script")
		}
		for _, check := range task.Checks {
			switch check.Kind {
			case "file_contains", "file_equals", "file_unchanged", "file_absent":
				if !fixturePath(check.Path) {
					return errors.New("invalid check path")
				}
				if check.Kind == "file_unchanged" {
					_, original := task.Fixture[check.Path]
					_, changed := task.InitialChanges[check.Path]
					if !original && !changed {
						return errors.New("file_unchanged needs an initial file")
					}
				}
			case "answer_contains", "answer_not_contains", "final_answer_contains":
				if check.Want == "" {
					return errors.New("answer check must not be empty")
				}
			case "tool_called", "tool_failed", "tool_succeeded":
				if check.Tool == "" || check.Min < 0 {
					return errors.New("invalid tool check")
				}
			case "command":
				if strings.TrimSpace(check.Command) == "" {
					return errors.New("command check is empty")
				}
			default:
				return fmt.Errorf("unknown check kind %q", check.Kind)
			}
		}
	}
	return nil
}

func fixturePath(path string) bool {
	if !filepath.IsLocal(path) || strings.Contains(path, "\\") || strings.Contains(path, ":") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if strings.EqualFold(part, ".git") || part == ".." || part == "." || part == "" {
			return false
		}
	}
	return true
}

// run_command reports a nonzero exit in its text even when execution itself
// succeeded. Graders must not confuse successful launch with a passing check.
func toolFailed(result agent.ToolResult) bool {
	return result.IsError || (result.Name == "run_command" && strings.HasPrefix(strings.TrimSpace(result.Output), "exit code"))
}
