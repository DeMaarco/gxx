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
)

func TestRetryableClassifiesStatusCodes(t *testing.T) {
	ctx := context.Background()
	rateLimited := &anthropicsdk.Error{StatusCode: http.StatusTooManyRequests}
	if !anthropic.Retryable(rateLimited, ctx, nil) {
		t.Fatal("429 should be retryable")
	}
	if !anthropic.Retryable(&anthropicsdk.Error{StatusCode: http.StatusBadGateway}, ctx, nil) {
		t.Fatal("502 should be retryable")
	}
	if anthropic.Retryable(&anthropicsdk.Error{StatusCode: http.StatusBadRequest}, ctx, nil) {
		t.Fatal("400 should not be retryable")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if anthropic.Retryable(errors.New("timeout"), canceled, nil) {
		t.Fatal("should not retry after the parent context is canceled")
	}
	if !anthropic.Retryable(io.ErrUnexpectedEOF, ctx, nil) {
		t.Fatal("unexpected EOF should be retryable")
	}
}

func TestRetryDelayCapsRetryAfter(t *testing.T) {
	raw := &http.Response{Header: make(http.Header)}
	raw.Header.Set("Retry-After", "86400")
	got := anthropic.RetryDelay(1, raw)
	if got != anthropic.MaxRetryAfter {
		t.Fatalf("RetryDelay() = %s, want cap %s", got, anthropic.MaxRetryAfter)
	}
	raw.Header.Set("Retry-After", "0")
	raw.Header.Del("Retry-After-Ms")
	if got := anthropic.RetryDelay(1, raw); got != 0 {
		t.Fatalf("Retry-After 0 = %s, want 0", got)
	}
	raw.Header.Del("Retry-After")
	raw.Header.Set("Retry-After-Ms", "120000")
	if got := anthropic.RetryDelay(1, raw); got != anthropic.MaxRetryAfter {
		t.Fatalf("Retry-After-Ms cap = %s, want %s", got, anthropic.MaxRetryAfter)
	}
	if got := anthropic.RetryDelay(1, nil); got != time.Second {
		t.Fatalf("default attempt 1 = %s, want 1s", got)
	}
}

func TestProviderRetries429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.Path, "messages") {
			http.NotFound(writer, request)
			return
		}
		n := calls.Add(1)
		if n == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
			return
		}
		writeClaudeTextStream(t, writer, "recovered")
	}))
	defer server.Close()

	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-5", "hi", time.Second)
	provider.SetHTTPClient(server.Client())
	provider.SetBaseURL(server.URL + "/")
	var notices []string
	result, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		func(event agent.Event) {
			if event.Kind == agent.EventNotice {
				notices = append(notices, event.Text)
			}
		},
	)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if result.Text != "recovered" {
		t.Fatalf("text = %q, want recovered", result.Text)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "Retrying") {
		t.Fatalf("notices = %#v, want retry notice", notices)
	}
}

func writeClaudeTextStream(t *testing.T, writer http.ResponseWriter, text string) {
	t.Helper()
	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := writer.(http.Flusher)
	write := func(event, data string) {
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	write("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`)
	write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	write("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text))
	write("content_block_stop", `{"type":"content_block_stop","index":0}`)
	write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`)
	write("message_stop", `{"type":"message_stop"}`)
}
