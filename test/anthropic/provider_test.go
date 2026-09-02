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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/agent"
	"gxx/internal/anthropic"
	"gxx/internal/config"
)

func TestSystemBlocksStartWithOAuthIdentity(t *testing.T) {
	blocks := anthropic.SystemBlocks("You are gxx.")
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	if blocks[0].Text != anthropic.OAuthIdentity {
		t.Fatalf("first block = %q", blocks[0].Text)
	}
	if blocks[1].Text != "You are gxx." {
		t.Fatalf("second block = %q", blocks[1].Text)
	}
	if blocks[0].CacheControl.Type != "" {
		t.Fatalf("identity block should not carry cache_control when instructions follow: %#v", blocks[0].CacheControl)
	}
	if string(blocks[1].CacheControl.Type) != "ephemeral" {
		t.Fatalf("instructions cache_control = %#v, want ephemeral", blocks[1].CacheControl)
	}
}

func TestSystemBlocksCachesIdentityWhenNoInstructions(t *testing.T) {
	blocks := anthropic.SystemBlocks("")
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	if string(blocks[0].CacheControl.Type) != "ephemeral" {
		t.Fatalf("identity cache_control = %#v, want ephemeral", blocks[0].CacheControl)
	}
}

func TestToolParamsMapsDefinitions(t *testing.T) {
	params := anthropic.ToolParams([]agent.ToolDefinition{{
		Name:        "list_files",
		Description: "List workspace files",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}}, 0)
	if len(params) != 1 || params[0].OfTool == nil {
		t.Fatalf("params = %#v", params)
	}
	if params[0].OfTool.Name != "list_files" {
		t.Fatalf("name = %q", params[0].OfTool.Name)
	}
	if params[0].OfTool.Description.Value != "List workspace files" {
		t.Fatalf("description = %#v", params[0].OfTool.Description)
	}
}

func TestThinkingForEffort(t *testing.T) {
	thinking, maxTokens := anthropic.ThinkingFor("none", false)
	if thinking.OfDisabled == nil || maxTokens <= 0 {
		t.Fatalf("none = %#v %d", thinking, maxTokens)
	}
	thinking, _ = anthropic.ThinkingFor("high", false)
	if thinking.OfEnabled == nil || thinking.OfEnabled.BudgetTokens != 8192 {
		t.Fatalf("high = %#v", thinking)
	}
	thinking, _ = anthropic.ThinkingFor("medium", false)
	if thinking.OfAdaptive == nil {
		t.Fatalf("medium = %#v", thinking)
	}
	thinking, _ = anthropic.ThinkingFor("medium", true)
	if thinking.OfDisabled == nil {
		t.Fatalf("omit reasoning = %#v", thinking)
	}
}

func TestUnmatchedToolIDsAndAbsorb(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", "hi", time.Second)
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call-1", map[string]any{"path": "."}, "list_files")),
	})
	open := anthropic.UnmatchedIDs(provider.History())
	if !open["call-1"] {
		t.Fatalf("open = %#v", open)
	}
	provider.AbsorbToolResults([]agent.ToolResult{{
		CallID: "call-1",
		Name:   "list_files",
		Output: "ok",
	}})
	if leftover := anthropic.UnmatchedIDs(provider.History()); len(leftover) != 0 {
		t.Fatalf("leftover = %#v", leftover)
	}
}

func TestContextSnapshotCountsInstructions(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", strings.Repeat("x", 64), time.Second)
	empty := provider.ContextSnapshot()
	if empty.InstructionsTokens <= 0 || empty.WindowTokens != int64(config.ContextTokensFor(config.ProviderAnthropic, config.DefaultContext)) {
		t.Fatalf("empty snapshot = %+v", empty)
	}
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hello there")),
	})
	provider.RefreshContext()
	used := provider.ContextSnapshot()
	if used.UserTokens <= 0 || used.UsedTokens <= empty.UsedTokens {
		t.Fatalf("used snapshot = %+v empty = %+v", used, empty)
	}
}

