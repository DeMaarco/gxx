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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"gxx/internal/agent"
	"gxx/internal/budget"
	"gxx/internal/config"
)

const fallbackHistoryItems = budget.FallbackHistoryItems

var ErrContextOverflow = errors.New("context window is full; run /clear or start a new conversation")

// TokenSource returns a usable Claude access token, refreshing when needed.
type TokenSource interface {
	AccessToken(context.Context) (string, error)
}

// Provider implements a Messages conversation authenticated with OAuth.
type Provider struct {
	source        TokenSource
	accessToken   string
	model         string
	effort        string
	contextTokens int
	fast          bool
	instructions  string
	timeout       time.Duration

	mu              sync.Mutex
	generation      uint64
	history         []anthropicsdk.MessageParam
	session         agent.Usage
	sessionRequests int64
	rateLimit       agent.RateLimit
	contextUsage    agent.ContextUsage
	lastInputTokens int64
	tokenFactor     float64
	ecoLevel        int
	compactNumer    int
	compactDenom    int
	omitReasoning   bool
	ecoToolKeep     int
	ecoToolClip     int
	httpClient      *http.Client
	baseURL         string
}

func New(source TokenSource, model, instructions string, timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = config.DefaultAPITimeout
	}
	provider := &Provider{
		source:        source,
		model:         strings.TrimSpace(model),
		effort:        "medium",
		contextTokens: config.ContextTokensFor(config.ProviderAnthropic, config.DefaultContext),
		instructions:  instructions,
		timeout:       timeout,
		compactNumer:  2,
		compactDenom:  3,
		tokenFactor:   1.0,
	}
	provider.refreshContextLocked()
	return provider
}

func (p *Provider) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = model
}

func (p *Provider) SetEffort(effort string) {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.effort = effort
}

func (p *Provider) SetContext(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.contextTokens = config.ContextTokensFor(config.ProviderAnthropic, value)
	p.refreshContextLocked()
}

func (p *Provider) SetFast(fast bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fast = fast
}

func (p *Provider) SetTokenBudget(ecoLevel, compactNumer, compactDenom, toolKeep, toolClip int, includeReasoning bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ecoLevel = ecoLevel
	if compactNumer <= 0 || compactDenom <= 0 {
		compactNumer, compactDenom = 2, 3
	}
	p.compactNumer = compactNumer
	p.compactDenom = compactDenom
	p.omitReasoning = !includeReasoning
	p.ecoToolKeep = toolKeep
	p.ecoToolClip = toolClip
	p.refreshContextLocked()
}

func (p *Provider) SetInstructions(instructions string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.instructions = instructions
	p.refreshContextLocked()
}

func (p *Provider) SetAccessToken(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.accessToken = strings.TrimSpace(token)
	p.generation++
	p.history = nil
	p.session = agent.Usage{}
	p.sessionRequests = 0
	p.rateLimit = agent.RateLimit{}
	p.lastInputTokens = 0
	p.tokenFactor = 1.0
	p.refreshContextLocked()
}

func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
	p.history = nil
	p.lastInputTokens = 0
	p.refreshContextLocked()
}

func (p *Provider) ExportHistory() (string, json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.history) == 0 {
		return config.ProviderAnthropic, nil, nil
	}
	data, err := json.Marshal(p.history)
	if err != nil {
		return "", nil, fmt.Errorf("encode anthropic history: %w", err)
	}
	return config.ProviderAnthropic, data, nil
}

