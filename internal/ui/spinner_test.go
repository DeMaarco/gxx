package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
)

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		elapsed time.Duration
		want    string
	}{
		{elapsed: 0, want: "0.0s"},
		{elapsed: 1500 * time.Millisecond, want: "1.5s"},
		{elapsed: 12 * time.Second, want: "12.0s"},
		{elapsed: 61 * time.Second, want: "1m01s"},
		{elapsed: 2*time.Minute + 5*time.Second, want: "2m05s"},
	}
	for _, test := range tests {
		if got := formatElapsed(test.elapsed); got != test.want {
			t.Fatalf("formatElapsed(%s) = %q, want %q", test.elapsed, got, test.want)
		}
	}
}

func TestToolHintUsesPathCommandOrQuery(t *testing.T) {
	if got := toolHint([]byte(`{"path":"README.md"}`)); got != "README.md" {
		t.Fatalf("path hint = %q", got)
	}
	if got := toolHint([]byte(`{"command":"go test ./..."}`)); got != "go test ./..." {
		t.Fatalf("command hint = %q", got)
	}
	if got := toolHint([]byte(`{"query":"TODO"}`)); got != "TODO" {
		t.Fatalf("query hint = %q", got)
	}
	if got := toolHint([]byte(`{"content":"secret"}`)); got != "" {
		t.Fatalf("content leaked as hint: %q", got)
	}
}

func TestLiveLineShowsSpinnerLabelAndElapsed(t *testing.T) {
	got := liveLine(false, 0, "thinking", 1500*time.Millisecond)
	for _, expected := range []string{spinnerFrames[0], "thinking", "1.5s"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("liveLine = %q, want %q", got, expected)
		}
	}
}

func TestLiveRendererShowsThinkingThenToolElapsed(t *testing.T) {
	var output bytes.Buffer
	frozen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := frozen
	renderer := NewRenderer(&output)
	renderer.live = true
	renderer.spinEvery = 0
	renderer.now = func() time.Time { return now }

	renderer.StartTurn()
	if !strings.Contains(output.String(), "thinking") || !strings.Contains(output.String(), "0.0s") {
		t.Fatalf("start = %q, want thinking spinner", output.String())
	}

	now = frozen.Add(2 * time.Second)
	renderer.Event(agent.Event{
		Kind: agent.EventToolStarted,
		ToolCall: &agent.ToolCall{
			Name:      "read_file",
			Arguments: []byte(`{"path":"README.md"}`),
		},
	})
	text := output.String()
	if !strings.Contains(text, "read_file") || !strings.Contains(text, "README.md") {
		t.Fatalf("tool start = %q, want read_file hint", text)
	}
	if strings.Contains(text, "→") {
		t.Fatalf("live renderer printed a static arrow: %q", text)
	}

	now = frozen.Add(3500 * time.Millisecond)
	renderer.Event(agent.Event{
		Kind:   agent.EventToolDone,
		Result: &agent.ToolResult{Name: "read_file", DurationMS: 1500},
	})
	renderer.Finish("")
	text = output.String()
	if !strings.Contains(text, "✓") || !strings.Contains(text, "read_file") || !strings.Contains(text, "(1500ms)") {
		t.Fatalf("tool done = %q, want completed tool status", text)
	}
}

func TestLiveRendererStopsThinkingWhenModelStreams(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output)
	renderer.live = true
	renderer.spinEvery = 0

	renderer.StartTurn()
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: "hello"})
	renderer.Finish("")
	if strings.HasSuffix(strings.TrimRight(output.String(), "\n"), "thinking") {
		t.Fatalf("streamed text left thinking spinner active: %q", output.String())
	}
	if !strings.Contains(output.String(), "hello") {
		t.Fatalf("output = %q, want streamed hello", output.String())
	}
}

func TestNonLiveRendererKeepsStaticToolArrow(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output)
	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind:     agent.EventToolStarted,
		ToolCall: &agent.ToolCall{Name: "search_files", Arguments: []byte(`{"query":"TODO"}`)},
	})
	renderer.Event(agent.Event{
		Kind:   agent.EventToolDone,
		Result: &agent.ToolResult{Name: "search_files", DurationMS: 9},
	})
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "→") || !strings.Contains(text, "search_files") || !strings.Contains(text, "TODO") {
		t.Fatalf("static tool line = %q", text)
	}
}
