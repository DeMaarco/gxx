package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

func TestToolParamsEnableStrictSchemas(t *testing.T) {
	params := toolParams([]agent.ToolDefinition{{
		Name:        "read_file",
		Description: "Read a file",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"path": map[string]any{"type": "string"}},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
	}})
	if len(params) != 1 {
		t.Fatalf("len(params) = %d, want 1", len(params))
	}
	data, err := json.Marshal(params[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"strict":true`) || !strings.Contains(text, `"name":"read_file"`) {
		t.Fatalf("tool JSON = %s", text)
	}
}

func TestProviderCanBeConfiguredAfterStartup(t *testing.T) {
	provider := New("", "gpt-5.6", "instructions", time.Second)
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err == nil || !strings.Contains(err.Error(), "/config") {
		t.Fatalf("Respond() error = %v, want configuration guidance", err)
	}
	provider.history = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("old", responses.EasyInputMessageRoleUser),
	}
	if err := provider.SetAPIKey("new-key"); err != nil {
		t.Fatalf("SetAPIKey() error = %v", err)
	}
	if provider.apiKey != "new-key" || len(provider.history) != 0 {
		t.Fatalf("provider was not reconfigured: key=%q history=%d", provider.apiKey, len(provider.history))
	}
}

func TestReasoningOutputPreservesEncryptedContent(t *testing.T) {
	var item responses.ResponseOutputItemUnion
	err := json.Unmarshal([]byte(`{
		"id":"rs_1",
		"type":"reasoning",
		"summary":[],
		"encrypted_content":"encrypted-reasoning",
		"status":"completed"
	}`), &item)
	if err != nil {
		t.Fatal(err)
	}

	param, ok := outputItemParam(item)
	if !ok {
		t.Fatal("outputItemParam() rejected reasoning item")
	}
	data, err := json.Marshal(param)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"encrypted_content":"encrypted-reasoning"`) {
		t.Fatalf("reasoning JSON = %s", data)
	}
}

func TestFunctionCallOutputCanBeReplayed(t *testing.T) {
	var item responses.ResponseOutputItemUnion
	err := json.Unmarshal([]byte(`{
		"id":"fc_1",
		"type":"function_call",
		"call_id":"call_1",
		"name":"read_file",
		"arguments":"{\"path\":\"README.md\"}",
		"status":"completed"
	}`), &item)
	if err != nil {
		t.Fatal(err)
	}

	param, ok := outputItemParam(item)
	if !ok {
		t.Fatal("outputItemParam() rejected function call")
	}
	data, err := json.Marshal(param)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"call_id":"call_1"`, `"name":"read_file"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("function call JSON = %s, want %s", data, expected)
		}
	}
}