func (p *Provider) ImportHistory(provider string, history json.RawMessage) error {
	if strings.TrimSpace(provider) != config.ProviderAnthropic {
		return fmt.Errorf("history provider %q does not match anthropic", provider)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(history) == 0 {
		p.generation++
		p.history = nil
		p.lastInputTokens = 0
		p.refreshContextLocked()
		return nil
	}
	var items []anthropicsdk.MessageParam
	if err := json.Unmarshal(history, &items); err != nil {
		return fmt.Errorf("decode anthropic history: %w", err)
	}
	p.generation++
	p.history = items
	p.lastInputTokens = 0
	p.refreshContextLocked()
	return nil
}

func (p *Provider) Respond(
	ctx context.Context,
	input agent.ModelInput,
	definitions []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	var result agent.ModelResponse

	token, err := p.resolveToken(ctx)
	if err != nil {
		return result, err
	}

	p.mu.Lock()
	hasUserText := strings.TrimSpace(input.UserText) != ""
	if len(input.ToolResults) > 0 {
		p.appendToolResultsLocked(input.ToolResults)
	}
	if hasUserText {
		if n := p.closeOpenToolCallsLocked(unansweredToolOutput); n > 0 {
			agent.Emit(emit, agent.Event{
				Kind: agent.EventNotice,
				Text: "Closed unanswered tool calls from the previous turn.",
			})
		}
	}
	// A long tool loop can outgrow the window without the user typing again,
	// so compaction cannot wait for the next prompt. Dropping whole turns only
	// ever cuts at a user-text boundary, which never separates tool_use from
	// its adjacent tool_result.
	needCompact := p.shouldCompact(input.UserText)
	p.mu.Unlock()
	if needCompact {
		if err := p.compactSession(ctx, emit, "", false); err != nil && ctx.Err() != nil {
			return result, err
		}
	}
	p.mu.Lock()
	// Snapshot before the user append so API failure / overflow / cancel can
	// roll back only that message. Tool results from this Respond stay.
	var historyBeforeUser []anthropicsdk.MessageParam
	if hasUserText {
		historyBeforeUser = append([]anthropicsdk.MessageParam(nil), p.history...)
		p.history = append(p.history, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(input.UserText)))
	}
	if !hasUserText && len(input.ToolResults) == 0 {
		p.mu.Unlock()
		return result, errors.New("model input contains neither a user message nor tool results")
	}
	p.history = dropOldThinking(p.history)

	staged := slimInput(p.history, p.ecoLevel, p.ecoToolKeep, p.ecoToolClip)
	if p.overBudget(staged) {
		staged = emergencyFit(staged, p.contextTokens, p.instructions)
	}
	if p.overBudget(staged) {
		if hasUserText {
			p.history = historyBeforeUser
		}
		p.refreshContextLocked()
		p.mu.Unlock()
		return result, ErrContextOverflow
	}
	generation := p.generation
	timeout := p.timeout
	params := p.requestParamsLocked(staged, definitions, input.FinalStep)
	p.refreshContextLocked()
	p.mu.Unlock()

	rollbackUserAppend := func() {
		if !hasUserText {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.generation == generation {
			p.history = historyBeforeUser
			p.refreshContextLocked()
		}
	}

	client := p.newClient(token)
	var (
		message anthropicsdk.Message
		raw     *http.Response
		lastErr error
	)
	for attempt := 0; attempt < maxAPIAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, raw)
			agent.Emit(emit, agent.Event{
				Kind: agent.EventNotice,
				Text: "Retrying Claude request…",
			})
			if err := sleepContext(ctx, delay); err != nil {
				rollbackUserAppend()
				return result, err
			}
		}
		requestContext, cancel := context.WithTimeout(ctx, timeout)
		message, raw, lastErr = p.stream(requestContext, client, params, emit)
		cancel()
		if lastErr == nil {
			break
		}
		if !retryable(lastErr, ctx, raw) {
			break
		}
	}
	if lastErr != nil {
		if raw != nil {
			p.mu.Lock()
			p.rateLimit = parseRateLimit(raw.Header)
			p.mu.Unlock()
		}
		rollbackUserAppend()
		return result, lastErr
	}

	result.Text = assistantText(message)
	result.ToolCalls = assistantToolCalls(message)
	// Anthropic splits input into uncached / cache_read / cache_creation.
	// Normalize to inclusive InputTokens so pricing and calibration match OpenAI.
	inputTokens := message.Usage.InputTokens +
		message.Usage.CacheReadInputTokens +
		message.Usage.CacheCreationInputTokens
	result.Usage = agent.Usage{
		InputTokens:      inputTokens,
		OutputTokens:     message.Usage.OutputTokens,
		CachedTokens:     message.Usage.CacheReadInputTokens,
		CacheWriteTokens: message.Usage.CacheCreationInputTokens,
		ReasoningTokens:  message.Usage.OutputTokensDetails.ThinkingTokens,
		TotalTokens:      inputTokens + message.Usage.OutputTokens,
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.refreshContextLocked()
	if raw != nil {
		p.rateLimit = parseRateLimit(raw.Header)
	}
	if p.generation == generation {
		// Commit the staged (possibly slimmed) request history plus the new
		// assistant turn. Slim is not durable until this success path.
		p.history = append(staged, message.ToParam())
		p.session.Add(result.Usage)
		p.sessionRequests++
		p.lastInputTokens = result.Usage.InputTokens
		p.updateTokenFactorLocked(staged, estimateJSON(toolParams(definitions, p.ecoLevel)))
	}
	return result, nil
}

