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

package openai_test

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

	"gxx/internal/openai"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

func TestToolParamsEnableStrictSchemas(t *testing.T) {
	params := openai.ToolParams([]agent.ToolDefinition{{
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
	provider := openai.New("", "gpt-5.6", "instructions", time.Second)
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err == nil || !strings.Contains(err.Error(), "/config") {
		t.Fatalf("Respond() error = %v, want configuration guidance", err)
	}
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("old", responses.EasyInputMessageRoleUser),
	})
	if err := provider.SetAPIKey("new-key"); err != nil {
		t.Fatalf("SetAPIKey() error = %v", err)
	}
	if provider.APIKey() != "new-key" || len(provider.History()) != 0 {
		t.Fatalf("provider was not reconfigured: key=%q history=%d", provider.APIKey(), len(provider.History()))
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

	param, ok := openai.OutputItemParam(item)
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

	param, ok := openai.OutputItemParam(item)
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

func TestProgramOutputCanBeReplayed(t *testing.T) {
	var item responses.ResponseOutputItemUnion
	err := json.Unmarshal([]byte(`{
		"id":"prg_1",
		"type":"program",
		"call_id":"call_prog",
		"code":"await tools.list_files({});",
		"fingerprint":"fp_1"
	}`), &item)
	if err != nil {
		t.Fatal(err)
	}
	param, ok := openai.OutputItemParam(item)
	if !ok {
		t.Fatal("outputItemParam() rejected program item")
	}
	data, err := json.Marshal(param)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"type":"program"`, `"call_id":"call_prog"`, `"fingerprint":"fp_1"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("program JSON = %s, want %s", data, expected)
		}
	}
}

func TestShellCallMapsToRunCommand(t *testing.T) {
	var item responses.ResponseOutputItemUnion
	err := json.Unmarshal([]byte(`{
		"id":"shell_1",
		"type":"shell_call",
		"call_id":"call_shell",
		"status":"completed",
		"action":{"commands":["rm -rf assets","rm index.html"],"timeout_ms":0,"max_output_length":0},
		"environment":{"type":"local"}
	}`), &item)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := openai.ToolCallFromOutput(item)
	if !ok {
		t.Fatal("toolCallFromOutput() rejected shell_call")
	}
	if call.Name != "run_command" || call.ID != "call_shell" {
		t.Fatalf("call = %+v", call)
	}
	if !strings.Contains(string(call.Arguments), `"rm -rf assets && rm index.html"`) &&
		!strings.Contains(string(call.Arguments), "rm -rf assets") {
		t.Fatalf("arguments = %s", call.Arguments)
	}
}

func TestApplyPatchCallDeleteMapsToApplyPatch(t *testing.T) {
	var item responses.ResponseOutputItemUnion
	err := json.Unmarshal([]byte(`{
		"id":"ap_1",
		"type":"apply_patch_call",
		"call_id":"call_patch",
		"status":"completed",
		"operation":{"type":"delete_file","path":"index.html"}
	}`), &item)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := openai.ToolCallFromOutput(item)
	if !ok {
		t.Fatal("toolCallFromOutput() rejected apply_patch_call")
	}
	if call.Name != "apply_patch" {
		t.Fatalf("name = %q", call.Name)
	}
	if !strings.Contains(string(call.Arguments), `"action":"delete"`) ||
		!strings.Contains(string(call.Arguments), `"path":"index.html"`) {
		t.Fatalf("arguments = %s", call.Arguments)
	}
}

func TestProviderRecoversFunctionCallFromOutputItemDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{
				"id":        "fc_1",
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "list_files",
				"arguments": `{"path":null,"max_depth":null}`,
				"status":    "completed",
			},
		})
		writeSSE(t, writer, map[string]any{
			"type":     "response.completed",
			"response": responseFixture(nil),
		})
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
	result, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "list files"},
		[]agent.ToolDefinition{{
			Name:        "list_files",
			Description: "List files",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []string{},
				"additionalProperties": false,
			},
			ReadOnly: true,
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "list_files" {
		t.Fatalf("tool calls = %+v", result.ToolCalls)
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

	provider := openai.New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	))
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
	if !strings.Contains(string(firstJSON), `"prompt_cache_key":"gxx:gpt-5.6:`) {
		t.Fatalf("first request = %s, want namespaced prompt cache key", firstJSON)
	}
	if !strings.Contains(string(firstJSON), `"ttl":"30m"`) {
		t.Fatalf("first request = %s, want prompt cache ttl", firstJSON)
	}
	if !strings.Contains(string(firstJSON), `"include_obfuscation":false`) {
		t.Fatalf("first request = %s, want stream obfuscation disabled", firstJSON)
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

		if len(requests) >= 2 && len(requests) <= 4 {
			writer.Header().Set("Retry-After", "0")
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

	provider := openai.New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
		option.WithMaxRetries(0),
	))
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
	thirdInput, _ := json.Marshal(requests[len(requests)-1]["input"])
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
		if len(requests) <= 3 {
			writer.Header().Set("Retry-After", "0")
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

	provider := openai.New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
		option.WithMaxRetries(0),
	))
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
	secondInput, _ := json.Marshal(requests[len(requests)-1]["input"])
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
	if got := openai.ResponseText(response); got != "I cannot do that." {
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

	provider := openai.New("test-key", "gpt-5.6-sol", "instructions", time.Second)
	provider.SetContext("1m")
	provider.SetFast(true)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	))
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

