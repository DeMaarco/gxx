package ui

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"gxx/internal/agent"
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
	renderer := NewRenderer(&output)

	err := RunREPL(
		context.Background(),
		loop,
		input,
		renderer,
		&output,
		REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   PermissionAsk,
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
		"◆ gxx  0.0.1",
		">",
		"test-model · ask · medium · 272k",
		"/clear",
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

func TestRunREPLConfiguresAPIKeyWithoutEchoingIt(t *testing.T) {
	model := &replModel{}
	loop := &agent.Loop{Model: model, Executor: emptyExecutor{}, MaxSteps: 2}
	input := bufio.NewReader(strings.NewReader("/config\n/exit\n"))
	var output bytes.Buffer
	var saved string

	err := RunREPL(
		context.Background(),
		loop,
		input,
		NewRenderer(&output),
		&output,
		REPLSettings{
			Version:          "0.0.1",
			Model:            "test-model",
			PermissionMode:   PermissionAsk,
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

func TestReadLineHonorsCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := readLine(ctx, bufio.NewReader(reader))
	if err != context.Canceled {
		t.Fatalf("readLine() error = %v, want context.Canceled", err)
	}
}

func TestRendererColorsToolEventsWhenEnabled(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRendererWithColor(&output, true)
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
	renderer := NewRenderer(&output)
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
	var synced []REPLSettings

	err := RunREPL(
		context.Background(),
		loop,
		input,
		NewRenderer(&output),
		&output,
		REPLSettings{
			Version:          "0.0.1",
			Model:            "gpt-5.6-sol",
			PermissionMode:   PermissionAsk,
			Effort:           "medium",
			Context:          "272k",
			Workspace:        "/workspace",
			APIKeyConfigured: true,
			SyncSession: func(session REPLSettings) error {
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
		"gpt-5.6-terra · ask · high · 272k · fast",
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
