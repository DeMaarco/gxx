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

package ui_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gxx/internal/ui"

	"gxx/internal/agent"
	"gxx/internal/config"
)

type replModel struct {
	resetCount int
	prompts    []string
	reply      string
}

func (m *replModel) Respond(
	_ context.Context,
	input agent.ModelInput,
	_ []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	if strings.TrimSpace(input.UserText) != "" {
		m.prompts = append(m.prompts, input.UserText)
	}
	reply := m.reply
	if reply == "" {
		reply = "world"
	}
	agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: reply})
	return agent.ModelResponse{Text: reply}, nil
}

func (m *replModel) Reset() {
	m.resetCount++
}

func (m *replModel) AbsorbToolResults([]agent.ToolResult) {}

func (m *replModel) CloseOpenToolCalls(string) {}

type emptyExecutor struct{}

func (emptyExecutor) Definitions() []agent.ToolDefinition {
	return nil
}

func (emptyExecutor) Execute(
	_ context.Context,
	_ []agent.ToolCall,
	_ agent.EmitFunc,
) []agent.ToolResult {
	return nil
}

func TestRunREPLHandlesCommandsAndInjectedIO(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/help\n/clear\nhello\n/exit\n"))
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		renderer,
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"◆ gxx  v0.0.1",
		">",
		"test-model  ·  ask  ·  medium  ·  272k  ·  (0%)",
		"/clear",
		"Shift+Tab",
		"Conversation cleared.",
		"world",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
	if model.resetCount != 1 {
		t.Fatalf("reset count = %d, want 1", model.resetCount)
	}
}