func TestProviderStreamsAndResendsStatelessHistory(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(writer, request)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests = append(requests, decoded)

		writer.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			writeSSE(t, writer, map[string]any{
				"type": "response.completed",
				"response": responseFixture([]any{
					map[string]any{
						"id":                "rs_1",
						"type":              "reasoning",
						"summary":           []any{},
						"encrypted_content": "ciphertext",
						"status":            "completed",
					},
					map[string]any{
						"id":        "fc_1",
						"type":      "function_call",
						"call_id":   "call_1",
						"name":      "read_file",
						"arguments": `{"path":"README.md","offset_line":null,"limit_lines":null}`,
						"status":    "completed",
					},
				}),
			})
		} else {
			writeSSE(t, writer, map[string]any{
				"type":  "response.output_text.delta",
				"delta": "Done.",
			})
			writeSSE(t, writer, map[string]any{
				"type": "response.completed",
				"response": responseFixture([]any{
					map[string]any{
						"id":     "msg_1",
						"type":   "message",
						"role":   "assistant",
						"status": "completed",
						"content": []any{map[string]any{
							"type":        "output_text",
							"text":        "Done.",
							"annotations": []any{},
							"logprobs":    []any{},
						}},
					},
				}),
			})
		}
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.client = openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	)
	definitions := []agent.ToolDefinition{{
		Name:        "read_file",
		Description: "Read a file",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		},
		ReadOnly: true,
	}}

	first, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "Read the README"},
		definitions,
		nil,
	)
	if err != nil {
		t.Fatalf("first Respond() error = %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "call_1" {
		t.Fatalf("first response = %+v", first)
	}

	var streamed strings.Builder
	second, err := provider.Respond(
		context.Background(),
		agent.ModelInput{ToolResults: []agent.ToolResult{{
			CallID: "call_1",
			Name:   "read_file",
			Output: "README contents",
		}}},
		definitions,
		func(event agent.Event) {
			if event.Kind == agent.EventTextDelta {
				streamed.WriteString(event.Text)
			}
		},
	)
	if err != nil {
		t.Fatalf("second Respond() error = %v", err)
	}
	if second.Text != "Done." || streamed.String() != "Done." {
		t.Fatalf("second response = %+v, streamed = %q", second, streamed.String())
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	if stored, ok := requests[0]["store"].(bool); !ok || stored {
		t.Fatalf("first request store = %#v, want false", requests[0]["store"])
	}
	firstJSON, _ := json.Marshal(requests[0])
	if !strings.Contains(string(firstJSON), "reasoning.encrypted_content") {
		t.Fatalf("first request = %s, want encrypted reasoning include", firstJSON)
	}
	if !strings.Contains(string(firstJSON), `"effort":"medium"`) {
		t.Fatalf("first request = %s, want default reasoning effort", firstJSON)
	}
	if !strings.Contains(string(firstJSON), `"prompt_cache_key":"gxx"`) {
		t.Fatalf("first request = %s, want prompt cache key", firstJSON)
	}
	if !strings.Contains(string(firstJSON), `"ttl":"30m"`) {
		t.Fatalf("first request = %s, want prompt cache ttl", firstJSON)
	}
	if strings.Contains(string(firstJSON), `"context":`) {
		t.Fatalf("first request = %s, did not want reasoning context", firstJSON)
	}
	if strings.Contains(string(firstJSON), `"service_tier"`) {
		t.Fatalf("first request = %s, did not want service_tier", firstJSON)
	}
	secondJSON, _ := json.Marshal(requests[1]["input"])
	for _, expected := range []string{"ciphertext", "function_call", "function_call_output", "README contents"} {
		if !strings.Contains(string(secondJSON), expected) {
			t.Fatalf("second input = %s, want %q", secondJSON, expected)
		}
	}
}

func TestProviderRetainsToolOutputsAfterFailedFollowUp(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		requests = append(requests, decoded)

		if len(requests) == 2 {
			http.Error(writer, "temporary failure", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		var output []any
		if len(requests) == 1 {
			output = []any{map[string]any{
				"id":        "fc_1",
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "read_file",
				"arguments": `{}`,
				"status":    "completed",
			}}
		} else {
			output = []any{map[string]any{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{map[string]any{
					"type": "output_text", "text": "Recovered.", "annotations": []any{},
				}},
			}}
		}
		writeSSE(t, writer, map[string]any{
			"type":     "response.completed",
			"response": responseFixture(output),
		})
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.client = openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
		option.WithMaxRetries(0),
	)
	first, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "read"},
		nil,
		nil,
	)
	if err != nil || len(first.ToolCalls) != 1 {
		t.Fatalf("first Respond() = %+v, %v", first, err)
	}
	_, err = provider.Respond(
		context.Background(),
		agent.ModelInput{ToolResults: []agent.ToolResult{{
			CallID: "call_1", Name: "read_file", Output: "contents",
		}}},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("second Respond() succeeded, want server error")
	}
	third, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "continue"},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("third Respond() error = %v", err)
	}
	if third.Text != "Recovered." {
		t.Fatalf("third response = %+v", third)
	}
	thirdInput, _ := json.Marshal(requests[2]["input"])
	for _, expected := range []string{"function_call_output", "contents", "continue"} {
		if !strings.Contains(string(thirdInput), expected) {
			t.Fatalf("third input = %s, want %q", thirdInput, expected)
		}
	}
}