func TestProviderOmitsEncryptedReasoningInEcoMax(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(writer, request)
			return
		}
		body, _ = io.ReadAll(request.Body)
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

	provider := openai.New("test-key", "gpt-5.6-sol", "instructions", time.Second)
	provider.SetTokenBudget(3, 1, 3, 1, 256, false)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	))
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if strings.Contains(string(body), "reasoning.encrypted_content") {
		t.Fatalf("request = %s, did not want encrypted reasoning include", body)
	}
}

func TestProviderContextBudgetUsesWindowSize(t *testing.T) {
	provider := openai.New("test-key", "gpt-5.6-sol", strings.Repeat("x", 64), time.Second)
	provider.SetContext("32k")
	if provider.ContextTokens() != 32_000 {
		t.Fatalf("contextTokens = %d, want 32000", provider.ContextTokens())
	}
	if provider.OverBudget(nil) {
		t.Fatal("default 32k budget should accept empty history")
	}
	provider.SetContextTokens(4)
	if !provider.OverBudget(nil) {
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

	provider := openai.New("test-key", "gpt-5.6", "instructions", time.Second)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
	))
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL)
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
	if provider.SessionRequests() != 0 || provider.SessionUsage().TotalTokens != 0 || provider.RateLimitState().Known {
		t.Fatalf("usage survived key change: session=%+v requests=%d limit=%+v", provider.SessionUsage(), provider.SessionRequests(), provider.RateLimitState())
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
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("earlier", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall(`{}`, "call_orphan", "read_file"),
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

func TestFinalStepKeepsToolsAndForbidsCalls(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	definitions := []agent.ToolDefinition{{
		Name:        "run_command",
		Description: "Run a shell command",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"command": map[string]any{"type": "string"}},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
	}}
	provider := testStreamingProvider(t, server, "instructions")
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "review the project", FinalStep: true},
		definitions,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	data, _ := json.Marshal(request)
	if !strings.Contains(string(data), `"tool_choice":"none"`) {
		t.Fatalf("request = %s, want tool_choice none", data)
	}
	// The tool namespace has to stay declared. A model that has been calling
	// tools and suddenly finds none writes "to=functions.run_command" into
	// the answer instead.
	tools, _ := json.Marshal(request["tools"])
	if !strings.Contains(string(tools), `"name":"run_command"`) {
		t.Fatalf("tools = %s, want the definitions to survive the final step", tools)
	}
}

