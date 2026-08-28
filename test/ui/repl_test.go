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
	"testing"
	"time"

	"gxx/internal/ui"

	"gxx/internal/agent"
	"gxx/internal/config"
)

type replModel struct {
	resetCount int
}

func (m *replModel) Respond(
	_ context.Context,
	_ agent.ModelInput,
	_ []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: "world"})
	return agent.ModelResponse{Text: "world"}, nil
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
		"test-model · ask · medium · 272k · 0%",
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
		"session",
		"remaining    $8.50",
		"99 / 100",
		"900 / 1,000",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output = %q, want %q", text, expected)
		}
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
	for _, expected := range []string{"Run /config", "API key saved", "Conversation cleared"} {
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
		"* gpt-5.6-sol",
		"model gpt-5.6-terra · context 272k · effort medium · fast off",
		"model gpt-5.6-terra · context 272k · effort high · fast on",
		"Conversation cleared.",
		"gpt-5.6-terra · ask · high · 272k · 0% · fast",
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
		"permission ask · confirm every file change and command; a-xxxx allows a command for the session",
		"* ask",
		"auto-writes",
		"permission auto-writes · file changes run without confirmation; commands still ask",
		"permission auto · file changes and commands run without confirmation",
		"gpt-5.6-sol · auto · medium · 272k · 0%",
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