func (p *Provider) resolveToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	override := p.accessToken
	source := p.source
	p.mu.Unlock()
	if override != "" {
		return override, nil
	}
	if source == nil {
		return "", errors.New("Claude is not logged in; run /login")
	}
	token, err := source.AccessToken(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("Claude is not logged in; run /login")
	}
	return token, nil
}

func (p *Provider) requestParamsLocked(
	staged []anthropicsdk.MessageParam,
	definitions []agent.ToolDefinition,
	finalStep bool,
) anthropicsdk.MessageNewParams {
	instructions := p.instructions
	thinking, maxTokens := thinkingForEffort(p.effort, p.omitReasoning)
	params := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(p.model),
		MaxTokens: maxTokens,
		Messages:  staged,
		System:    systemBlocks(instructions),
		Tools:     toolParams(definitions, p.ecoLevel),
		Thinking:  thinking,
	}
	if finalStep {
		none := anthropicsdk.NewToolChoiceNoneParam()
		params.ToolChoice = anthropicsdk.ToolChoiceUnionParam{OfNone: &none}
	}
	return params
}

func (p *Provider) stream(
	ctx context.Context,
	client anthropicsdk.Client,
	params anthropicsdk.MessageNewParams,
	emit agent.EmitFunc,
) (anthropicsdk.Message, *http.Response, error) {
	var raw *http.Response
	stream := client.Messages.NewStreaming(ctx, params, option.WithResponseInto(&raw))
	var message anthropicsdk.Message
	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			agent.Emit(emit, agent.Event{Kind: agent.EventTextDelta, Text: event.Delta.Text})
		}
		if err := message.Accumulate(event); err != nil {
			return message, raw, fmt.Errorf("Claude stream: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return message, raw, err
	}
	return message, raw, nil
}

func (p *Provider) newClient(token string) anthropicsdk.Client {
	opts := []option.RequestOption{
		option.WithAuthToken(token),
		option.WithHeaderDel("X-Api-Key"),
		option.WithHeader("anthropic-beta", oauthBetaHeader),
		option.WithHeader("anthropic-version", "2023-06-01"),
		option.WithHeader("x-app", oauthAppHeader),
		option.WithMaxRetries(0),
	}
	if p.httpClient != nil {
		opts = append(opts, option.WithHTTPClient(p.httpClient))
	}
	if strings.TrimSpace(p.baseURL) != "" {
		opts = append(opts, option.WithBaseURL(p.baseURL))
	}
	return anthropicsdk.NewClient(opts...)
}

func (p *Provider) Report(ctx context.Context) agent.UsageReport {
	p.mu.Lock()
	report := agent.UsageReport{
		Source:          "Claude",
		Session:         p.session,
		SessionRequests: p.sessionRequests,
		RateLimit:       p.rateLimit,
	}
	httpClient := p.httpClient
	baseURL := p.baseURL
	p.mu.Unlock()

	token, err := p.resolveToken(ctx)
	if err != nil {
		report.Account.Error = err.Error()
		return report
	}
	fetchContext, cancel := context.WithTimeout(ctx, usageFetchTimeout)
	defer cancel()
	report.Account = fetchOAuthUsage(fetchContext, httpClient, baseURL, token)
	return report
}

func (p *Provider) ContextSnapshot() agent.ContextUsage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.contextUsage
}

func (p *Provider) refreshContextLocked() {
	p.contextUsage = p.computeContextLocked()
}

