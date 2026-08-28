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
