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
	"fmt"
	"net/http"
	"strings"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"gxx/internal/agent"
	"gxx/internal/compact"
)

// Compact forces a context compaction, preferring a model-written summary.
func (p *Provider) Compact(ctx context.Context, emit agent.EmitFunc, focus string) error {
	return p.compactSession(ctx, emit, focus, true)
}

func (p *Provider) compactSession(ctx context.Context, emit agent.EmitFunc, focus string, force bool) error {
	if err := p.refreshOAuthClient(ctx); err != nil {
		return err
	}

	p.mu.Lock()
	generation := p.generation
	p.history = dropOldPrograms(dropOldReasoning(p.history))
	if !force {
		if !p.overTarget(p.history) && !(p.lastInputTokens > 0 && p.lastInputTokens > p.compactTarget()) {
			p.mu.Unlock()
			return nil
		}
	}
	cutStart, ok := findOpenAICutStart(p.history, p.compactTarget(), p.instructions, force)
	if !ok {
		p.mu.Unlock()
		return nil
	}
	dropped := append([]responses.ResponseInputItemUnionParam(nil), p.history[:cutStart]...)
	kept := append([]responses.ResponseInputItemUnionParam(nil), p.history[cutStart:]...)
	transcript := renderOpenAITranscript(dropped)
	heuristic := summarizeDropped(dropped)
	model := p.model
	timeout := p.timeout
	client := p.client
	p.mu.Unlock()

	summary := heuristic
	if text, usage, sumErr := summarizeWithOpenAI(ctx, client, model, timeout, transcript, focus); sumErr == nil && strings.TrimSpace(text) != "" {
		summary = compact.NoticeWithSummary(text)
		p.mu.Lock()
		if p.generation == generation {
			p.session.Add(usage)
			p.sessionRequests++
		}
		p.mu.Unlock()
	} else if sumErr != nil && ctx.Err() != nil {
		return sumErr
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.refreshContextLocked()
	if p.generation != generation {
		return nil
	}
	notice := responses.ResponseInputItemParamOfMessage(summary, responses.EasyInputMessageRoleUser)
	p.history = append([]responses.ResponseInputItemUnionParam{notice}, kept...)
	p.lastInputTokens = historyTokens(p.history, p.instructions)
	agent.Emit(emit, agent.Event{Kind: agent.EventNotice, Text: compactNotice})
	return nil
}

func findOpenAICutStart(items []responses.ResponseInputItemUnionParam, target int64, instructions string, force bool) (int, bool) {
	users := userIndexes(items)
	if len(users) <= 1 {
		return 0, false
	}
	if force {
		return users[len(users)-1], true
	}
	best := -1
	for _, start := range users[1:] {
		notice := responses.ResponseInputItemParamOfMessage(summarizeDropped(items[:start]), responses.EasyInputMessageRoleUser)
		candidate := append([]responses.ResponseInputItemUnionParam{notice}, items[start:]...)
		best = start
		if historyTokens(candidate, instructions) <= target {
			return start, true
		}
	}
	if best >= 0 {
		return best, true
	}
	return 0, false
}

func renderOpenAITranscript(items []responses.ResponseInputItemUnionParam) string {
	var b strings.Builder
	for _, item := range items {
		switch itemKind(item) {
		case "user":
			b.WriteString("USER:\n")
			b.WriteString(inputItemText(item))
			b.WriteByte('\n')
		case "assistant":
			b.WriteString("ASSISTANT:\n")
			b.WriteString(inputItemText(item))
			b.WriteByte('\n')
		case "reasoning":
			b.WriteString("[reasoning omitted]\n")
		default:
			if name := functionCallName(item); name != "" {
				fmt.Fprintf(&b, "[tool_call %s]\n", name)
				continue
			}
			if id, _, isOut := functionCallID(item); isOut {
				out := functionCallOutput(item)
				if len(out) > 800 {
					out = clipBytes(out, 800) + "\n… [clipped]"
				}
				fmt.Fprintf(&b, "[tool_result %s]\n%s\n", id, out)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func summarizeWithOpenAI(
	ctx context.Context,
	client openaisdk.Client,
	model string,
	timeout time.Duration,
	transcript, focus string,
) (string, agent.Usage, error) {
	var usage agent.Usage
	params := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(model),
		Instructions: openaisdk.String(compact.SystemPrompt()),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(
					compact.BuildPrompt(transcript, focus),
					responses.EasyInputMessageRoleUser,
				),
			},
		},
		Store: openaisdk.Bool(false),
	}
	if max := int64(compact.MaxSummaryTokens); max > 0 {
		params.MaxOutputTokens = openaisdk.Int(max)
	}

	var (
		completed *responses.Response
		raw       *http.Response
		lastErr   error
	)
	for attempt := 0; attempt < maxAPIAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, retryDelay(attempt, raw)); err != nil {
				return "", usage, err
			}
		}
		requestContext, cancel := context.WithTimeout(ctx, timeout)
		completed, raw, lastErr = streamResponse(requestContext, client, params, nil)
		cancel()
		if lastErr == nil {
			break
		}
		if !retryable(lastErr, ctx, raw) {
			break
		}
	}
	if lastErr != nil {
		return "", usage, lastErr
	}
	usage = agent.Usage{
		InputTokens:      completed.Usage.InputTokens,
		OutputTokens:     completed.Usage.OutputTokens,
		ReasoningTokens:  completed.Usage.OutputTokensDetails.ReasoningTokens,
		CachedTokens:     completed.Usage.InputTokensDetails.CachedTokens,
		CacheWriteTokens: completed.Usage.InputTokensDetails.CacheWriteTokens,
		TotalTokens:      completed.Usage.TotalTokens,
	}
	return responseText(*completed), usage, nil
}
