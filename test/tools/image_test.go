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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/tools"
)

func TestGenerateImageWritesApprovedFile(t *testing.T) {
	root := t.TempDir()
	approver := &staticApprover{approved: true}
	var seen tools.ImageRequest
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(_ context.Context, req tools.ImageRequest) (tools.ImageResult, error) {
			seen = req
			return tools.ImageResult{
				Data:    []byte("png-bytes"),
				Model:   req.Model,
				Size:    "1024x1024",
				Quality: "high",
				Format:  "png",
			}, nil
		},
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(map[string]any{
			"prompt": "a red square icon",
			"path":   "assets/logo",
		})),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("generate_image failed: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Wrote assets/logo.png") {
		t.Fatalf("output = %q, want destination path", result.Output)
	}
	if !strings.Contains(result.Output, "gpt-image-2") || !strings.Contains(result.Output, "1024x1024") {
		t.Fatalf("output = %q, want model and size", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "assets", "logo.png"), "png-bytes")
	if seen.Prompt != "a red square icon" || seen.Model != "gpt-image-2" || seen.Format != "png" {
		t.Fatalf("request = %+v", seen)
	}
	if len(approver.actions) != 1 ||
		approver.actions[0].Kind != approval.KindWrite ||
		!strings.Contains(approver.actions[0].Preview, "assets/logo.png") ||
		!strings.Contains(approver.actions[0].Preview, "a red square icon") {
		t.Fatalf("approval actions = %#v", approver.actions)
	}
}

func TestGenerateImageDeniedDoesNotCallAPI(t *testing.T) {
	root := t.TempDir()
	called := false
	registry := newTestRegistry(t, root, &staticApprover{approved: false}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			called = true
			return tools.ImageResult{Data: []byte("nope")}, nil
		},
	})

	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(map[string]any{"path": "out.png"})),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "permission denied") {
		t.Fatalf("result = %+v, want denied", result)
	}
	if called {
		t.Fatal("image API was called before approval")
	}
	if _, err := os.Stat(filepath.Join(root, "out.png")); !os.IsNotExist(err) {
		t.Fatalf("denied write created a file: %v", err)
	}
}

func TestGenerateImageDoesNotStartBeforeApproval(t *testing.T) {
	root := t.TempDir()
	var events []agent.Event
	var mu sync.Mutex
	approver := approverFunc(func(_ context.Context, _ approval.Action) (approval.Decision, error) {
		mu.Lock()
		defer mu.Unlock()
		for _, event := range events {
			if event.Kind == agent.EventToolStarted {
				t.Fatal("tool started before approval")
			}
		}
		return approval.Decision{Approved: true}, nil
	})
	registry := newTestRegistry(t, root, approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			return tools.ImageResult{Data: []byte("ok")}, nil
		},
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(map[string]any{"path": "icon.webp", "output_format": "webp"})),
	}, func(event agent.Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})[0]
	if result.IsError {
		t.Fatalf("generate_image failed: %s", result.Output)
	}
}

func TestGenerateImageNeedsAPIKey(t *testing.T) {
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(nil)),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "OPENAI_API_KEY") {
		t.Fatalf("result = %+v, want API key error", result)
	}
}

func TestGenerateImageRejectsSensitivePath(t *testing.T) {
	called := false
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			called = true
			return tools.ImageResult{Data: []byte("nope")}, nil
		},
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(map[string]any{"path": ".env.png"})),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "sensitive path") {
		t.Fatalf("result = %+v, want sensitive path", result)
	}
	if called {
		t.Fatal("image API was called for a sensitive path")
	}
}