func TestToolParamsOmitsEmptyToolList(t *testing.T) {
	if params := openai.ToolParams(nil); params != nil {
		t.Fatalf("ToolParams(nil) = %#v, want nil so the field is omitted", params)
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
	provider.SetContextTokens(80)
	oldUser := strings.Repeat("old-turn-", 40)
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage(oldUser, responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("old answer", responses.EasyInputMessageRoleAssistant),
		responses.ResponseInputItemParamOfMessage("recent question", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("recent answer", responses.EasyInputMessageRoleAssistant),
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

func TestProviderCompactsInsideToolLoop(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "inst")
	provider.SetContextTokens(80)
	oldUser := strings.Repeat("old-turn-", 40)
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage(oldUser, responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("old answer", responses.EasyInputMessageRoleAssistant),
		responses.ResponseInputItemParamOfMessage("recent question", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall("{}", "call-1", "run_command"),
	})

	var notices []string
	// No user text: this is the step that follows a tool call, which is where
	// a long loop used to grow the history past the window unchecked.
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

	input, _ := json.Marshal(request["input"])
	if strings.Contains(string(input), oldUser) {
		t.Fatalf("input = %s, want the old turn compacted away", input)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Fatalf("notices = %#v, want one compaction notice", notices)
	}
	// Compaction must not orphan the output of a call it dropped.
	if strings.Contains(string(input), "call-1") && !strings.Contains(string(input), "function_call\"") {
		t.Fatalf("input = %s, kept a tool output without its call", input)
	}
}

func TestProviderDisablesAPITruncation(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
	if _, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if request["truncation"] != "disabled" {
		t.Fatalf("truncation = %#v, want disabled", request["truncation"])
	}
}

func TestProviderCompactsUsingLastInputTokens(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		body, _ := io.ReadAll(httpRequest.Body)
		_ = json.Unmarshal(body, &request)
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "inst")
	provider.SetContextTokens(90)
	oldUser := "remember this goal"
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage(oldUser, responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("old answer", responses.EasyInputMessageRoleAssistant),
		responses.ResponseInputItemParamOfMessage("recent question", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("recent answer", responses.EasyInputMessageRoleAssistant),
	})
	provider.SetLastInputTokens(80)

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
	if strings.Contains(string(input), oldUser) && !strings.Contains(string(input), "Prior user requests") {
		t.Fatalf("input = %s, want usage-driven compact of the old turn", input)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "compacted") {
		t.Fatalf("notices = %#v, want compaction from last input tokens", notices)
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

func testStreamingProvider(t *testing.T, server *httptest.Server, instructions string) *openai.Provider {
	t.Helper()
	provider := openai.New("test-key", "gpt-5.6", instructions, time.Second)
	provider.SetClient(openaisdk.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL+"/"),
		option.WithMaxRetries(0),
	))
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

type staticTokens struct {
	token   string
	account string
}

func (s staticTokens) AccessToken(context.Context) (string, error) { return s.token, nil }
func (s staticTokens) AccountID(context.Context) (string, error)   { return s.account, nil }

func TestCodexClientSendsAccountHeaders(t *testing.T) {
	var got *http.Request
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ = io.ReadAll(request.Body)
		clone := request.Clone(request.Context())
		clone.Body = nil
		got = clone
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := openai.NewWithSource(staticTokens{token: "oauth-token", account: "acct-123"}, "gpt-5.6", "instructions", time.Second)
	if !provider.UsingOAuth() || provider.BaseURL() != openai.CodexAPIBaseURL() {
		t.Fatalf("oauth=%v base=%q", provider.UsingOAuth(), provider.BaseURL())
	}
	provider.SetBaseURL(server.URL)
	_, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if got == nil {
		t.Fatal("no request")
	}
	if got.URL.Path != "/responses" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer oauth-token" {
		t.Fatalf("Authorization = %q", auth)
	}
	if got.Header.Get(openai.CodexAccountHeader()) != "acct-123" {
		t.Fatalf("account header = %q", got.Header.Get(openai.CodexAccountHeader()))
	}
	if got.Header.Get("originator") != openai.CodexOriginator() || got.Header.Get("originator") == "codex_cli_rs" {
		t.Fatalf("originator = %q", got.Header.Get("originator"))
	}
	if got.Header.Get("OpenAI-Beta") != openai.CodexBetaHeader() {
		t.Fatalf("OpenAI-Beta = %q", got.Header.Get("OpenAI-Beta"))
	}
	if got.Header.Get(openai.CodexResponsesLiteHeader()) != "true" {
		t.Fatalf("lite header = %q", got.Header.Get(openai.CodexResponsesLiteHeader()))
	}
	if got.Header.Get("session-id") == "" {
		t.Fatal("missing session-id")
	}
	if strings.Contains(string(body), "prompt_cache_options") || strings.Contains(string(body), `"truncation"`) || strings.Contains(string(body), "include_obfuscation") {
		t.Fatalf("codex body included platform-only fields: %s", body)
	}
	if !strings.Contains(string(body), `"store":false`) {
		t.Fatalf("codex body = %s, want store:false", body)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body = %s", body)
	}
	for key := range payload {
		switch key {
		case "model", "instructions", "input", "tools", "tool_choice", "parallel_tool_calls",
			"reasoning", "store", "stream", "include", "service_tier", "prompt_cache_key", "text":
		default:
			t.Fatalf("unsupported Codex field %q in %s", key, body)
		}
	}
}

func TestCodexLiteRequestPutsToolsInAdditionalTools(t *testing.T) {
	var body []byte
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ = io.ReadAll(request.Body)
		got = request.Header.Clone()
		writeCompletedText(t, writer, "ok")
	}))
	defer server.Close()

	provider := openai.NewWithSource(
		staticTokens{token: "oauth-token", account: "acct-123"},
		"gpt-5.6-luna",
		"instructions",
		time.Second,
	)
	provider.SetBaseURL(server.URL)
	_, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		[]agent.ToolDefinition{{
			Name:        "list_files",
			Description: "List files",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []string{},
				"additionalProperties": false,
			},
			ReadOnly: true,
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if got.Get(openai.CodexResponsesLiteHeader()) != "true" {
		t.Fatalf("lite header = %q", got.Get(openai.CodexResponsesLiteHeader()))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body = %s", body)
	}
	if payload["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %v, want false", payload["parallel_tool_calls"])
	}
	if !strings.Contains(string(body), `"context":"all_turns"`) {
		t.Fatalf("body = %s, want reasoning.context all_turns", body)
	}
	input, _ := payload["input"].([]any)
	if len(input) < 1 {
		t.Fatalf("input = %s", body)
	}
	first, _ := input[0].(map[string]any)
	if first["type"] != "additional_tools" {
		t.Fatalf("input[0] = %s, want additional_tools", body)
	}
	tools, _ := first["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("additional_tools = %s", body)
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "list_files" {
		t.Fatalf("tool = %s", body)
	}
	if !strings.Contains(string(body), `"allowed_callers"`) {
		t.Fatalf("body = %s, want allowed_callers for programmatic tools", body)
	}
	topTools, _ := payload["tools"].([]any)
	if len(topTools) != 0 {
		t.Fatalf("top-level tools = %s, want omitted for Responses Lite", body)
	}
}