func TestRunREPLListsSkills(t *testing.T) {
	loop := &agent.Loop{Model: &replModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/skills\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			ListSkills: func() []ui.SkillEntry {
				return []ui.SkillEntry{
					{Name: "code-review", Origin: "project", Description: "Review diffs"},
					{Name: "personal", Origin: "user", Description: "Personal helper"},
				}
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"code-review",
		"(project)",
		"Review diffs",
		"personal",
		"(user)",
		"Personal helper",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
}

func TestRunREPLSkillsEmptyHint(t *testing.T) {
	loop := &agent.Loop{Model: &replModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/skills\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			ListSkills:       func() []ui.SkillEntry { return nil },
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "No skills discovered.") {
		t.Fatalf("output = %q, want empty hint", text)
	}
	if !strings.Contains(text, ".agents/skills") || !strings.Contains(text, "~/.config/gxx/skills") {
		t.Fatalf("output = %q, want discovery path hints", text)
	}
}

func TestRunREPLRefreshesModelsBeforeEachPrompt(t *testing.T) {
	loop := &agent.Loop{Model: &replModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/exit\n"))
	var output bytes.Buffer
	var calls atomic.Int32
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			Models:           []string{"stale"},
			RefreshModels: func(settings *ui.REPLSettings) {
				calls.Add(1)
				settings.Models = []string{"live"}
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if calls.Load() < 1 {
		t.Fatal("RefreshModels was not called before the prompt")
	}
}

func TestRunREPLShowsUsage(t *testing.T) {
	loop := &agent.Loop{Model: &replModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/usage\n/exit\n"))
	var output bytes.Buffer

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			FetchUsage: func(context.Context) (agent.UsageReport, error) {
				return agent.UsageReport{
					Session:         agent.Usage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
					SessionRequests: 1,
					RateLimit: agent.RateLimit{
						Known:             true,
						RequestsLimit:     100,
						RequestsRemaining: 99,
						TokensLimit:       1000,
						TokensRemaining:   900,
					},
					Account: agent.AccountUsage{
						SpendUSD:     1.5,
						HasSpend:     true,
						LimitUSD:     10,
						HasLimit:     true,
						RemainingUSD: 8.5,
						HasRemaining: true,
					},
				}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"Session",
		"remaining      $8.50",
		"99 / 100",
		"900 / 1,000",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
}

type pricedModel struct{}

func (pricedModel) Respond(
	_ context.Context,
	_ agent.ModelInput,
	_ []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	usage := agent.Usage{InputTokens: 8100, OutputTokens: 4300, TotalTokens: 12400}
	agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: "world"})
	agent.Emit(emit, agent.Event{Kind: agent.EventUsage, Usage: usage})
	return agent.ModelResponse{Text: "world", Usage: usage}, nil
}

func (pricedModel) Reset()                               {}
func (pricedModel) AbsorbToolResults([]agent.ToolResult) {}
func (pricedModel) CloseOpenToolCalls(string)            {}

func TestRunREPLShowsTurnCostAfterPrompt(t *testing.T) {
	var refreshed atomic.Int32
	loop := &agent.Loop{Model: pricedModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("hello\n/exit\n"))
	var output bytes.Buffer

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:        "0.0.1",
			Model:          "gpt-5.6-sol",
			PermissionMode: config.PermissionAsk,
			RefreshPricing: func(context.Context) { refreshed.Add(1) },
			QuoteCost: func(usage agent.Usage) (float64, bool) {
				if usage.TotalTokens == 0 {
					return 0, false
				}
				return 0.042, true
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if refreshed.Load() < 1 {
		t.Fatalf("pricing refresh count = %d, want at least one after the prompt", refreshed.Load())
	}
	if !strings.Contains(output.String(), "$0.042") {
		t.Fatalf("output = %q, want turn cost", output.String())
	}
}

func TestRunREPLShowsContext(t *testing.T) {
	loop := &agent.Loop{Model: &replModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/context\n/exit\n"))
	var output bytes.Buffer

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			FetchContext: func() agent.ContextUsage {
				return agent.ContextUsage{
					WindowTokens:       272_000,
					UsedTokens:         27_200,
					Percent:            10,
					InstructionsTokens: 1_200,
					UserTokens:         8_000,
					AssistantTokens:    12_000,
					ReasoningTokens:    4_000,
					ToolTokens:         2_000,
				}
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"10%",
		"27,200 / 272,000",
		"instructions",
		"user",
		"assistant",
		"reasoning",
		"tools",
		"free",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
}

func TestRunREPLConfiguresAPIKeyWithoutEchoingIt(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/config\n/exit\n"))
	var output bytes.Buffer
	var saved string

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: false,
			ReadAPIKey: func(context.Context) (string, error) {
				return "secret-api-key", nil
			},
			SaveAPIKey: func(apiKey string) (string, error) {
				saved = apiKey
				return "/home/user/.config/gxx/config.json", nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if saved != "secret-api-key" {
		t.Fatalf("saved API key = %q", saved)
	}
	if strings.Contains(output.String(), "secret-api-key") {
		t.Fatalf("API key leaked to output: %q", output.String())
	}
	for _, expected := range []string{"Run /login", "API key saved", "Conversation cleared"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output = %q, want %q", output.String(), expected)
		}
	}
	if model.resetCount != 1 {
		t.Fatalf("reset count = %d, want 1", model.resetCount)
	}
}

func TestRunREPLRejectsUnknownSlashCommands(t *testing.T) {
	model := &countingReplModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/foo\n/help extra\nhello\n/exit\n"))
	var output bytes.Buffer

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"unknown command /foo",
		"unexpected argument for /help",
		"world",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
	if model.calls != 1 {
		t.Fatalf("model calls = %d, want 1", model.calls)
	}
}

func TestRunREPLInterruptedTurnStaysOpen(t *testing.T) {
	loop := &agent.Loop{Model: &canceledReplModel{}, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("hello\n/exit\n"))
	var output bytes.Buffer

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v, want to stay open after interrupt", err)
	}
	if !strings.Contains(output.String(), "interrupted") {
		t.Fatalf("output = %q, want interrupted", output.String())
	}
}

type countingReplModel struct {
	replModel
	calls int
}

func (m *countingReplModel) Respond(
	ctx context.Context,
	input agent.ModelInput,
	definitions []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	m.calls++
	return m.replModel.Respond(ctx, input, definitions, emit)
}

type canceledReplModel struct{}

func (canceledReplModel) Respond(
	context.Context,
	agent.ModelInput,
	[]agent.ToolDefinition,
	agent.EmitFunc,
) (agent.ModelResponse, error) {
	return agent.ModelResponse{}, context.Canceled
}

func (canceledReplModel) Reset() {}

func (canceledReplModel) AbsorbToolResults([]agent.ToolResult) {}

func (canceledReplModel) CloseOpenToolCalls(string) {}

func TestTurnGateFirstInterruptCancelsTurn(t *testing.T) {
	var gate ui.TurnGate
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	turnCtx, finish := gate.Start(parent)
	defer finish()

	gate.Handle(cancelParent)
	if turnCtx.Err() == nil {
		t.Fatal("first interrupt should cancel the turn")
	}
	if parent.Err() != nil {
		t.Fatal("first interrupt should not cancel the session")
	}
}

func TestTurnGateSecondInterruptCancelsSession(t *testing.T) {
	var gate ui.TurnGate
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	_, finish := gate.Start(parent)
	defer finish()

	gate.Handle(cancelParent)
	gate.Handle(cancelParent)
	if parent.Err() == nil {
		t.Fatal("second interrupt should cancel the session")
	}
}

func TestReadLineHonorsCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ui.ReadLine(ctx, bufio.NewReader(reader), nil)
	if err != context.Canceled {
		t.Fatalf("readLine() error = %v, want context.Canceled", err)
	}
}

func TestReadLineCancelDoesNotConsumeNextLine(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	buffered := bufio.NewReader(reader)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := ui.ReadLine(ctx, buffered, reader)
		done <- err
	}()
	<-started
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("readLine() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readLine() did not return after cancel")
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("hello\n"))
		writeDone <- err
	}()
	line, err := ui.ReadLine(context.Background(), buffered, reader)
	if err != nil {
		t.Fatalf("second readLine() error = %v", err)
	}
	if strings.TrimSpace(line) != "hello" {
		t.Fatalf("line = %q, cancelled read consumed the next prompt", line)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write line: %v", err)
	}
}

func TestRendererColorsToolEventsWhenEnabled(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRendererWithColor(&output, true)
	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind:     agent.EventToolStarted,
		ToolCall: &agent.ToolCall{Name: "read_file"},
	})
	renderer.Event(agent.Event{
		Kind:   agent.EventToolDone,
		Result: &agent.ToolResult{Name: "read_file", DurationMS: 4},
	})
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "\x1b[32m") || !strings.Contains(text, "read_file") {
		t.Fatalf("colored renderer output = %q, want green tool status", text)
	}
}

