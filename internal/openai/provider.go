package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"gxx/internal/agent"
	"gxx/internal/config"
)

const fallbackHistoryItems = 256

// Provider implements a store:false Responses conversation. It resends the
// completed output items, including encrypted reasoning, on each turn.
type Provider struct {
	client        openaisdk.Client
	apiKey        string
	model         string
	effort        string
	contextTokens int
	fast          bool
	instructions  string
	timeout       time.Duration

	mu              sync.Mutex
	generation      uint64
	history         []responses.ResponseInputItemUnionParam
	session         agent.Usage
	sessionRequests int64
	rateLimit       agent.RateLimit
	contextUsage    agent.ContextUsage
	httpClient      *http.Client
	baseURL         string
}

func New(apiKey, model, instructions string, timeout time.Duration) *Provider {
	apiKey = strings.TrimSpace(apiKey)
	provider := &Provider{
		client:        newClient(apiKey),
		apiKey:        apiKey,
		model:         model,
		effort:        "medium",
		contextTokens: config.ContextTokens(config.DefaultContext),
		instructions:  instructions,
		timeout:       timeout,
		httpClient:    &http.Client{Timeout: usageFetchTimeout},
		baseURL:       defaultAPIBaseURL,
	}
	provider.refreshContextLocked()
	return provider
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

func (p *Provider) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = model
}

func (p *Provider) SetContext(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.contextTokens = config.ContextTokens(value)
	p.refreshContextLocked()
}

func (p *Provider) SetFast(fast bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fast = fast
}

func (p *Provider) Respond(
	ctx context.Context,
	input agent.ModelInput,
	definitions []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	var result agent.ModelResponse

	p.mu.Lock()
	if p.apiKey == "" {
		p.mu.Unlock()
		return result, errors.New("OpenAI API key is not configured; run /config")
	}

	hasUserText := strings.TrimSpace(input.UserText) != ""
	if len(input.ToolResults) > 0 {
		p.appendToolResultsLocked(input.ToolResults)
	}
	if hasUserText {
		if n := p.closeOpenFunctionCallsLocked(unansweredToolOutput); n > 0 {
			agent.Emit(emit, agent.Event{
				Kind: agent.EventNotice,
				Text: "Closed unanswered tool calls from the previous turn.",
			})
		}
		if p.shouldCompact(input.UserText) {
			p.compactLocked(emit)
		}
		p.history = append(p.history, responses.ResponseInputItemParamOfMessage(
			input.UserText,
			responses.EasyInputMessageRoleUser,
		))
	}
	if !hasUserText && len(input.ToolResults) == 0 {
		p.mu.Unlock()
		return result, errors.New("model input contains neither a user message nor tool results")
	}

	staged := append([]responses.ResponseInputItemUnionParam(nil), p.history...)
	generation := p.generation
	timeout := p.timeout
	params := p.requestParamsLocked(staged, definitions)
	client := p.client
	p.refreshContextLocked()
	p.mu.Unlock()

	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var raw *http.Response
	stream := client.Responses.NewStreaming(requestContext, params, option.WithResponseInto(&raw))
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

	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.refreshContextLocked()
	if raw != nil {
		p.rateLimit = parseRateLimit(raw.Header)
	}
	if err := stream.Err(); err != nil {
		return result, err
	}
	if streamError != nil {
		return result, streamError
	}
	if completed == nil {
		return result, errors.New("OpenAI stream ended without a completed response")
	}
	if completed.Status != responses.ResponseStatusCompleted {
		return result, responseStatusError(*completed)
	}

	outputItems := make([]responses.ResponseInputItemUnionParam, 0, len(completed.Output))
	for _, item := range completed.Output {
		param, ok := outputItemParam(item)
		if ok {
			outputItems = append(outputItems, param)
		}
		if item.Type == "function_call" {
			call := item.AsFunctionCall()
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:        call.CallID,
				Name:      call.Name,
				Arguments: json.RawMessage(call.Arguments),
			})
		}
	}

	result.Text = responseText(*completed)
	result.Usage = agent.Usage{
		InputTokens:      completed.Usage.InputTokens,
		OutputTokens:     completed.Usage.OutputTokens,
		ReasoningTokens:  completed.Usage.OutputTokensDetails.ReasoningTokens,
		CachedTokens:     completed.Usage.InputTokensDetails.CachedTokens,
		CacheWriteTokens: completed.Usage.InputTokensDetails.CacheWriteTokens,
		TotalTokens:      completed.Usage.TotalTokens,
	}
	if p.generation == generation {
		p.history = append(staged, outputItems...)
		p.session.Add(result.Usage)
		p.sessionRequests++
	}
	return result, nil
}