func TestUsesResponsesLite(t *testing.T) {
	for _, model := range []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6"} {
		if !openai.UsesResponsesLite(model) {
			t.Fatalf("%s should use Responses Lite", model)
		}
	}
	if openai.UsesResponsesLite("gpt-5.5") {
		t.Fatal("gpt-5.5 should not use Responses Lite")
	}
}

func TestSanitizeCodexPayloadDropsUnknownFields(t *testing.T) {
	got := openai.SanitizeCodexPayload([]byte(`{
		"model":"gpt-5.6-sol",
		"store":false,
		"stream":true,
		"metadata":{"x":"1"},
		"truncation":"disabled",
		"prompt_cache_options":{"ttl":"30m"},
		"service_tier":"fast",
		"tools":[{"type":"function","name":"read_file","strict":true}]
	}`))
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"metadata", "truncation", "prompt_cache_options", "service_tier"} {
		if _, ok := payload[extra]; ok {
			t.Fatalf("kept %s: %s", extra, got)
		}
	}
	tools := payload["tools"].([]any)
	tool := tools[0].(map[string]any)
	if _, ok := tool["strict"]; ok {
		t.Fatalf("kept tool strict: %s", got)
	}
}

func TestCodexErrorSurfacesDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"detail":"Unknown field: prompt_cache_options"}`))
	}))
	defer server.Close()

	provider := openai.NewWithSource(staticTokens{token: "oauth-token", account: "acct-123"}, "gpt-5.6", "instructions", time.Second)
	provider.SetBaseURL(server.URL)
	_, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Unknown field: prompt_cache_options") {
		t.Fatalf("error = %v", err)
	}
}

func TestOAuthUsageFetchesChatGPTSubscription(t *testing.T) {
	var path, originator, accountID, auth string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		originator = request.Header.Get("originator")
		accountID = request.Header.Get(openai.CodexAccountHeader())
		auth = request.Header.Get("Authorization")
		if strings.Contains(request.URL.Path, "/organization/") {
			t.Errorf("OAuth usage probed organization endpoint %s", request.URL.Path)
		}
		writeJSON(t, writer, map[string]any{
			"plan_type": "plus",
			"rate_limit": map[string]any{
				"primary_window": map[string]any{
					"used_percent":         55,
					"limit_window_seconds": 18000,
					"reset_after_seconds":  7920,
				},
				"secondary_window": map[string]any{
					"used_percent":         51,
					"limit_window_seconds": 604800,
					"reset_after_seconds":  489600,
				},
			},
		})
	}))
	defer server.Close()

	provider := openai.NewWithSource(staticTokens{token: "oauth-token", account: "acct"}, "gpt-5.6", "", time.Second)
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL)
	report := provider.Report(context.Background())
	if path != "/wham/usage" {
		t.Fatalf("path = %q, want /wham/usage", path)
	}
	if originator != openai.CodexOriginator() || accountID != "acct" || auth != "Bearer oauth-token" {
		t.Fatalf("headers originator=%q account=%q auth=%q", originator, accountID, auth)
	}
	if report.Source != "ChatGPT" || report.Account.Plan != "plus" || len(report.Account.Windows) != 2 {
		t.Fatalf("account = %+v", report.Account)
	}
	if report.Account.Windows[0].Name != "5h" || report.Account.Windows[0].UsedPercent != 55 {
		t.Fatalf("5h window = %+v", report.Account.Windows[0])
	}
	if report.Account.Windows[1].Name != "weekly" || report.Account.Windows[1].UsedPercent != 51 {
		t.Fatalf("weekly window = %+v", report.Account.Windows[1])
	}
}

func TestProviderStreamsOutputTextDoneWithoutDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, map[string]any{
			"type": "response.output_text.done",
			"text": "Hello from done.",
		})
		writeSSE(t, writer, map[string]any{
			"type": "response.completed",
			"response": responseFixture([]any{map[string]any{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{map[string]any{
					"type": "output_text", "text": "Hello from done.", "annotations": []any{},
				}},
			}}),
		})
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
	var streamed strings.Builder
	result, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		func(event agent.Event) {
			if event.Kind == agent.EventTextDelta {
				streamed.WriteString(event.Text)
			}
		},
	)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Text != "Hello from done." {
		t.Fatalf("text = %q", result.Text)
	}
	if streamed.String() != "Hello from done." {
		t.Fatalf("streamed = %q", streamed.String())
	}
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
