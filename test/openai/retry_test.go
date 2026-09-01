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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gxx/internal/openai"

	openaisdk "github.com/openai/openai-go/v3"

	"gxx/internal/agent"
)

func TestProviderRetries429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
			return
		}
		writeCompletedText(t, writer, "recovered")
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
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

func TestProviderDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	provider := testStreamingProvider(t, server, "instructions")
	_, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: "hello"},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("Respond() error = nil, want 400")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestRetryableClassifiesStatusCodes(t *testing.T) {
	ctx := context.Background()
	rateLimited := &openaisdk.Error{StatusCode: http.StatusTooManyRequests}
	if !openai.Retryable(rateLimited, ctx, nil) {
		t.Fatal("429 should be retryable")
	}
	server := &openaisdk.Error{StatusCode: http.StatusBadGateway}
	if !openai.Retryable(server, ctx, nil) {
		t.Fatal("502 should be retryable")
	}
	badRequest := &openaisdk.Error{StatusCode: http.StatusBadRequest}
	if openai.Retryable(badRequest, ctx, nil) {
		t.Fatal("400 should not be retryable")
	}
	if openai.Retryable(context.Canceled, ctx, nil) {
		t.Fatal("canceled context error should not be retryable without a live context")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if openai.Retryable(errors.New("timeout"), canceled, nil) {
		t.Fatal("should not retry after the parent context is canceled")
	}
	if !openai.Retryable(io.ErrUnexpectedEOF, ctx, nil) {
		t.Fatal("unexpected EOF should be retryable")
	}
}

func TestRetryDelayCapsRetryAfter(t *testing.T) {
	raw := &http.Response{Header: make(http.Header)}
	raw.Header.Set("Retry-After", "86400")
	got := openai.RetryDelay(1, raw)
	if got != openai.MaxRetryAfter {
		t.Fatalf("RetryDelay() = %s, want cap %s", got, openai.MaxRetryAfter)
	}
	raw.Header.Set("Retry-After", "0")
	if got := openai.RetryDelay(1, raw); got != 0 {
		t.Fatalf("Retry-After 0 = %s, want 0", got)
	}
	if got := openai.RetryDelay(1, nil); got != time.Second {
		t.Fatalf("default attempt 1 = %s, want 1s", got)
	}
}
