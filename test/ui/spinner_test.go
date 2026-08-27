package ui_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"gxx/internal/ui"

	"gxx/internal/agent"
)

func TestLiveLineShowsTokens(t *testing.T) {
	got := ui.LiveLine(false, 0, "thinking", 400*time.Millisecond, agent.Usage{TotalTokens: 12400}, 80)
	for _, expected := range []string{ui.SpinnerFrames[0], "thinking", "0.4s", "12.4k tok"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("liveLine = %q, want %q", got, expected)
		}
	}
}

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
		if got := ui.FormatElapsed(test.elapsed); got != test.want {
			t.Fatalf("formatElapsed(%s) = %q, want %q", test.elapsed, got, test.want)
		}
	}
}

func TestToolHintUsesPathCommandOrQuery(t *testing.T) {
	if got := ui.ToolHint([]byte(`{"path":"README.md"}`)); got != "README.md" {
		t.Fatalf("path hint = %q", got)
	}
	if got := ui.ToolHint([]byte(`{"command":"go test ./..."}`)); got != "go test ./..." {
		t.Fatalf("command hint = %q", got)
	}
	if got := ui.ToolHint([]byte(`{"query":"TODO"}`)); got != "TODO" {
		t.Fatalf("query hint = %q", got)
	}
	if got := ui.ToolHint([]byte(`{"content":"secret"}`)); got != "" {
		t.Fatalf("content leaked as hint: %q", got)
	}
}

func TestLiveLineShowsSpinnerLabelAndElapsed(t *testing.T) {
	got := ui.LiveLine(false, 0, "thinking", 1500*time.Millisecond, agent.Usage{}, 80)
	for _, expected := range []string{ui.SpinnerFrames[0], "thinking", "1.5s"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("liveLine = %q, want %q", got, expected)
		}
	}
}

func TestLiveRendererShowsThinkingThenToolElapsed(t *testing.T) {
	var output bytes.Buffer
	frozen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := frozen
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)
	renderer.SetNow(func() time.Time { return now })

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
	if !strings.Contains(text, "✓") || !strings.Contains(text, "read_file") ||
		!strings.Contains(text, "README.md") || !strings.Contains(text, "(1.5s)") {
		t.Fatalf("tool done = %q, want completed tool status with path", text)
	}
}

func TestLiveRendererStopsThinkingWhenModelStreams(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

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

func TestLiveLineFitsTerminalWidth(t *testing.T) {
	label := ui.CompactRunningLabel(false, []ui.RunningTool{
		{Name: "read_file", Hint: "internal/tools/registry.go"},
		{Name: "read_file", Hint: "internal/ui/repl.go"},
		{Name: "read_file", Hint: "internal/agent/loop.go"},
		{Name: "read_file", Hint: "internal/app/app.go"},
	})
	if !strings.Contains(label, "×4") || !strings.Contains(label, "internal/tools/registry.go") {
		t.Fatalf("compact label = %q, want grouped read_file", label)
	}
	got := ui.LiveLine(false, 0, label, time.Second, agent.Usage{}, 40)
	body := strings.TrimPrefix(got, ui.ClearLine)
	if ui.VisibleWidth(body) >= 40 {
		t.Fatalf("live line width = %d, want < 40: %q", ui.VisibleWidth(body), body)
	}
	if strings.Contains(body, "\n") {
		t.Fatalf("live line wrapped: %q", body)
	}
}

func TestLiveRendererKeepsDoneLineOnItsOwnRow(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)
	renderer.SetColumns(36)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventToolStarted,
		ToolCall: &agent.ToolCall{
			ID:        "1",
			Name:      "read_file",
			Arguments: []byte(`{"path":"cmd/gxx/main.go"}`),
		},
	})
	renderer.Event(agent.Event{
		Kind: agent.EventToolStarted,
		ToolCall: &agent.ToolCall{
			ID:        "2",
			Name:      "read_file",
			Arguments: []byte(`{"path":"internal/ui/repl.go"}`),
		},
	})
	renderer.Event(agent.Event{
		Kind:   agent.EventToolDone,
		Result: &agent.ToolResult{CallID: "1", Name: "read_file", DurationMS: 4},
	})
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "✓ read_file  cmd/gxx/main.go  (4ms)") {
		t.Fatalf("done line = %q", text)
	}
	if strings.Contains(text, "intern✓") || strings.Contains(text, "go · read_file") {
		t.Fatalf("done line collided with spinner remnant: %q", text)
	}
}

func TestFormatToolDuration(t *testing.T) {
	if got := ui.FormatToolDuration(0); got != "<1ms" {
		t.Fatalf("0ms = %q", got)
	}
	if got := ui.FormatToolDuration(4); got != "4ms" {
		t.Fatalf("4ms = %q", got)
	}
	if got := ui.FormatToolDuration(1500); got != "1.5s" {
		t.Fatalf("1500ms = %q", got)
	}
}

