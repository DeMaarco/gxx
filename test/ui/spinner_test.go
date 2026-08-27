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
	if got := ui.ToolHint([]byte(`{"changes":[{"path":"a.go"},{"path":"b.go"}]}`)); got != "a.go · 2 files" {
		t.Fatalf("patch hint = %q", got)
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
	if !strings.Contains(text, "reading") || !strings.Contains(text, "README.md") {
		t.Fatalf("tool start = %q, want reading hint", text)
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
	if !strings.Contains(label, "×4") || !strings.Contains(label, "reading") || !strings.Contains(label, "internal/tools/registry.go") {
		t.Fatalf("compact label = %q, want grouped reading", label)
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
	if !ui.HoldModelText("to=functions.apply_patch code:") {
		t.Fatal("leaked function call header should be held")
	}
	if !ui.HoldModelText("to=") {
		t.Fatal("incomplete to= prefix should be held")
	}
}

func TestRendererDropsLeakedFunctionCallText(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventTextDelta,
		Text: "Revisando el modal. to=functions.apply_patch code:\n",
	})
	renderer.Event(agent.Event{
		Kind: agent.EventTextDelta,
		Text: `{"changes":[{"path":"styles.css","action":"update","old_text":"a","new_text":"b"}]} Ахада to=functions.read_file code:` + "\n",
	})
	renderer.Event(agent.Event{
		Kind: agent.EventTextDelta,
		Text: `{"limit_lines":10,"offset_line":1500,"path":"styles.css"}` + "\nHaré un último ajuste.\n",
	})
	renderer.Finish("")
	text := output.String()
	for _, leaked := range []string{
		"to=functions", "old_text", "limit_lines", "Ахада", "changes",
	} {
		if strings.Contains(text, leaked) {
			t.Fatalf("leaked tool text %q was printed: %q", leaked, text)
		}
	}
	if !strings.Contains(text, "Revisando el modal.") {
		t.Fatalf("output = %q, want prose before leak", text)
	}
	if !strings.Contains(text, "Haré un último ajuste.") {
		t.Fatalf("output = %q, want prose after leak", text)
	}
}

func TestRendererKeepsSearchCountsOnTheMatchingLine(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind:     agent.EventToolStarted,
		ToolCall: &agent.ToolCall{ID: "a", Name: "search_files", Arguments: []byte(`{"query":"alpha"}`)},
	})
	renderer.Event(agent.Event{
		Kind:     agent.EventToolStarted,
		ToolCall: &agent.ToolCall{ID: "b", Name: "search_files", Arguments: []byte(`{"query":"beta"}`)},
	})
	renderer.Event(agent.Event{
		Kind: agent.EventToolDone,
		Result: &agent.ToolResult{
			CallID: "a", Name: "search_files", DurationMS: 1, Output: "No matches found.",
		},
	})
	renderer.Event(agent.Event{
		Kind: agent.EventToolDone,
		Result: &agent.ToolResult{
			CallID: "b", Name: "search_files", DurationMS: 2,
			Output: "index.html:15:beta\nindex.html:32:beta",
		},
	})
	renderer.Finish("")
	text := output.String()
	alpha := strings.Index(text, "✓ search_files  alpha")
	beta := strings.Index(text, "✓ search_files  beta")
	none := strings.Index(text, "no matches")
	two := strings.Index(text, "2 matches")
	if alpha < 0 || beta < 0 || none < 0 || two < 0 {
		t.Fatalf("search lines = %q", text)
	}
	if !(alpha < none && none < beta && beta < two) {
		t.Fatalf("search extras were detached: %q", text)
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
	if !strings.Contains(text, "→") || !strings.Contains(text, "searching") || !strings.Contains(text, "TODO") {
		t.Fatalf("static tool line = %q", text)
	}
}

