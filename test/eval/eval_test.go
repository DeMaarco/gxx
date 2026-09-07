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

package eval_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/config"
	"gxx/internal/eval"
)

func smallSuite() (eval.Suite, eval.Matrix) {
	return eval.Suite{Version: 1, Cases: []eval.Case{{ID: "case", Category: "test", Mode: "ask", Permission: "ask",
		Fixture: map[string]string{"keep.txt": "original"}, Turns: []eval.Turn{{Prompt: "Answer done without changing keep.txt."}},
		Script: []agent.ModelResponse{{Text: "done"}}, Checks: []eval.Check{{Kind: "answer_contains", Want: "done"}, {Kind: "file_unchanged", Path: "keep.txt"}},
	}}}, eval.Matrix{Version: 1, Configurations: []eval.Configuration{{ID: "default", Provider: "openai", Model: config.DefaultModel, Effort: "medium", Context: "272k"}}}
}

func TestOfflineSuiteExercisesRealToolsAndGraders(t *testing.T) {
	var suite eval.Suite
	var matrix eval.Matrix
	for path, target := range map[string]any{"../../evals/suite.json": &suite, "../../evals/matrix.json": &matrix} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
	}
	if len(suite.Cases) != 24 {
		t.Fatalf("cases = %d", len(suite.Cases))
	}
	// Browser execution is opt-in in development; CI covers the missing dependency
	// path while provider-independent browser behavior has a manual rubric.
	for i := range suite.Cases {
		if suite.Cases[i].ID == "web-browser" {
			suite.Cases[i].Requires = []string{"gxx-eval-test-missing-browser"}
		}
	}
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 1, CaseTimeout: 30 * time.Second, Trace: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range report.Attempts {
		if a.Status != "passed" && a.Status != "skipped" {
			t.Errorf("%s: %s %s checks=%+v", a.Case, a.Status, a.Reason, a.Checks)
		}
		if a.Requests != 0 || a.CostUSD != nil {
			t.Fatal("simulation claimed provider usage")
		}
		if a.Case == "follow-six-references" && a.ToolCalls < 6 {
			t.Fatal("did not exercise exploration beyond four reads")
		}
		if a.Case == "compact-repeat-constraint" && a.Compactions != 2 {
			t.Fatalf("compactions = %d", a.Compactions)
		}
	}
}

func TestTrialsUseFreshWorkspacesAndDoNotLoadPersonalSkills(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	personal := filepath.Join(base, "gxx", "skills", "personal-only")
	if err := os.MkdirAll(personal, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(personal, "SKILL.md"), []byte("---\nname: personal-only\ndescription: Never load this catalog entry.\n---\nBody"), 0600); err != nil {
		t.Fatal(err)
	}
	suite, matrix := smallSuite()
	var roots []string
	var models []*eval.ScriptedBackend
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 3, BackendFactory: func(c config.Config, task eval.Case) (agent.Backend, error) {
		roots = append(roots, c.Workspace)
		m := &eval.ScriptedBackend{Responses: task.Script}
		models = append(models, m)
		return m, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Attempts) != 3 || roots[0] == roots[1] {
		t.Fatal("trials did not get separate workspaces")
	}
	for i, root := range roots {
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("temporary workspace leaked")
		}
		if strings.Contains(models[i].Instructions, "personal-only") || strings.Contains(models[i].Inputs[0].UserText, "personal-only") {
			t.Fatal("personal skills contaminated evaluation")
		}
	}
}

func TestFailedValidationCommandDoesNotPass(t *testing.T) {
	suite, matrix := smallSuite()
	suite.Cases[0].Checks = append(suite.Cases[0].Checks, eval.Check{Kind: "command", Command: "gxx-eval-test-command-not-found"})
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Attempts[0].Status != "failed" {
		t.Fatal("nonzero command exit was graded as a pass")
	}
}

func TestBudgetIncludesRetriesAndSummaries(t *testing.T) {
	meter := eval.NewMeter(10, 3)
	ctx := agent.WithRequestObserver(context.Background(), meter)
	for i, kind := range []string{"response", "response", "summary"} {
		finish, err := agent.BeginRequest(ctx, agent.Request{Provider: "openai", Model: config.DefaultModel, Kind: kind, Attempt: i + 1})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			finish(agent.Usage{}, errors.New("retryable"))
		} else {
			finish(agent.Usage{InputTokens: 3, OutputTokens: 3, TotalTokens: 6}, nil)
		}
	}
	if _, err := agent.BeginRequest(ctx, agent.Request{}); !errors.Is(err, eval.ErrBudget) {
		t.Fatalf("budget error = %v", err)
	}
	m := meter.Snapshot()
	if m.Requests != 3 || m.Usage.TotalTokens != 12 || m.Unknown != 1 || m.CostKnown {
		t.Fatalf("measurement = %+v", m)
	}
}

type meteredBackend struct{ *eval.ScriptedBackend }