func TestRendererEscapesModelTerminalControls(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.StartTurn()
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: "\x1b[2Jhello\rhidden\u202ereversed"})
	renderer.Finish("")

	if strings.Contains(output.String(), "\x1b") ||
		strings.Contains(output.String(), "\r") ||
		strings.Contains(output.String(), "\u202e") {
		t.Fatalf("renderer output contains raw controls: %q", output.String())
	}
	if !strings.Contains(output.String(), `\u001b`) ||
		!strings.Contains(output.String(), `\r`) ||
		!strings.Contains(output.String(), `\u202e`) {
		t.Fatalf("renderer output = %q, want escaped controls", output.String())
	}
}

func TestRunREPLAppliesModelCommand(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader(
		"/model\n/model terra\n/model effort high fast on\n/exit\n",
	))
	var output bytes.Buffer
	var synced []ui.REPLSettings

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "gpt-5.6-sol",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Context:          "272k",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			ActiveAccount:    config.AccountAPI,
			SyncSession: func(session ui.REPLSettings) error {
				synced = append(synced, session)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"model gpt-5.6-sol · context 272k · effort medium · fast off",
		"* sol  gpt-5.6-sol",
		"model gpt-5.6-terra · context 272k · effort medium · fast off",
		"model gpt-5.6-terra · context 272k · effort high · fast on",
		"Conversation cleared.",
		"gpt-5.6-terra  ·  ask  ·  high  ·  272k  ·  (0%)  ·  fast",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
	if model.resetCount != 1 {
		t.Fatalf("reset count = %d, want 1 after model change", model.resetCount)
	}
	if len(synced) != 2 {
		t.Fatalf("synced %d times, want 2", len(synced))
	}
	if synced[0].Model != "gpt-5.6-terra" || synced[1].Effort != "high" || !synced[1].Fast {
		t.Fatalf("synced = %+v", synced)
	}
}

func TestRunREPLAppliesModeCommand(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader(
		"/mode\n/mode auto-writes\n/mode yolo\n/exit\n",
	))
	var output bytes.Buffer
	var synced []ui.REPLSettings

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "gpt-5.6-sol",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Context:          "272k",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			SyncSession: func(session ui.REPLSettings) error {
				synced = append(synced, session)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"permission ask · confirm file changes and commands in agent mode; reads always run",
		"* ask",
		"auto-writes",
		"permission auto-writes · file changes run without confirmation; commands still ask",
		"permission auto · file changes and commands run without confirmation",
		"gpt-5.6-sol  ·  auto  ·  medium  ·  272k  ·  (0%)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
	if model.resetCount != 0 {
		t.Fatalf("reset count = %d, want 0 after mode change", model.resetCount)
	}
	if len(synced) != 2 {
		t.Fatalf("synced %d times, want 2", len(synced))
	}
	if synced[0].PermissionMode != config.PermissionAutoWrites ||
		synced[1].PermissionMode != config.PermissionAuto {
		t.Fatalf("synced = %+v", synced)
	}
}