func (p *Provider) requestParamsLocked(
	staged []responses.ResponseInputItemUnionParam,
	definitions []agent.ToolDefinition,
) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model:             shared.ResponsesModel(p.model),
		Instructions:      openaisdk.String(p.instructions),
		Store:             openaisdk.Bool(false),
		ParallelToolCalls: openaisdk.Bool(true),
		Truncation:        responses.ResponseNewParamsTruncationAuto,
		PromptCacheKey:    openaisdk.String("gxx"),
		PromptCacheOptions: responses.ResponseNewParamsPromptCacheOptions{
			Mode: "implicit",
			Ttl:  "30m",
		},
		Reasoning: shared.ReasoningParam{
			Effort: shared.ReasoningEffort(p.effort),
		},
		Include: []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: staged,
		},
		Tools: toolParams(definitions),
	}
	if p.fast {
		params.ServiceTier = responses.ResponseNewParamsServiceTierFast
	}
	return params
}

// SetAPIKey replaces the in-memory credential and clears provider history.
func (p *Provider) SetAPIKey(apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return errors.New("API key cannot be empty")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.apiKey = apiKey
	p.client = newClient(apiKey)
	p.generation++
	p.history = nil
	p.session = agent.Usage{}
	p.sessionRequests = 0
	p.rateLimit = agent.RateLimit{}
	p.refreshContextLocked()
	return nil
}

func newClient(apiKey string) openaisdk.Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if admin := strings.TrimSpace(os.Getenv("OPENAI_ADMIN_KEY")); admin != "" {
		opts = append(opts, option.WithAdminAPIKey(admin))
	} else if apiKey != "" {
		// Admin usage endpoints only send Authorization when an admin key is set.
		opts = append(opts, option.WithAdminAPIKey(apiKey))
	}
	return openaisdk.NewClient(opts...)
}

func responseText(response responses.Response) string {
	if text := response.OutputText(); text != "" {
		return text
	}
	var refusals []string
	for _, item := range response.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.AsMessage().Content {
			if content.Type == "refusal" {
				refusals = append(refusals, content.AsRefusal().Refusal)
			}
		}
	}
	return strings.Join(refusals, "\n")
}

func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
	p.history = nil
	p.refreshContextLocked()
}

func toolParams(definitions []agent.ToolDefinition) []responses.ToolUnionParam {
	params := make([]responses.ToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		function := responses.FunctionToolParam{
			Name:        definition.Name,
			Description: openaisdk.String(definition.Description),
			Parameters:  definition.Parameters,
			Strict:      openaisdk.Bool(true),
		}
		params = append(params, responses.ToolUnionParam{OfFunction: &function})
	}
	return params
}

func outputItemParam(item responses.ResponseOutputItemUnion) (responses.ResponseInputItemUnionParam, bool) {
	switch item.Type {
	case "message":
		message := item.AsMessage().ToParam()
		return responses.ResponseInputItemUnionParam{OfOutputMessage: &message}, true
	case "function_call":
		call := item.AsFunctionCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfFunctionCall: &call}, true
	case "reasoning":
		reasoning := item.AsReasoning().ToParam()
		return responses.ResponseInputItemUnionParam{OfReasoning: &reasoning}, true
	default:
		return responses.ResponseInputItemUnionParam{}, false
	}
}

func responseStatusError(response responses.Response) error {
	if message := strings.TrimSpace(response.Error.Message); message != "" {
		return fmt.Errorf("OpenAI response %s: %s", response.Status, message)
	}
	if reason := strings.TrimSpace(response.IncompleteDetails.Reason); reason != "" {
		return fmt.Errorf("OpenAI response %s: %s", response.Status, reason)
	}
	return fmt.Errorf("OpenAI response ended with status %s", response.Status)
}