func TestProviderRetainsUserMessageAfterTransportFailure(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		requests = append(requests, decoded)
		if len(requests) == 1 {
			http.Error(writer, "temporary failure", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, map[string]any{
			"type": "response.completed",
			"response": responseFixture([]any{map[string]any{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{map[string]any{
					"type": "output_text", "text": "Recovered.", "annotations": []any{},
				}},
			}}),
		})
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.client = openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
		option.WithMaxRetries(0),
	)
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "original request"},
		nil,
		nil,
	); err == nil {
		t.Fatal("first Respond() succeeded, want server error")
	}
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "continue"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("second Respond() error = %v", err)
	}
	secondInput, _ := json.Marshal(requests[1]["input"])
	for _, expected := range []string{"original request", "continue"} {
		if !strings.Contains(string(secondInput), expected) {
			t.Fatalf("second input = %s, want %q", secondInput, expected)
		}
	}
}

func TestResponseTextIncludesRefusal(t *testing.T) {
	data, err := json.Marshal(responseFixture([]any{map[string]any{
		"id":     "msg_1",
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []any{map[string]any{
			"type": "refusal", "refusal": "I cannot do that.",
		}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	var response responses.Response
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if got := responseText(response); got != "I cannot do that." {
		t.Fatalf("responseText() = %q", got)
	}
}

func TestProviderSendsContextAndFastServiceTier(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, err := io.ReadAll(httpRequest.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, map[string]any{
			"type": "response.completed",
			"response": responseFixture([]any{map[string]any{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{map[string]any{
					"type": "output_text", "text": "ok", "annotations": []any{},
				}},
			}}),
		})
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New("test-key", "gpt-5.6-sol", "instructions", time.Second)
	provider.SetContext("1m")
	provider.SetFast(true)
	provider.client = openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	)
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	data, _ := json.Marshal(request)
	if strings.Contains(string(data), `"context":`) {
		t.Fatalf("request = %s, did not want reasoning context", data)
	}
	for _, expected := range []string{`"service_tier":"fast"`, `"model":"gpt-5.6-sol"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("request = %s, want %s", data, expected)
		}
	}
}

func TestProviderContextBudgetUsesWindowSize(t *testing.T) {
	provider := New("test-key", "gpt-5.6-sol", strings.Repeat("x", 64), time.Second)
	provider.SetContext("32k")
	if provider.contextTokens != 32_000 {
		t.Fatalf("contextTokens = %d, want 32000", provider.contextTokens)
	}
	if provider.overBudget(nil) {
		t.Fatal("default 32k budget should accept empty history")
	}
	provider.contextTokens = 4
	if !provider.overBudget(nil) {
		t.Fatal("tiny budget should reject instructions-sized input")
	}
}

func TestProviderCapturesUsageAndRateLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("x-ratelimit-limit-requests", "5000")
		writer.Header().Set("x-ratelimit-remaining-requests", "4999")
		writer.Header().Set("x-ratelimit-reset-requests", "6s")
		writer.Header().Set("x-ratelimit-limit-tokens", "200000")
		writer.Header().Set("x-ratelimit-remaining-tokens", "199000")
		writer.Header().Set("x-ratelimit-reset-tokens", "6s")
		writeSSE(t, writer, map[string]any{
			"type": "response.completed",
			"response": responseFixture([]any{map[string]any{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{map[string]any{
					"type": "output_text", "text": "ok", "annotations": []any{},
				}},
			}}),
		})
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.client = openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	)
	provider.httpClient = server.Client()
	provider.baseURL = server.URL
	result, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Usage.TotalTokens != 5 || result.Usage.ReasoningTokens != 1 ||
		result.Usage.CachedTokens != 2 || result.Usage.CacheWriteTokens != 1 {
		t.Fatalf("response usage = %+v", result.Usage)
	}

	report := provider.Report(context.Background())
	if report.SessionRequests != 1 ||
		report.Session.TotalTokens != 5 ||
		report.Session.InputTokens != 3 ||
		report.Session.OutputTokens != 2 {
		t.Fatalf("session = %+v", report)
	}
	if !report.RateLimit.Known ||
		report.RateLimit.RequestsRemaining != 4999 ||
		report.RateLimit.TokensRemaining != 199000 {
		t.Fatalf("rate limit = %+v", report.RateLimit)
	}

	if err := provider.SetAPIKey("other-key"); err != nil {
		t.Fatalf("SetAPIKey() error = %v", err)
	}
	if provider.sessionRequests != 0 || provider.session.TotalTokens != 0 || provider.rateLimit.Known {
		t.Fatalf("usage survived key change: session=%+v requests=%d limit=%+v", provider.session, provider.sessionRequests, provider.rateLimit)
	}
}

func TestProviderClosesOpenToolCallsBeforeNewUserMessage(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
	provider.history = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("earlier", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall(`{}`, "call_orphan", "read_file"),
	}

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
	input, _ := json.Marshal(request["input"])
	if !strings.Contains(string(input), "function_call_output") ||
		!strings.Contains(string(input), "call_orphan") ||
		!strings.Contains(string(input), "not executed") {
		t.Fatalf("input = %s, want closed orphan tool call", input)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "unanswered") {
		t.Fatalf("notices = %#v", notices)
	}
}

func TestProviderCompactsHistoryInsteadOfWiping(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "inst")
	provider.contextTokens = 80
	oldUser := strings.Repeat("old-turn-", 40)
	provider.history = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage(oldUser, responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("old answer", responses.EasyInputMessageRoleAssistant),
		responses.ResponseInputItemParamOfMessage("recent question", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("recent answer", responses.EasyInputMessageRoleAssistant),
	}

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
	input, _ := json.Marshal(request["input"])
	if !strings.Contains(string(input), "latest question") {
		t.Fatalf("input = %s, want current user message", input)
	}
	if !strings.Contains(string(input), "compacted") {
		t.Fatalf("input = %s, want compact notice rather than a wipe", input)
	}
	if strings.Contains(string(input), oldUser) {
		t.Fatalf("input = %s, old turn should have been compacted away", input)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Fatalf("notices = %#v", notices)
	}
}

func TestProviderRespondDoesNotHoldLockDuringStream(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
	done := make(chan error, 1)
	go func() {
		_, err := provider.Respond(
			context.Background(),
			agent.ModelInput{UserText: "hello"},
			nil,
			nil,
		)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}

	snapshotDone := make(chan struct{})
	go func() {
		_ = provider.ContextSnapshot()
		close(snapshotDone)
	}()
	select {
	case <-snapshotDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ContextSnapshot blocked while Respond held the provider lock")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
}

func testStreamingProvider(t *testing.T, server *httptest.Server, instructions string) *Provider {
	t.Helper()
	provider := New("test-key", "gpt-5.6", instructions, time.Second)
	provider.client = openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
		option.WithMaxRetries(0),
	)
	return provider
}

func writeCompletedText(t *testing.T, writer http.ResponseWriter, text string) {
	t.Helper()
	writer.Header().Set("Content-Type", "text/event-stream")
	writeSSE(t, writer, map[string]any{
		"type": "response.completed",
		"response": responseFixture([]any{map[string]any{
			"id":     "msg_1",
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []any{map[string]any{
				"type": "output_text", "text": text, "annotations": []any{},
			}},
		}}),
	})
	_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
}

func writeSSE(t *testing.T, writer io.Writer, event any) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(writer, "data: %s\n\n", data)
}

func responseFixture(output []any) map[string]any {
	return map[string]any{
		"id":                  "resp_1",
		"object":              "response",
		"created_at":          1,
		"status":              "completed",
		"model":               "gpt-5.6",
		"output":              output,
		"parallel_tool_calls": true,
		"store":               false,
		"usage": map[string]any{
			"input_tokens":  3,
			"output_tokens": 2,
			"total_tokens":  5,
			"input_tokens_details": map[string]any{
				"cached_tokens":      2,
				"cache_write_tokens": 1,
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": 1,
			},
		},
	}
}