func TestRespondRequiresToken(t *testing.T) {
	provider := anthropic.New(nil, "claude-sonnet-4-6", "hi", time.Second)
	_, err := provider.Respond(context.Background(), agent.ModelInput{UserText: "hi"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("error = %v", err)
	}
}

func TestExportImportHistoryRoundTrip(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", "hi", time.Second)
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hello history")),
	})
	gotProvider, data, err := provider.ExportHistory()
	if err != nil {
		t.Fatalf("ExportHistory() error = %v", err)
	}
	if gotProvider != config.ProviderAnthropic || len(data) == 0 {
		t.Fatalf("ExportHistory() = provider %q len=%d", gotProvider, len(data))
	}
	provider.Reset()
	if err := provider.ImportHistory(config.ProviderOpenAI, data); err == nil {
		t.Fatal("ImportHistory() with wrong provider should fail")
	}
	if err := provider.ImportHistory(gotProvider, data); err != nil {
		t.Fatalf("ImportHistory() error = %v", err)
	}
	_, data2, err := provider.ExportHistory()
	if err != nil {
		t.Fatalf("ExportHistory() after import error = %v", err)
	}
	if string(data) != string(data2) {
		t.Fatalf("round trip = %s, want %s", data2, data)
	}
}

func TestProviderRollsBackUserTextOnAPIFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.Path, "messages") {
			http.NotFound(writer, request)
			return
		}
		n := calls.Add(1)
		if n <= 3 {
			writer.Header().Set("Retry-After", "0")
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"temporary failure"}}`))
			return
		}
		writeClaudeTextStream(t, writer, "recovered")
	}))
	defer server.Close()

	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-5", "hi", time.Second)
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL + "/")

	const userText = "same user prompt"
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: userText},
		nil,
		nil,
	); err == nil {
		t.Fatal("first Respond() succeeded, want server error")
	}
	if users := countUserMessages(provider.History()); users != 0 {
		t.Fatalf("history user messages after failure = %d, want 0", users)
	}
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: userText},
		nil,
		nil,
	); err != nil {
		t.Fatalf("second Respond() error = %v", err)
	}
	if users := countUserMessages(provider.History()); users != 1 {
		t.Fatalf("history user messages after retry = %d, want 1", users)
	}
}

func countUserMessages(history []anthropicsdk.MessageParam) int {
	n := 0
	for _, message := range history {
		if message.Role == anthropicsdk.MessageParamRoleUser {
			n++
		}
	}
	return n
}

func TestRespondFailsClosedWhenHistoryExceedsWindow(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", "inst", time.Second)
	provider.SetContextTokens(40)
	_, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: strings.Repeat("x", 4000)},
		nil,
		nil,
	)
	if !errors.Is(err, anthropic.ErrContextOverflow) {
		t.Fatalf("Respond() error = %v, want context overflow", err)
	}
	if len(provider.History()) != 0 {
		t.Fatalf("history after overflow = %d items, want user append rolled back", len(provider.History()))
	}
}

func TestProviderCompactsHistoryInsteadOfWiping(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeClaudeTextStream(t, writer, "ok")
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "inst")
	provider.SetContextTokens(200)
	oldUser := strings.Repeat("old-turn-", 40)
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(oldUser)),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent question")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("recent answer")),
	})

	var notices []string
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "latest question"},
		nil,
		func(event agent.Event) {
			if event.Kind == agent.EventNotice {
				notices = append(notices, event.Text)
			}
		},
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	messages, _ := json.Marshal(request["messages"])
	if !strings.Contains(string(messages), "latest question") {
		t.Fatalf("messages = %s, want current user message", messages)
	}
	if !strings.Contains(string(messages), "compacted") {
		t.Fatalf("messages = %s, want compact notice rather than a wipe", messages)
	}
	if strings.Contains(string(messages), oldUser) {
		t.Fatalf("messages = %s, old turn should have been compacted away", messages)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Fatalf("notices = %#v", notices)
	}
	if !anthropic.ToolPairsIntact(provider.History()) {
		t.Fatal("history tool pairs broken after compact")
	}
}

func TestProviderCompactsInsideToolLoop(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeClaudeTextStream(t, writer, "ok")
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "inst")
	provider.SetContextTokens(200)
	oldUser := strings.Repeat("old-turn-", 40)
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(oldUser)),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent question")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call-1", map[string]any{}, "run_command")),
	})

	var notices []string
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{ToolResults: []agent.ToolResult{{CallID: "call-1", Name: "run_command", Output: "done"}}},
		nil,
		func(event agent.Event) {
			if event.Kind == agent.EventNotice {
				notices = append(notices, event.Text)
			}
		},
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	messages, _ := json.Marshal(request["messages"])
	if strings.Contains(string(messages), oldUser) {
		t.Fatalf("messages = %s, want the old turn compacted away", messages)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Fatalf("notices = %#v, want one compaction notice", notices)
	}
	if strings.Contains(string(messages), "call-1") && !strings.Contains(string(messages), "tool_use") {
		t.Fatalf("messages = %s, kept a tool result without its call", messages)
	}
	if !anthropic.ToolPairsIntact(provider.History()) {
		t.Fatal("history tool pairs broken after tool-loop compact")
	}
}

func TestProviderCompactsUsingLastInputTokens(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeClaudeTextStream(t, writer, "ok")
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "inst")
	provider.SetContextTokens(220)
	oldUser := "remember this goal"
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(oldUser)),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent question")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("recent answer")),
	})
	provider.SetLastInputTokens(160)

	var notices []string
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "continue"},
		nil,
		func(event agent.Event) {
			if event.Kind == agent.EventNotice {
				notices = append(notices, event.Text)
			}
		},
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	messages, _ := json.Marshal(request["messages"])
	if strings.Contains(string(messages), oldUser) && !strings.Contains(string(messages), "Prior user requests") {
		t.Fatalf("messages = %s, want usage-driven compact of the old turn", messages)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Fatalf("notices = %#v, want compaction from last input tokens", notices)
	}
}

func TestFinalStepKeepsToolsAndForbidsCalls(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeClaudeTextStream(t, writer, "ok")
	}))
	defer server.Close()

	definitions := []agent.ToolDefinition{{
		Name:        "run_command",
		Description: "Run a shell command",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"command": map[string]any{"type": "string"}},
			"required":   []string{"command"},
		},
	}}
	provider := testClaudeProvider(t, server, "instructions")
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "review the project", FinalStep: true},
		definitions,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	data, _ := json.Marshal(request)
	if !strings.Contains(string(data), `"type":"none"`) && !strings.Contains(string(data), `"tool_choice":{"type":"none"}`) {
		// Anthropic encodes tool_choice as {"type":"none"}
		if choice, _ := json.Marshal(request["tool_choice"]); !strings.Contains(string(choice), "none") {
			t.Fatalf("request = %s, want tool_choice none", data)
		}
	}
	tools, _ := json.Marshal(request["tools"])
	if !strings.Contains(string(tools), `"name":"run_command"`) {
		t.Fatalf("tools = %s, want the definitions to survive the final step", tools)
	}
}

func TestProviderClosesOpenToolCallsBeforeNewUserMessage(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeClaudeTextStream(t, writer, "ok")
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "instructions")
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("earlier")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call_orphan", map[string]any{}, "read_file")),
	})

	var notices []string
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "continue"},
		nil,
		func(event agent.Event) {
			if event.Kind == agent.EventNotice {
				notices = append(notices, event.Text)
			}
		},
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	messages, _ := json.Marshal(request["messages"])
	if !strings.Contains(string(messages), "tool_result") ||
		!strings.Contains(string(messages), "call_orphan") ||
		!strings.Contains(string(messages), "not executed") {
		t.Fatalf("messages = %s, want closed orphan tool call", messages)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "unanswered") {
		t.Fatalf("notices = %#v", notices)
	}
}

func TestProviderSendsSystemCacheControl(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeClaudeTextStream(t, writer, "ok")
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "stable instructions")
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	system, _ := json.Marshal(request["system"])
	if !strings.Contains(string(system), `"cache_control"`) || !strings.Contains(string(system), "ephemeral") {
		t.Fatalf("system = %s, want ephemeral cache_control on instructions", system)
	}
	if strings.Contains(string(system), "AGENTS") {
		t.Fatalf("system = %s, AGENTS body must not be in system blocks", system)
	}
}

func TestProviderCancelDuringRespondRollsBackUserText(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-httpRequest.Context().Done()
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "inst")
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("prior")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("prior answer")),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.Respond(ctx, agent.ModelInput{UserText: "cancel me"}, nil, nil)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Respond() succeeded, want cancel error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Respond() did not return after cancel")
	}
	if users := countUserMessages(provider.History()); users != 1 {
		t.Fatalf("history user messages = %d, want 1 (canceled user rolled back)", users)
	}
	for _, message := range provider.History() {
		for _, block := range message.Content {
			if block.OfText != nil && strings.Contains(block.OfText.Text, "cancel me") {
				t.Fatal("canceled user text was committed to history")
			}
			if message.Role == anthropicsdk.MessageParamRoleAssistant && block.OfText != nil &&
				block.OfText.Text != "prior answer" {
				t.Fatalf("unexpected assistant text after cancel: %q", block.OfText.Text)
			}
		}
	}
}

func TestTokenFactorCalibratesContextSnapshot(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", strings.Repeat("x", 64), time.Second)
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hello from the user")),
	})
	provider.SetTokenFactor(1.0)
	provider.RefreshContext()
	base := provider.ContextSnapshot()

	provider.SetTokenFactor(2.0)
	provider.RefreshContext()
	scaled := provider.ContextSnapshot()
	if scaled.UsedTokens < base.UsedTokens*2-1 || scaled.UsedTokens > base.UsedTokens*2+1 {
		t.Fatalf("factor 2.0 used = %d, base = %d", scaled.UsedTokens, base.UsedTokens)
	}
	if scaled.UsedTokens <= base.UsedTokens {
		t.Fatalf("used should rise with factor: base=%d scaled=%d", base.UsedTokens, scaled.UsedTokens)
	}
}

func TestRespondUpdatesTokenFactorFromUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		if !strings.Contains(httpRequest.URL.Path, "messages") {
			http.NotFound(writer, httpRequest)
			return
		}
		writeClaudeTextStreamWithUsage(t, writer, "ok", 200)
	}))
	defer server.Close()

	provider := testClaudeProvider(t, server, "inst")
	if provider.TokenFactor() != 1.0 {
		t.Fatalf("default factor = %v, want 1.0", provider.TokenFactor())
	}
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if provider.LastInputTokens() != 200 {
		t.Fatalf("lastInputTokens = %d, want 200", provider.LastInputTokens())
	}
	if provider.TokenFactor() == 1.0 {
		t.Fatal("token factor should move after usage observation")
	}
	if provider.TokenFactor() < 0.5 || provider.TokenFactor() > 2.0 {
		t.Fatalf("token factor = %v, want within clamp", provider.TokenFactor())
	}
}

func testClaudeProvider(t *testing.T, server *httptest.Server, instructions string) *anthropic.Provider {
	t.Helper()
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", instructions, time.Second)
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL + "/")
	return provider
}

func writeClaudeTextStreamWithUsage(t *testing.T, writer http.ResponseWriter, text string, inputTokens int64) {
	t.Helper()
	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := writer.(http.Flusher)
	write := func(event, data string) {
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	write("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":%d,"output_tokens":1}}}`, inputTokens))
	write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	write("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text))
	write("content_block_stop", `{"type":"content_block_stop","index":0}`)
	write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`)
	write("message_stop", `{"type":"message_stop"}`)
}
