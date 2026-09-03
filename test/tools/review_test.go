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

package tools_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/tools"
)

func TestReviewFileFlagsHTMLDefects(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "index.html", `<!doctype html>
<html>
<body>
<section id="inicio"></section>
<div class="moon-scene" style="z-index: -1"></div>
<a href="#carta">one</a>
<a href="#carta">two</a>
<a href="#carta">three</a>
<a href="#missing">gone</a>
</body>
</html>
`)
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  8192,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("r", "review_file", map[string]any{"path": "index.html"}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("review_file failed: %s", result.Output)
	}
	for _, want := range []string{
		"[review_file index.html",
		"negative z-index",
		"in-page link #missing has no matching id",
		"3 in-page links share the same target #carta",
		"Think through remaining defects",
		"do not skip it because there are no automated tests",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("review = %q, want %q", result.Output, want)
		}
	}
}

func TestReviewFileFlagsMissingTitleAndViewport(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "landing.html", `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
</head>
<body>
  <h1>Shop</h1>
</body>
</html>
`)
	writeTestFile(t, root, "ok.html", `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Shop</title>
</head>
<body>
  <h1>Shop</h1>
</body>
</html>
`)
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	missing := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("r", "review_file", map[string]any{"path": "landing.html"}),
	}, nil)[0]
	if missing.IsError {
		t.Fatalf("review failed: %s", missing.Output)
	}
	if !strings.Contains(missing.Output, "missing a <title>") {
		t.Fatalf("review = %q, want missing title", missing.Output)
	}
	if !strings.Contains(missing.Output, "missing a viewport meta tag") {
		t.Fatalf("review = %q, want missing viewport", missing.Output)
	}
	ok := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("r", "review_file", map[string]any{"path": "ok.html"}),
	}, nil)[0]
	if ok.IsError || !strings.Contains(ok.Output, "findings: none") {
		t.Fatalf("complete document review = %q, want no findings", ok.Output)
	}
}

func TestReviewFileCleanGoFileHasNoFindings(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("r", "review_file", map[string]any{"path": "main.go"}),
	}, nil)[0]
	if result.IsError || !strings.Contains(result.Output, "findings: none") {
		t.Fatalf("review = %#v, want no findings", result)
	}
	if strings.Contains(result.Output, "static HTML") {
		t.Fatalf("go review mentioned HTML validation: %q", result.Output)
	}
}

func TestApplyPatchAttachesReview(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  8192,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("w", "apply_patch", map[string]any{
			"changes": []map[string]any{{
				"path":    "page.html",
				"action":  "add",
				"content": "<style>.moon { z-index: -1; }</style>\n",
			}},
		}),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("apply_patch failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Applied patch to 1 file(s): page.html") {
		t.Fatalf("output = %q, want applied prefix", result.Output)
	}
	if !strings.Contains(result.Output, "[review_file page.html") || !strings.Contains(result.Output, "negative z-index") {
		t.Fatalf("output = %q, want attached review", result.Output)
	}
}

func TestReviewFileAvailableInAskAndPlan(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "ok.txt", "hello\n")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	found := false
	for _, def := range registry.Definitions() {
		if def.Name == "review_file" {
			found = true
			if !def.ReadOnly {
				t.Fatal("review_file should be ReadOnly")
			}
		}
	}
	if !found {
		t.Fatal("agent mode missing review_file")
	}
	for _, mode := range []string{"ask", "plan"} {
		t.Run(mode, func(t *testing.T) {
			registry.SetAsk(false)
			registry.SetPlan(false)
			if mode == "ask" {
				registry.SetAsk(true)
			} else {
				registry.SetPlan(true)
			}
			found := false
			for _, def := range registry.Definitions() {
				if def.Name == "review_file" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s mode hid review_file", mode)
			}
			result := registry.Execute(context.Background(), []agent.ToolCall{
				toolCall("r", "review_file", map[string]any{"path": "ok.txt"}),
			}, nil)[0]
			if result.IsError || !strings.Contains(result.Output, "findings: none") {
				t.Fatalf("%s review_file = %#v", mode, result)
			}
		})
	}
}