func TestGenerateImageValidatesModelSizeAndFormat(t *testing.T) {
	registry := newTestRegistry(t, t.TempDir(), &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			t.Fatal("image API should not run")
			return tools.ImageResult{}, nil
		},
	})

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"dall-e", map[string]any{"model": "dall-e-3"}, "unsupported image model"},
		{"bad size", map[string]any{"size": "10x10"}, "divisible by 16"},
		{"aspect", map[string]any{"size": "64x16"}, "aspect ratio"},
		{"format mismatch", map[string]any{"path": "logo.png", "output_format": "jpeg"}, "does not match"},
		{"bad extension", map[string]any{"path": "logo.gif"}, "path must end"},
		{"transparent jpeg", map[string]any{"path": "logo.jpg", "background": "transparent"}, "png or webp"},
		{"empty prompt", map[string]any{"prompt": "   "}, "prompt cannot be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := registry.Execute(context.Background(), []agent.ToolCall{
				toolCall("img", "generate_image", imageArgs(tc.args)),
			}, nil)[0]
			if !result.IsError || !strings.Contains(result.Output, tc.want) {
				t.Fatalf("result = %+v, want %q", result, tc.want)
			}
		})
	}
}

func TestGenerateImageHiddenInPlanMode(t *testing.T) {
	approver := &staticApprover{approved: true}
	called := false
	registry := newTestRegistry(t, t.TempDir(), approver, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			called = true
			return tools.ImageResult{Data: []byte("nope")}, nil
		},
	})
	registry.SetPlan(true)
	for _, def := range registry.Definitions() {
		if def.Name == "generate_image" {
			t.Fatal("plan mode exposed generate_image")
		}
	}
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(nil)),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "plan mode") {
		t.Fatalf("result = %+v, want plan mode", result)
	}
	if called {
		t.Fatal("plan mode called the image API")
	}
	if len(approver.actions) != 0 {
		t.Fatalf("plan mode asked for approval: %#v", approver.actions)
	}
}

func TestGenerateImageOverwritesInPlace(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "icon.png", "old")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			return tools.ImageResult{Data: []byte("new")}, nil
		},
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(map[string]any{"path": "icon.png"})),
	}, nil)[0]
	if result.IsError {
		t.Fatalf("generate_image failed: %s", result.Output)
	}
	assertToolFileContents(t, filepath.Join(root, "icon.png"), "new")
}

func TestGenerateImageRejectsFileChangedAfterApproval(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "icon.png", "old")
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			if err := os.WriteFile(filepath.Join(root, "icon.png"), []byte("changed"), 0o644); err != nil {
				t.Fatal(err)
			}
			return tools.ImageResult{Data: []byte("new")}, nil
		},
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(map[string]any{"path": "icon.png"})),
	}, nil)[0]
	if !result.IsError || !strings.Contains(result.Output, "changed") {
		t.Fatalf("result = %+v, want changed-file error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "icon.png"), "changed")
}

func TestGenerateImageRejectsFileThatAppearedAfterApproval(t *testing.T) {
	root := t.TempDir()
	registry := newTestRegistry(t, root, &staticApprover{approved: true}, tools.Options{
		MaxResultBytes:  4096,
		MaxSearchResult: 10,
		ParallelReads:   1,
		CommandTimeout:  time.Second,
		GenerateImage: func(context.Context, tools.ImageRequest) (tools.ImageResult, error) {
			if err := os.WriteFile(filepath.Join(root, "out.png"), []byte("sneak"), 0o644); err != nil {
				t.Fatal(err)
			}
			return tools.ImageResult{Data: []byte("new")}, nil
		},
	})
	result := registry.Execute(context.Background(), []agent.ToolCall{
		toolCall("img", "generate_image", imageArgs(nil)),
	}, nil)[0]
	if !result.IsError {
		t.Fatalf("result = %+v, want appeared-file error", result)
	}
	assertToolFileContents(t, filepath.Join(root, "out.png"), "sneak")
}

func imageArgs(overrides map[string]any) map[string]any {
	args := map[string]any{
		"prompt":        "a red square",
		"path":          "out.png",
		"model":         nil,
		"size":          nil,
		"quality":       nil,
		"output_format": nil,
		"background":    nil,
	}
	for key, value := range overrides {
		args[key] = value
	}
	return args
}