func (p *Provider) computeContextLocked() agent.ContextUsage {
	usage := agent.ContextUsage{
		WindowTokens:       int64(p.contextTokens),
		InstructionsTokens: p.calibrate(estimateTokens(len(oauthIdentity) + len(p.instructions))),
	}
	for _, message := range p.history {
		tokens := p.calibrate(estimateJSON(message))
		switch message.Role {
		case anthropicsdk.MessageParamRoleUser:
			if messageHasToolResult(message) {
				usage.ToolTokens += tokens
			} else {
				usage.UserTokens += tokens
			}
		case anthropicsdk.MessageParamRoleAssistant:
			if messageHasToolUse(message) {
				usage.ToolTokens += tokens
			} else {
				usage.AssistantTokens += tokens
			}
		default:
			usage.ToolTokens += tokens
		}
	}
	usage.UsedTokens = usage.InstructionsTokens +
		usage.UserTokens +
		usage.AssistantTokens +
		usage.ReasoningTokens +
		usage.ToolTokens
	usage.Percent = agent.ContextPercent(usage.UsedTokens, usage.WindowTokens)
	return usage
}

func (p *Provider) overBudget(history []anthropicsdk.MessageParam) bool {
	if p.contextTokens <= 0 {
		return len(history) > fallbackHistoryItems
	}
	return p.calibrate(historyTokens(history, p.instructions)) > int64(p.contextTokens)
}

func (p *Provider) overTarget(history []anthropicsdk.MessageParam) bool {
	if p.contextTokens <= 0 {
		return len(history) > fallbackHistoryItems
	}
	return p.calibrate(historyTokens(history, p.instructions)) > p.compactTarget()
}

func (p *Provider) calibrate(tokens int64) int64 {
	return budget.Calibrate(tokens, p.tokenFactor)
}

func (p *Provider) updateTokenFactorLocked(staged []anthropicsdk.MessageParam, toolTokens int64) {
	est := historyTokens(staged, p.instructions) + toolTokens
	p.tokenFactor = budget.UpdateFactor(p.tokenFactor, est, p.lastInputTokens)
}

func messageHasToolResult(message anthropicsdk.MessageParam) bool {
	for _, block := range message.Content {
		if block.OfToolResult != nil {
			return true
		}
	}
	return false
}

func messageHasToolUse(message anthropicsdk.MessageParam) bool {
	for _, block := range message.Content {
		if block.OfToolUse != nil {
			return true
		}
	}
	return false
}

func historyTokens(history []anthropicsdk.MessageParam, instructions string) int64 {
	total := estimateTokens(len(oauthIdentity) + len(instructions))
	for _, message := range history {
		total += estimateJSON(message)
	}
	return total
}

func estimateJSON(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return budget.EstimateTokens(len(data))
}

func estimateTokens(bytes int) int64 {
	return budget.EstimateTokens(bytes)
}

func thinkingForEffort(effort string, omit bool) (anthropicsdk.ThinkingConfigParamUnion, int64) {
	const output = int64(16_384)
	if omit || effort == "none" {
		disabled := anthropicsdk.NewThinkingConfigDisabledParam()
		return anthropicsdk.ThinkingConfigParamUnion{OfDisabled: &disabled}, output
	}
	switch effort {
	case "minimal":
		enabled := anthropicsdk.ThinkingConfigEnabledParam{BudgetTokens: 1024}
		return anthropicsdk.ThinkingConfigParamUnion{OfEnabled: &enabled}, output + 1024
	case "low":
		enabled := anthropicsdk.ThinkingConfigEnabledParam{BudgetTokens: 2048}
		return anthropicsdk.ThinkingConfigParamUnion{OfEnabled: &enabled}, output + 2048
	case "high":
		enabled := anthropicsdk.ThinkingConfigEnabledParam{BudgetTokens: 8192}
		return anthropicsdk.ThinkingConfigParamUnion{OfEnabled: &enabled}, output + 8192
	case "xhigh", "max":
		enabled := anthropicsdk.ThinkingConfigEnabledParam{BudgetTokens: 16_384}
		return anthropicsdk.ThinkingConfigParamUnion{OfEnabled: &enabled}, output + 16_384
	default:
		adaptive := anthropicsdk.ThinkingConfigAdaptiveParam{}
		return anthropicsdk.ThinkingConfigParamUnion{OfAdaptive: &adaptive}, output + 8192
	}
}
