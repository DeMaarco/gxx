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

package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

const maxAPIAttempts = 3

func retryDelay(attempt int, raw *http.Response) time.Duration {
	if raw != nil {
		if value := strings.TrimSpace(raw.Header.Get("Retry-After")); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	if attempt <= 1 {
		return time.Second
	}
	return 2 * time.Second
}

func retryable(err error, ctx context.Context, raw *http.Response) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		return false
	}
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	if raw != nil {
		if raw.StatusCode == http.StatusTooManyRequests || raw.StatusCode >= 500 {
			return true
		}
		if raw.StatusCode >= 400 && raw.StatusCode < 500 {
			return false
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset") ||
		strings.Contains(message, "eof") ||
		strings.Contains(message, "timeout")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func streamResponse(
	ctx context.Context,
	client openaisdk.Client,
	params responses.ResponseNewParams,
	emit agent.EmitFunc,
) (*responses.Response, *http.Response, error) {
	var raw *http.Response
	stream := client.Responses.NewStreaming(ctx, params, option.WithResponseInto(&raw))
	defer stream.Close()

	var completed *responses.Response
	var streamError error
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: event.Delta})
		case "response.refusal.delta":
			agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: event.Delta})
		case "response.completed", "response.failed", "response.incomplete":
			response := event.Response
			completed = &response
		case "error":
			streamError = fmt.Errorf("OpenAI stream error: %s", event.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return completed, raw, err
	}
	if streamError != nil {
		return completed, raw, streamError
	}
	if completed == nil {
		return nil, raw, errors.New("OpenAI stream ended without a completed response")
	}
	if completed.Status != responses.ResponseStatusCompleted {
		return completed, raw, responseStatusError(*completed)
	}
	return completed, raw, nil
}