func TestRendererShowsSearchProgressAndMatches(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventToolStarted,
		ToolCall: &agent.ToolCall{
			ID:        "s1",
			Name:      "search_files",
			Arguments: []byte(`{"query":"TODO"}`),
		},
	})
	renderer.Event(agent.Event{
		Kind:     agent.EventToolProgress,
		ToolCall: &agent.ToolCall{ID: "s1", Name: "search_files"},
		Text:     "internal/ui/repl.go",
	})
	live := output.String()
	if !strings.Contains(live, "searching") || !strings.Contains(live, "internal/ui/repl.go") {
		t.Fatalf("search progress = %q, want live path", live)
	}

	renderer.Event(agent.Event{
		Kind: agent.EventToolDone,
		Result: &agent.ToolResult{
			CallID:     "s1",
			Name:       "search_files",
			DurationMS: 12,
			Output:     "internal/ui/repl.go:88:TODO here\ninternal/tools/fs.go:250:TODO there",
		},
	})
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "✓ search_files") {
		t.Fatalf("search done = %q, want completed search", text)
	}
	if !strings.Contains(text, "2 matches") {
		t.Fatalf("search done = %q, want match count", text)
	}
	if strings.Contains(text, "TODO here") {
		t.Fatalf("search done = %q, want count not match bodies", text)
	}
}

func TestRendererShowsColoredPatchLines(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRendererWithColor(&output, true)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventToolStarted,
		ToolCall: &agent.ToolCall{
			Name:      "apply_patch",
			Arguments: []byte(`{"changes":[{"path":"file.go","action":"update","old_text":"before","new_text":"after"}]}`),
		},
	})
	if !strings.Contains(output.String(), "writing") {
		t.Fatalf("patch start = %q, want writing", output.String())
	}
	if !strings.Contains(output.String(), "+1") || !strings.Contains(output.String(), "-1") {
		t.Fatalf("patch start = %q, want live line counts", output.String())
	}
	renderer.Event(agent.Event{
		Kind: agent.EventToolDone,
		Result: &agent.ToolResult{
			Name:       "apply_patch",
			DurationMS: 8,
			Output:     "Applied patch to 1 file(s): file.go",
		},
	})
	renderer.Finish("")
	text := output.String()
	if !strings.Contains(text, "✓") || !strings.Contains(text, "apply_patch") || !strings.Contains(text, "file.go") {
		t.Fatalf("patch done = %q, want apply_patch", text)
	}
	if !strings.Contains(text, "\x1b[31m") || !strings.Contains(text, "-1") {
		t.Fatalf("patch done = %q, want red deleted count", text)
	}
	if !strings.Contains(text, "\x1b[32m") || !strings.Contains(text, "+1") {
		t.Fatalf("patch done = %q, want green inserted count", text)
	}
	if strings.Contains(text, "- before") || strings.Contains(text, "+ after") {
		t.Fatalf("patch done = %q, want counts not file contents", text)
	}
}

func TestRendererShowsPatchLinesPerFile(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind: agent.EventToolStarted,
		ToolCall: &agent.ToolCall{
			Name:      "apply_patch",
			Arguments: []byte(`{"changes":[{"path":"a.go","action":"update","old_text":"old a","new_text":"new a"},{"path":"b.go","action":"add","content":"hello\n"}]}`),
		},
	})
	renderer.Event(agent.Event{
		Kind:   agent.EventToolDone,
		Result: &agent.ToolResult{Name: "apply_patch", DurationMS: 5},
	})
	renderer.Finish("")
	text := output.String()
	for _, expected := range []string{"a.go", "b.go", "+1", "-1", "+2"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("multi-file patch = %q, want %q", text, expected)
		}
	}
	if strings.Contains(text, "old a") || strings.Contains(text, "hello") {
		t.Fatalf("multi-file patch = %q, want counts not file contents", text)
	}
}

func TestRendererShowsListFileCount(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRenderer(&output)
	renderer.SetLive(true)
	renderer.SetSpinEvery(0)

	renderer.StartTurn()
	renderer.Event(agent.Event{
		Kind:     agent.EventToolStarted,
		ToolCall: &agent.ToolCall{Name: "list_files", Arguments: []byte(`{"path":"."}`)},
	})
	renderer.Event(agent.Event{
		Kind: agent.EventToolProgress,
		Text: "internal/ui/",
	})
	if !strings.Contains(output.String(), "listing") || !strings.Contains(output.String(), "internal/ui/") {
		t.Fatalf("list progress = %q", output.String())
	}
	renderer.Event(agent.Event{
		Kind: agent.EventToolDone,
		Result: &agent.ToolResult{
			Name:       "list_files",
			DurationMS: 3,
			Output:     "go.mod\nREADME.md\ninternal/ui/repl.go",
		},
	})
	renderer.Finish("")
	if !strings.Contains(output.String(), "3 files") {
		t.Fatalf("list done = %q, want file count", output.String())
	}
}
