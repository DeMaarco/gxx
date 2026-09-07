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

package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"gxx/internal/agent"
	"gxx/internal/eval"
)

func TestRequestBudgetStopsRetriesAndIncludesSummary(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "busy", http.StatusTooManyRequests)
			return
		}
		writeClaudeTextStreamWithUsage(t, w, "done", 40, 0, 0)
	}))
	defer server.Close()
	provider := testClaudeProvider(t, server, "instructions")
	meter := eval.NewMeter(10000, 2)
	ctx := agent.WithRequestObserver(context.Background(), meter)
	if _, err := provider.Respond(ctx, agent.ModelInput{UserText: "hello"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || meter.Snapshot().Requests != 2 || meter.Snapshot().Usage.TotalTokens == 0 {
		t.Fatalf("retry usage missing: %+v", meter.Snapshot())
	}
	oldText := "Goal: keep constraints. Tests failed."
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(oldText)),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent")),
	})
	var partial bool
	if err := provider.Compact(ctx, func(e agent.Event) { partial = partial || strings.Contains(e.Text, "continuity is partial") }, ""); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !partial {
		t.Fatal("summary bypassed exhausted request budget or fallback was not disclosed")
	}
	if _, err := provider.Respond(ctx, agent.ModelInput{UserText: "again"}, nil, nil); err == nil {
		t.Fatal("response bypassed exhausted budget")
	}
}

func TestSummaryRequestIsObserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeClaudeTextStreamWithUsage(t, w, "done", 40, 0, 0) }))
	defer server.Close()
	provider := testClaudeProvider(t, server, "instructions")
	oldText := "Goal: save; User constraints: no deployment."
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(oldText)),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent")),
	})
	observer := &requestRecorder{}
	if err := provider.Compact(agent.WithRequestObserver(context.Background(), observer), nil, ""); err != nil {
		t.Fatal(err)
	}
	if len(observer.requests) != 1 || observer.requests[0].Kind != "summary" || observer.usage.TotalTokens == 0 {
		t.Fatalf("summary observation missing: %+v", observer)
	}
}

type requestRecorder struct {
	requests []agent.Request
	usage    agent.Usage
}

func (r *requestRecorder) Before(_ context.Context, request agent.Request) error {
	r.requests = append(r.requests, request)
	return nil
}
func (r *requestRecorder) After(_ agent.Request, usage agent.Usage, _ error) { r.usage.Add(usage) }

func TestEcoPreservesHistoricalInstructionsAndToolContracts(t *testing.T) {
	for level := 0; level <= 3; level++ {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				writeClaudeTextStreamWithUsage(t, w, "done", 40, 0, 0)
			}))
			defer server.Close()
			provider := testClaudeProvider(t, server, "instructions")
			oldText := "Please never change Exact Path/id_7.txt. " + strings.Repeat("Preserve all these original requirements. ", 100)
			provider.SetHistory([]anthropicsdk.MessageParam{
				anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(oldText)),
				anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
				anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent")),
			})
			provider.SetTokenBudget(level, 2, 3, 1, 256, true)
			description := "Please never omit requirements or retry failed writes blindly."
			defs := []agent.ToolDefinition{{Name: "read_file", Description: description, Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": description}}}}}
			if _, err := provider.Respond(context.Background(), agent.ModelInput{UserText: "Please report the preserved constraint exactly."}, defs, nil); err != nil {
				t.Fatal(err)
			}
			quoted, _ := json.Marshal(oldText)
			if !strings.Contains(string(body), string(quoted[1:len(quoted)-1])) || !strings.Contains(string(body), description) {
				t.Fatal("Eco altered user instructions or tool descriptions")
			}
		})
	}
}
