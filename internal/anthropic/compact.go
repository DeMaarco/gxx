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

package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"gxx/internal/agent"
	"gxx/internal/compact"
)

// Compact forces a context compaction, preferring a model-written summary.
func (p *Provider) Compact(ctx context.Context, emit agent.EmitFunc, focus string) error {
	return p.compactSession(ctx, emit, focus, true)
}

func (p *Provider) compactSession(ctx context.Context, emit agent.EmitFunc, focus string, force bool) error {
	token, err := p.resolveToken(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	generation := p.generation
	p.history = dropOldThinking(p.history)
	if !force {
		if !p.overTarget(p.history) && !(p.lastInputTokens > 0 && p.lastInputTokens > p.compactTarget()) {
			p.mu.Unlock()
			return nil
		}
	}
	cutStart, ok := findUserCutStart(p.history, p.compactTarget(), p.instructions, force)
	if !ok {
		p.mu.Unlock()
		return nil
	}
	dropped := cloneMessages(p.history[:cutStart])
	kept := cloneMessages(p.history[cutStart:])
	transcript := renderAnthropicTranscript(dropped)
	heuristic := summarizeDropped(dropped)
	model := p.model
	timeout := p.timeout
	httpClient := p.httpClient
	baseURL := p.baseURL
	p.mu.Unlock()

	summary := heuristic
	if text, usage, sumErr := summarizeWithClaude(ctx, token, model, timeout, httpClient, baseURL, transcript, focus); sumErr == nil && strings.TrimSpace(text) != "" {
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
	notice := anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(summary))
	p.history = append([]anthropicsdk.MessageParam{notice}, kept...)
	p.lastInputTokens = historyTokens(p.history, p.instructions)
	agent.Emit(emit, agent.Event{Kind: agent.EventNotice, Text: compactNotice})
	return nil
}

func findUserCutStart(messages []anthropicsdk.MessageParam, target int64, instructions string, force bool) (int, bool) {
	users := userTurnIndexes(messages)
	if len(users) <= 1 {
		return 0, false
	}
	if force {
		return users[len(users)-1], true
	}
	best := -1
	for _, start := range users[1:] {
		notice := anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(summarizeDropped(messages[:start])))
		candidate := append([]anthropicsdk.MessageParam{notice}, messages[start:]...)
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

func renderAnthropicTranscript(messages []anthropicsdk.MessageParam) string {
	var b strings.Builder
	for _, message := range messages {
		role := string(message.Role)
		if role == "" {
			role = "message"
		}
		b.WriteString(strings.ToUpper(role))
		b.WriteString(":\n")
		for _, block := range message.Content {
			switch {
			case block.OfText != nil:
				b.WriteString(block.OfText.Text)
				b.WriteByte('\n')
			case block.OfToolUse != nil:
				fmt.Fprintf(&b, "[tool_use %s %s]\n", block.OfToolUse.Name, block.OfToolUse.ID)
			case block.OfToolResult != nil:
				out := toolResultOutput(block)
				if len(out) > 800 {
					out = clipBytes(out, 800) + "\n… [clipped]"
				}
				fmt.Fprintf(&b, "[tool_result %s]\n%s\n", block.OfToolResult.ToolUseID, out)
			case block.OfThinking != nil:
				b.WriteString("[thinking omitted]\n")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func summarizeWithClaude(
	ctx context.Context,
	token, model string,
	timeout time.Duration,
	httpClient *http.Client,
	baseURL, transcript, focus string,
) (string, agent.Usage, error) {
	var usage agent.Usage
	opts := []option.RequestOption{
		option.WithAuthToken(token),
		option.WithHeaderDel("X-Api-Key"),
		option.WithHeader("anthropic-beta", oauthBetaHeader),
		option.WithHeader("anthropic-version", "2023-06-01"),
		option.WithHeader("x-app", oauthAppHeader),
		option.WithMaxRetries(0),
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := anthropicsdk.NewClient(opts...)
	thinking, _ := thinkingForEffort("none", true)
	params := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(model),
		MaxTokens: compact.MaxSummaryTokens,
		Messages: []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(compact.BuildPrompt(transcript, focus))),
		},
		System:   systemBlocks(compact.SystemPrompt()),
		Thinking: thinking,
	}

	var (
		message anthropicsdk.Message
		raw     *http.Response
		lastErr error
	)
	streamOnce := func(reqCtx context.Context) (anthropicsdk.Message, *http.Response, error) {
		var localRaw *http.Response
		stream := client.Messages.NewStreaming(reqCtx, params, option.WithResponseInto(&localRaw))
		var msg anthropicsdk.Message
		for stream.Next() {
			event := stream.Current()
			if err := msg.Accumulate(event); err != nil {
				return msg, localRaw, fmt.Errorf("Claude stream: %w", err)
			}
		}
		if err := stream.Err(); err != nil {
			return msg, localRaw, err
		}
		return msg, localRaw, nil
	}
	for attempt := 0; attempt < maxAPIAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, retryDelay(attempt, raw)); err != nil {
				return "", usage, err
			}
		}
		requestContext, cancel := context.WithTimeout(ctx, timeout)
		message, raw, lastErr = streamOnce(requestContext)
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
	inputTokens := message.Usage.InputTokens +
		message.Usage.CacheReadInputTokens +
		message.Usage.CacheCreationInputTokens
	usage = agent.Usage{
		InputTokens:      inputTokens,
		OutputTokens:     message.Usage.OutputTokens,
		CachedTokens:     message.Usage.CacheReadInputTokens,
		CacheWriteTokens: message.Usage.CacheCreationInputTokens,
		TotalTokens:      inputTokens + message.Usage.OutputTokens,
	}
	return assistantText(message), usage, nil
}
