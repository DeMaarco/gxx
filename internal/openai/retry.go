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
	"net/http"
	"strings"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
	"gxx/internal/budget"
)

const maxAPIAttempts = budget.MaxAPIAttempts

var maxRetryAfter = budget.MaxRetryAfter

func retryDelay(attempt int, raw *http.Response) time.Duration {
	return budget.RetryDelay(attempt, raw)
}

func retryable(err error, ctx context.Context, raw *http.Response) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var apiErr *openaisdk.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	return budget.Retryable(err, ctx, raw)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	return budget.SleepContext(ctx, delay)
}

func formatResponsesError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openaisdk.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	detail := strings.TrimSpace(apiErr.Message)
	if detail == "" {
		detail = parseAPIError([]byte(apiErr.RawJSON()))
	}
	if detail == "" || detail == "request failed" || strings.Contains(err.Error(), detail) {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
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
	var streamedText bool
	var streamedItems []responses.ResponseOutputItemUnion
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			streamedText = true
			agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: event.Delta})
		case "response.output_text.done":
			if !streamedText && event.Text != "" {
				streamedText = true
				agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: event.Text})
			}
		case "response.output_item.done":
			if event.Item.Type != "" {
				streamedItems = append(streamedItems, event.Item)
			}
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
		return completed, raw, formatResponsesError(err)
	}
	if streamError != nil {
		return completed, raw, streamError
	}
	if completed == nil {
		return nil, raw, errors.New("OpenAI stream ended without a completed response")
	}
	if len(completed.Output) == 0 && len(streamedItems) > 0 {
		completed.Output = streamedItems
	}
	if completed.Status != responses.ResponseStatusCompleted {
		return completed, raw, responseStatusError(*completed)
	}
	return completed, raw, nil
}
