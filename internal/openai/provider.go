package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	mu      sync.Mutex
	history []responses.ResponseInputItemUnionParam
}

func New(apiKey, model, instructions string, timeout time.Duration) *Provider {
	apiKey = strings.TrimSpace(apiKey)
	return &Provider{
		client:        newClient(apiKey),
		apiKey:        apiKey,
		model:         model,
		effort:        "medium",
		contextTokens: config.ContextTokens(config.DefaultContext),
		instructions:  instructions,
		timeout:       timeout,
	}
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
	defer p.mu.Unlock()
	if p.apiKey == "" {
		return result, errors.New("OpenAI API key is not configured; run /config")
	}

	hasUserText := strings.TrimSpace(input.UserText) != ""
	if hasUserText && p.overBudget(p.history) {
		p.history = nil
		agent.Emit(emit, agent.Event{
			Kind: agent.EventNotice,
			Text: "Conversation context reset after reaching the local history limit.",
		})
	}

	staged := append([]responses.ResponseInputItemUnionParam(nil), p.history...)
	for _, toolResult := range input.ToolResults {
		staged = append(staged, responses.ResponseInputItemParamOfFunctionCallOutput(
			toolResult.CallID,
			toolResult.Output,
		))
	}
	if len(input.ToolResults) > 0 {
		// Tool outputs resolve function calls already committed to history. Keep
		// them even if the follow-up API request fails so the next turn remains
		// protocol-valid.
		p.history = append([]responses.ResponseInputItemUnionParam(nil), staged...)
	}
	if hasUserText {
		staged = append(staged, responses.ResponseInputItemParamOfMessage(
			input.UserText,
			responses.EasyInputMessageRoleUser,
		))
		// Preserve the user's request across transport errors so a later
		// "continue" still has the task that produced any partial stream.
		p.history = append([]responses.ResponseInputItemUnionParam(nil), staged...)
	}
	if !hasUserText && len(input.ToolResults) == 0 {
		return result, errors.New("model input contains neither a user message nor tool results")
	}

	requestContext, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	params := responses.ResponseNewParams{
		Model:             shared.ResponsesModel(p.model),
		Instructions:      openaisdk.String(p.instructions),
		Store:             openaisdk.Bool(false),
		ParallelToolCalls: openaisdk.Bool(true),
		Truncation:        responses.ResponseNewParamsTruncationAuto,
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

	stream := p.client.Responses.NewStreaming(requestContext, params)
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
	p.history = append(staged, outputItems...)

	result.Text = responseText(*completed)
	result.Usage = agent.Usage{
		InputTokens:     completed.Usage.InputTokens,
		OutputTokens:    completed.Usage.OutputTokens,
		ReasoningTokens: completed.Usage.OutputTokensDetails.ReasoningTokens,
		TotalTokens:     completed.Usage.TotalTokens,
	}
	return result, nil
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
	p.history = nil
	return nil
}

func newClient(apiKey string) openaisdk.Client {
	return openaisdk.NewClient(option.WithAPIKey(apiKey))
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
	p.history = nil
}

func (p *Provider) overBudget(items []responses.ResponseInputItemUnionParam) bool {
	if p.contextTokens <= 0 {
		return len(items) > fallbackHistoryItems
	}
	data, err := json.Marshal(items)
	if err != nil {
		return len(items) > fallbackHistoryItems
	}
	estimated := (len(p.instructions) + len(data)) / 4
	return estimated > p.contextTokens
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
