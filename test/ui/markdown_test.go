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

	"gxx/internal/agent"
	"gxx/internal/ui"
)

func TestFormatMarkdownStripsMarkers(t *testing.T) {
	got := ui.FormatMarkdown(false, "1. **Menú** en `script.js`\n- item")
	if strings.Contains(got, "**") || strings.Contains(got, "`") {
		t.Fatalf("markdown = %q, want markers stripped", got)
	}
	if !strings.Contains(got, "Menú") || !strings.Contains(got, "script.js") {
		t.Fatalf("markdown = %q, want inner text", got)
	}
	if !strings.Contains(got, "• ") || !strings.Contains(got, "item") {
		t.Fatalf("markdown = %q, want bullet", got)
	}
}

func TestFormatMarkdownUsesColors(t *testing.T) {
	got := ui.FormatMarkdown(true, "**Menú** y `script.js`")
	if strings.Contains(got, "**") || strings.Contains(got, "`") {
		t.Fatalf("colored markdown = %q, want markers stripped", got)
	}
	if !strings.Contains(got, "\x1b[1m") || !strings.Contains(got, "Menú") {
		t.Fatalf("colored markdown = %q, want bold", got)
	}
	if !strings.Contains(got, "\x1b[33m") || !strings.Contains(got, "script.js") {
		t.Fatalf("colored markdown = %q, want yellow code", got)
	}
}

const markdownSample = "## Errores confirmados\n\n" +
	"### 1. Faltan archivos — Crítico\n\n" +
	"- `index.html:65` — roto\n" +
	"  - anidado\n\n" +
	"```js\nevent.preventDefault();\nform.reset();\n```\n\n" +
	"1. primero **importante**\n" +
	"2. segundo\n\n" +
	"---\n\n" +
	"> una cita\n\n" +
	"Fin del **informe**.\n"

func TestFormatMarkdownRendersBlockConstructs(t *testing.T) {
	got := ui.FormatMarkdown(false, markdownSample)
	for _, marker := range []string{"##", "```", "**", "`", "---"} {
		if strings.Contains(got, marker) {
			t.Fatalf("markdown = %q, want %q consumed", got, marker)
		}
	}
	for _, want := range []string{
		"Errores confirmados",
		"• index.html:65 — roto",
		"  • anidado",
		"  event.preventDefault();",
		"1. primero importante",
		"│ una cita",
		"Fin del informe.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("markdown = %q, want %q", got, want)
		}
	}
	// The language tag of a fence is not content.
	if strings.Contains(got, "js\n") {
		t.Fatalf("markdown = %q, want the fence info string dropped", got)
	}
}

func TestFormatMarkdownStylesHeadingsAndCodeBlocks(t *testing.T) {
	got := ui.FormatMarkdown(true, "## Título\n\n```go\nx := 1\n```\n")
	if !strings.Contains(got, ui.ColorBold+ui.ColorCyan+"Título") {
		t.Fatalf("markdown = %q, want a styled heading", got)
	}
	if !strings.Contains(got, ui.ColorDim+"x := 1") {
		t.Fatalf("markdown = %q, want a dim code block", got)
	}
}

func TestFormatMarkdownKeepsInlineMarkersOnOneLine(t *testing.T) {
	// An unpaired backtick used to open a span that swallowed everything up
	// to the next one, including whole fenced blocks.
	got := ui.FormatMarkdown(false, "usa `npm y luego\notra línea\n")
	if !strings.Contains(got, "usa `npm y luego") || !strings.Contains(got, "otra línea") {
		t.Fatalf("markdown = %q, want both lines intact", got)
	}
}

func TestMarkdownStreamsIdenticallyByteByByte(t *testing.T) {
	var whole bytes.Buffer
	full := ui.NewRendererWithColor(&whole, true)
	full.StartTurn()
	full.Event(agent.Event{Kind: agent.EventTextDelta, Text: markdownSample})
	full.Finish("")

	var chunked bytes.Buffer
	split := ui.NewRendererWithColor(&chunked, true)
	split.StartTurn()
	for i := range len(markdownSample) {
		split.Event(agent.Event{Kind: agent.EventTextDelta, Text: markdownSample[i : i+1]})
	}
	split.Finish("")

	// A delta boundary must not change a single byte of the rendering.
	if whole.String() != chunked.String() {
		t.Fatalf("streamed = %q, want %q", chunked.String(), whole.String())
	}
}

func TestRendererHoldsRuneSplitAcrossDeltas(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRendererWithColor(&output, false)
	renderer.StartTurn()
	accented := "Crítico"
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: accented[:3]})
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: accented[3:]})
	renderer.Finish("")
	if !strings.Contains(output.String(), accented) {
		t.Fatalf("output = %q, want %q rebuilt from a split rune", output.String(), accented)
	}
}

func TestRendererRendersMarkdownAsItStreams(t *testing.T) {
	var output bytes.Buffer
	renderer := ui.NewRendererWithColor(&output, true)
	renderer.StartTurn()
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: "1. **Menú"})
	if strings.Contains(output.String(), "**") || strings.Contains(output.String(), "Menú") {
		t.Fatalf("partial bold leaked: %q", output.String())
	}
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: " de especificaciones** en `scr"})
	if strings.Contains(output.String(), "**") {
		t.Fatalf("bold markers leaked: %q", output.String())
	}
	if !strings.Contains(output.String(), "Menú de especificaciones") {
		t.Fatalf("streamed bold = %q", output.String())
	}
	renderer.Event(agent.Event{Kind: agent.EventTextDelta, Text: "ipt.js`.\n- borde duplicado"})
	renderer.Finish("")
	text := output.String()
	if strings.Contains(text, "**") || strings.Contains(text, "`") {
		t.Fatalf("finished markdown leaked markers: %q", text)
	}
	if !strings.Contains(text, "script.js") || !strings.Contains(text, "• ") || !strings.Contains(text, "borde duplicado") {
		t.Fatalf("finished markdown = %q", text)
	}
}