func (m meteredBackend) Respond(ctx context.Context, input agent.ModelInput, defs []agent.ToolDefinition, emit agent.EmitFunc) (agent.ModelResponse, error) {
	finish, err := agent.BeginRequest(ctx, agent.Request{Provider: "openai", Model: config.DefaultModel, Kind: "response", Attempt: 1})
	if err != nil {
		return agent.ModelResponse{}, err
	}
	finish(agent.Usage{InputTokens: 5, OutputTokens: 5, TotalTokens: 10}, nil)
	return m.ScriptedBackend.Respond(ctx, input, defs, emit)
}

func TestExhaustedBudgetMarksRemainingAttemptsNotRun(t *testing.T) {
	suite, matrix := smallSuite()
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 3, MaxTokens: 5, MaxRequests: 2, BackendFactory: func(c config.Config, task eval.Case) (agent.Backend, error) {
		return meteredBackend{&eval.ScriptedBackend{Responses: task.Script}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Attempts[0].Status != "passed" || report.Attempts[1].Status != "skipped" || report.Attempts[1].Reason != "budget_exhausted" {
		t.Fatalf("attempts: %+v", report.Attempts)
	}
}

type waitingBackend struct{ *eval.ScriptedBackend }

func (m waitingBackend) Respond(ctx context.Context, _ agent.ModelInput, _ []agent.ToolDefinition, _ agent.EmitFunc) (agent.ModelResponse, error) {
	<-ctx.Done()
	return agent.ModelResponse{}, ctx.Err()
}

func TestTimeoutCancellationAndMissingDependencies(t *testing.T) {
	suite, matrix := smallSuite()
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 1, CaseTimeout: 10 * time.Millisecond, BackendFactory: func(config.Config, eval.Case) (agent.Backend, error) {
		return waitingBackend{&eval.ScriptedBackend{}}, nil
	}})
	if err != nil || report.Attempts[0].Reason != "timeout" {
		t.Fatalf("timeout report=%+v err=%v", report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err = eval.Run(ctx, suite, matrix, eval.Options{Trials: 1})
	if err != nil || report.Attempts[0].Reason != "canceled" {
		t.Fatal("cancel not represented")
	}
	suite.Cases[0].Requires = []string{"gxx-eval-never-installed"}
	report, err = eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 1})
	if err != nil || report.Attempts[0].Status != "skipped" {
		t.Fatal("missing dependency counted as model failure")
	}
}

func TestFixtureTraversalAndLiveWithoutBudgetAreRejected(t *testing.T) {
	suite, matrix := smallSuite()
	for _, path := range []string{"../escape", "/outside", "C:/outside", "a/../../escape", ".git/config"} {
		suite.Cases[0].Fixture = map[string]string{path: "x"}
		if err := eval.Validate(suite, matrix, eval.Options{}); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	suite, matrix = smallSuite()
	if err := eval.Validate(suite, matrix, eval.Options{Live: true}); err == nil {
		t.Fatal("live did not require budget")
	}
}

func TestReportsRedactCredentialsAndRefuseIncompatibleBaseline(t *testing.T) {
	suite, matrix := smallSuite()
	secret := "fake-secret-for-test"
	suite.Cases[0].Script = []agent.ModelResponse{{Text: "done " + secret}}
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 1, Credentials: config.Config{APIKey: secret}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(report)
	if bytes.Contains(data, []byte(secret)) {
		t.Fatal("credential leaked into report")
	}
	baseline := report
	baseline.SuiteHash = "different"
	if _, err := eval.Markdown(report, &baseline); err == nil {
		t.Fatal("accepted incompatible baseline")
	}
	markdown, err := eval.Markdown(report, nil)
	if err != nil || !strings.Contains(markdown, "does not measure model quality") {
		t.Fatal("offline report misrepresented quality")
	}
}

func TestCLILiveRequiresBudgetBeforeCreatingReports(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report")
	var stdout, stderr bytes.Buffer
	code := eval.CLI(context.Background(), []string{"--suite", "../../evals/suite.json", "--matrix", "../../evals/matrix.json", "--live", "--out", out}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid run wrote reports")
	}
}

func TestLiveRubricRemainsPendingAndDoesNotMutateSuite(t *testing.T) {
	suite, matrix := smallSuite()
	suite.Cases[0].Rubric = []string{"Check synthetic-secret does not appear."}
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Live: true, Trials: 1, MaxTokens: 100, MaxRequests: 10,
		Credentials: config.Config{APIKey: "synthetic-secret"}, BackendFactory: func(c config.Config, task eval.Case) (agent.Backend, error) {
			return meteredBackend{&eval.ScriptedBackend{Responses: task.Script}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Attempts[0].Status != "review_required" || eval.Summaries(report)[0].Passed != 0 {
		t.Fatal("pending human review counted as success")
	}
	if suite.Cases[0].Rubric[0] != "Check synthetic-secret does not appear." {
		t.Fatal("report redaction mutated the suite")
	}
}

func TestRedactionCannotCorruptNumericFields(t *testing.T) {
	suite, matrix := smallSuite()
	suite.Cases[0].Script[0].Text = "done 0"
	report, err := eval.Run(context.Background(), suite, matrix, eval.Options{Trials: 1, Credentials: config.Config{APIKey: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || strings.Contains(report.Attempts[0].Answers[0], "0") || !strings.Contains(report.Attempts[0].Answers[0], "[REDACTED]") {
		t.Fatal("redaction failed or damaged report metadata")
	}
}