func TestLiveRendererShowsUsageOnSpinnerAndFinish(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventUsage,
		Usage: agent.Usage{
			InputTokens:     8100,
			OutputTokens:    4300,
			ReasoningTokens: 1100,
			TotalTokens:     12400,
		},
	})
	if !strings.Contains(output.String(), "12.4k tok") {
		t.Fatalf("live usage = %q, want spinner tokens", output.String())
	}

	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: "ok"})
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "12.4k tok · 8.1k in · 4.3k out · 1.1k reason") {
		t.Fatalf("finish usage = %q, want turn footer", text)
	}
}

func TestRendererGroupsSequentialReadsAcrossThinking(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	for i, path := range []string{"go.mod", "README.md", "internal/ui/repl.go"} {
		renderer.Event(agent.Event{
			Kind: agent.EventToolStarted,
			ToolCall: &agent.ToolCall{
				ID:        string(rune('a' + i)),
				Name:      "read_file",
				Arguments: []byte(`{"path":"` + path + `"}`),
			},
		})
		renderer.Event(agent.Event{
			Kind:   agent.EventToolDone,
			Result: &agent.ToolResult{CallID: string(rune('a' + i)), Name: "read_file", DurationMS: 1},
		})
	}
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
	if !strings.Contains(text, "✓ read_file ×3  go.mod, README.md, internal/ui/repl.go  (1ms)") {
		t.Fatalf("sequential reads = %q", text)
	}
	if !strings.Contains(text, "✓ search_files  TODO  (9ms)") {
		t.Fatalf("search line = %q", text)
	}
	if strings.Count(text, "✓ read_file") != 1 {
		t.Fatalf("read_file was not grouped: %q", text)
	}
}

func TestRendererGroupsConsecutiveReadFiles(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	for i, path := range []string{"go.mod", "README.md", "cmd/gxx/main.go", "internal/app/app.go"} {
		renderer.Event(agent.Event{
			Kind: agent.EventToolStarted,
			ToolCall: &agent.ToolCall{
				ID:        string(rune('1' + i)),
				Name:      "read_file",
				Arguments: []byte(`{"path":"` + path + `"}`),
			},
		})
	}
	for i, path := range []string{"go.mod", "README.md", "cmd/gxx/main.go", "internal/app/app.go"} {
		renderer.Event(agent.Event{
			Kind: agent.EventToolDone,
			Result: &agent.ToolResult{
				CallID:     string(rune('1' + i)),
				Name:       "read_file",
				DurationMS: 1,
			},
		})
		_ = path
	}
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "✓ read_file ×4  go.mod, README.md, cmd/gxx/main.go, …  (1ms)") {
		t.Fatalf("grouped reads = %q", text)
	}
	if strings.Count(text, "✓") != 1 {
		t.Fatalf("grouped reads printed multiple checks: %q", text)
	}
}

func TestRendererDropsLeakedToolJSON(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventTextDelta,
		Text: `{"command":"true","timeout_seconds":1}`,
	})
	renderer.Event(agent.Event{
		Kind: agent.EventTextDelta,
		Text: `{"limit_lines":500,"offset_line":1,"path":"internal/agent/prompt_test.go"}`,
	})
	renderer.Event(agent.Event{
		Kind:     agent.EventToolStarted,
		ToolCall: &agent.ToolCall{Name: "read_file", Arguments: []byte(`{"path":"internal/agent/prompt_test.go"}`)},
	})
	renderer.Event(agent.Event{
		Kind:   agent.EventToolDone,
		Result: &agent.ToolResult{Name: "read_file", DurationMS: 1},
	})
	renderer.Finish(`{"path":"internal/agent/prompt_test.go"}`)
	text := output.String()
	if strings.Contains(text, "timeout_seconds") || strings.Contains(text, "limit_lines") {
		t.Fatalf("leaked tool JSON was printed: %q", text)
	}
	if !strings.Contains(text, "✓ read_file  internal/agent/prompt_test.go") {
		t.Fatalf("output = %q, want completed read_file", text)
	}
}

func TestHoldModelTextDetectsToolJSON(t *testing.T) {
	if !ui.HoldModelText(`{"command":"true","timeout_seconds":1}`) {
		t.Fatal("command JSON should be held")
	}
	if !ui.HoldModelText(`{"limit_lines":500,"offset_line":1,"path":"internal/agent/prompt_test.go"}`) {
		t.Fatal("read_file JSON should be held")
	}
	if !ui.HoldModelText(`{"path":"x"`) {
		t.Fatal("incomplete JSON should be held")
	}
	if ui.HoldModelText("The project uses apply_patch.") {
		t.Fatal("prose should not be held")
	}
	if ui.HoldModelText(`{"name":"gxx","version":"0.0.2"}`) {
		t.Fatal("unrelated JSON should stream")
	}
}

func TestNonLiveRendererKeepsStaticToolArrow(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
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