func TestRunREPLAppliesEcoCommand(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/eco 2\n/eco off\n/exit\n"))
	var output bytes.Buffer
	var ecoLevels []int
	synced := 0

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "gpt-5.6-luna",
			PermissionMode:   config.PermissionAsk,
			Effort:           "max",
			Context:          "1m",
			Fast:             true,
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			SetEco: func(level int) error {
				ecoLevels = append(ecoLevels, level)
				return nil
			},
			SyncSession: func(session ui.REPLSettings) error {
				synced++
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"eco full · caveman input · model unchanged",
		"* eco full",
		"eco off",
		"> eco full",
		"gpt-5.6-luna  ·  ask  ·  max  ·  1m  ·  (0%)  ·  fast",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
	if strings.Contains(text, "Conversation cleared.") {
		t.Fatalf("eco must not reset the conversation: %q", text)
	}
	if model.resetCount != 0 {
		t.Fatalf("reset count = %d, want 0", model.resetCount)
	}
	if synced != 0 {
		t.Fatalf("eco should not persist session, synced = %d", synced)
	}
	if len(ecoLevels) != 2 || ecoLevels[0] != 2 || ecoLevels[1] != 0 {
		t.Fatalf("eco levels = %v", ecoLevels)
	}
}

func TestRunREPLAppliesCompactCommand(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/compact auth tests\n/exit\n"))
	var output bytes.Buffer
	var focuses []string

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "gpt-5.6-luna",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			FetchContext: func() agent.ContextUsage {
				return agent.ContextUsage{UsedTokens: 12000, WindowTokens: 272000}
			},
			Compact: func(_ context.Context, focus string) error {
				focuses = append(focuses, focus)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if len(focuses) != 1 || focuses[0] != "auth tests" {
		t.Fatalf("focuses = %#v", focuses)
	}
	if !strings.Contains(output.String(), "Conversation compacted") {
		t.Fatalf("output = %q, want compact notice", output.String())
	}
}

func TestRunREPLLoginAndLogoutClaude(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/login claude\n/logout claude\n/exit\n"))
	var output bytes.Buffer
	loggedIn := false
	loggedOut := false

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "claude-sonnet-4-6",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			ClaudeConfigured: false,
			Login: func(_ context.Context, _ io.Writer, args []string) (string, error) {
				if len(args) == 0 || args[0] != "claude" {
					t.Errorf("login args = %v", args)
				}
				loggedIn = true
				return "/home/user/.config/gxx/config.json", nil
			},
			Logout: func(args []string) (string, error) {
				if len(args) == 0 || args[0] != "claude" {
					t.Errorf("logout args = %v", args)
				}
				loggedOut = true
				return "/home/user/.config/gxx/config.json", nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if !loggedIn || !loggedOut {
		t.Fatalf("login=%v logout=%v", loggedIn, loggedOut)
	}
	text := output.String()
	for _, expected := range []string{
		"Run /login",
		"Claude login saved",
		"Claude login cleared",
		"Conversation cleared",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
}

func TestRunREPLLoginAndLogoutOpenAI(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/login openai\n/logout chatgpt\n/exit\n"))
	var output bytes.Buffer
	var loginArgs, logoutArgs []string

	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:          "0.0.1",
			Model:            "gpt-5.6-sol",
			PermissionMode:   config.PermissionAsk,
			Effort:           "medium",
			Workspace:        "/workspace",
			OpenAIConfigured: false,
			Login: func(_ context.Context, _ io.Writer, args []string) (string, error) {
				loginArgs = append([]string(nil), args...)
				return "/home/user/.config/gxx/config.json", nil
			},
			Logout: func(args []string) (string, error) {
				logoutArgs = append([]string(nil), args...)
				return "/home/user/.config/gxx/config.json", nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if len(loginArgs) == 0 || loginArgs[0] != "openai" {
		t.Fatalf("login args = %v", loginArgs)
	}
	if len(logoutArgs) == 0 || logoutArgs[0] != "openai" {
		t.Fatalf("logout args = %v", logoutArgs)
	}
	text := output.String()
	for _, expected := range []string{
		"Run /login",
		"OpenAI login saved",
		"OpenAI login cleared",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
	}
}

func TestRunREPLLoginRequiresProviderWithoutTTY(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/login\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:        "0.0.1",
			Model:          "gpt-5.6-sol",
			PermissionMode: config.PermissionAsk,
			Workspace:      "/workspace",
			Login: func(context.Context, io.Writer, []string) (string, error) {
				t.Fatal("login should not run without a provider")
				return "", nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if !strings.Contains(output.String(), "/login openai") {
		t.Fatalf("output = %q, want provider guidance", output.String())
	}
}

func TestRunREPLPlanExecuteLeavesPlanAndImplements(t *testing.T) {
	model := &replModel{reply: "the plan"}
	plan := true
	ask := true
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("design auth\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:        "0.0.1",
			Model:          "test-model",
			PermissionMode: config.PermissionAsk,
			Workspace:      "/workspace",
			Plan:           true,
			Ask:            true,
			SetAsk: func(next bool) error {
				ask = next
				return nil
			},
			SetPlan: func(next bool) error {
				plan = next
				return nil
			},
			ChoosePlan: func(context.Context) (ui.PlanChoice, error) {
				return ui.PlanExecute, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if plan {
		t.Fatal("execute should leave plan mode")
	}
	if ask {
		t.Fatal("execute should leave ask mode so agent can implement")
	}
	if len(model.prompts) != 2 || model.prompts[0] != "design auth" || model.prompts[1] != ui.ImplementPlanPrompt {
		t.Fatalf("prompts = %#v, want design then implement", model.prompts)
	}
	if !strings.Contains(output.String(), "Leaving plan mode · implementing") {
		t.Fatalf("output = %q, want implement notice", output.String())
	}
}

func TestRunREPLPlanReviseStaysInPlanAndSendsChanges(t *testing.T) {
	model := &replModel{reply: "revised plan"}
	choices := []ui.PlanChoice{ui.PlanRevise, ui.PlanCancel}
	var plan = true
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("design auth\nuse JWT instead\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:        "0.0.1",
			Model:          "test-model",
			PermissionMode: config.PermissionAsk,
			Workspace:      "/workspace",
			Plan:           true,
			SetPlan: func(next bool) error {
				plan = next
				return nil
			},
			ChoosePlan: func(context.Context) (ui.PlanChoice, error) {
				if len(choices) == 0 {
					t.Fatal("ChoosePlan called too many times")
				}
				choice := choices[0]
				choices = choices[1:]
				return choice, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if !plan {
		t.Fatal("revise should stay in plan mode")
	}
	if len(model.prompts) != 2 || model.prompts[0] != "design auth" || model.prompts[1] != "use JWT instead" {
		t.Fatalf("prompts = %#v, want design then revision", model.prompts)
	}
	if !strings.Contains(output.String(), "Describe the changes to the plan.") {
		t.Fatalf("output = %q, want revision hint", output.String())
	}
}

func TestRunREPLPlanCancelKeepsPlanWithoutAnotherTurn(t *testing.T) {
	model := &replModel{reply: "the plan"}
	plan := true
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("design auth\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:        "0.0.1",
			Model:          "test-model",
			PermissionMode: config.PermissionAsk,
			Workspace:      "/workspace",
			Plan:           true,
			SetPlan: func(next bool) error {
				plan = next
				return nil
			},
			ChoosePlan: func(context.Context) (ui.PlanChoice, error) {
				return ui.PlanCancel, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if !plan {
		t.Fatal("cancel should keep plan mode")
	}
	if len(model.prompts) != 1 || model.prompts[0] != "design auth" {
		t.Fatalf("prompts = %#v, want only the plan turn", model.prompts)
	}
}

func TestRunREPLSkipsPlanMenuWithoutChooserOrTerminal(t *testing.T) {
	model := &replModel{reply: "the plan"}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("design auth\n/exit\n"))
	var output bytes.Buffer
	err := ui.RunREPL(
		context.Background(),
		loop,
		input,
		ui.NewRenderer(&output),
		&output,
		ui.REPLSettings{
			Version:        "0.0.1",
			Model:          "test-model",
			PermissionMode: config.PermissionAsk,
			Workspace:      "/workspace",
			Plan:           true,
		},
	)
	if err != nil {
		t.Fatalf("RunREPL() error = %v", err)
	}
	if len(model.prompts) != 1 {
		t.Fatalf("prompts = %#v, want a single turn when the menu is unavailable", model.prompts)
	}
	if strings.Contains(output.String(), "Leaving plan mode") ||
		strings.Contains(output.String(), "Describe the changes") {
		t.Fatalf("output = %q, did not want a plan follow-up", output.String())
	}
}
