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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	"gxx/internal/caveman"
	"gxx/internal/config"
)

const fallbackHistoryItems = 256

var errOpenAIUnconfigured = errors.New("OpenAI is not configured; run gxx login openai or /config")

// ErrContextOverflow is returned when history still exceeds the window after
// compaction and emergency clipping. The API is not called.
var ErrContextOverflow = errors.New("context window is full; run /clear or start a new conversation")

// TokenSource returns a usable Codex OAuth access token, refreshing when needed.
type TokenSource interface {
	AccessToken(context.Context) (string, error)
	AccountID(context.Context) (string, error)
}

// Provider implements a store:false Responses conversation. It resends the
// completed output items, including encrypted reasoning, on each turn.
type Provider struct {
	client        openaisdk.Client
	apiKey        string
	tokens        TokenSource
	oauth         bool
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
	sessionID       string
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
		compactNumer:  2,
		compactDenom:  3,
		tokenFactor:   1.0,
		httpClient:    &http.Client{Timeout: usageFetchTimeout},
		baseURL:       defaultAPIBaseURL,
		sessionID:     newSessionID(),
	}
	provider.refreshContextLocked()
	return provider
}

// NewWithSource authenticates against the Codex backend with a ChatGPT account.
func NewWithSource(source TokenSource, model, instructions string, timeout time.Duration) *Provider {
	provider := New("", model, instructions, timeout)
	provider.tokens = source
	provider.oauth = true
	provider.baseURL = codexAPIBaseURL
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

// SetTokenBudget controls how aggressively each request's input is slimmed.
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

func (p *Provider) Respond(
	ctx context.Context,
	input agent.ModelInput,
	definitions []agent.ToolDefinition,
	emit agent.EmitFunc,
) (agent.ModelResponse, error) {
	var result agent.ModelResponse

	if err := p.refreshOAuthClient(ctx); err != nil {
		return result, err
	}

	p.mu.Lock()
	if p.apiKey == "" && !p.oauth {
		p.mu.Unlock()
		return result, errOpenAIUnconfigured
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
	}
	// A long tool loop can outgrow the window without the user typing again,
	// so compaction cannot wait for the next prompt. Dropping whole turns only
	// ever cuts at a user message, which never separates a call from its
	// output.
	if p.shouldCompact(input.UserText) {
		p.compactLocked(emit)
	}
	// Snapshot before the user append so API failure / overflow / cancel can
	// roll back only that message. Tool results from this Respond stay.
	var historyBeforeUser []responses.ResponseInputItemUnionParam
	if hasUserText {
		historyBeforeUser = append([]responses.ResponseInputItemUnionParam(nil), p.history...)
		p.history = append(p.history, responses.ResponseInputItemParamOfMessage(
			input.UserText,
			responses.EasyInputMessageRoleUser,
		))
	}
	if !hasUserText && len(input.ToolResults) == 0 {
		p.mu.Unlock()
		return result, errors.New("model input contains neither a user message nor tool results")
	}
	p.history = dropOldPrograms(dropOldReasoning(p.history))

	staged := slimInput(
		append([]responses.ResponseInputItemUnionParam(nil), p.history...),
		p.ecoLevel,
		p.ecoToolKeep,
		p.ecoToolClip,
	)
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
	client := p.client
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

	var (
		completed *responses.Response
		raw       *http.Response
		lastErr   error
	)
	for attempt := 0; attempt < maxAPIAttempts; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, raw)
			agent.Emit(emit, agent.Event{
				Kind: agent.EventNotice,
				Text: "Retrying OpenAI request…",
			})
			if err := sleepContext(ctx, delay); err != nil {
				rollbackUserAppend()
				return result, err
			}
		}
		requestContext, cancel := context.WithTimeout(ctx, timeout)
		completed, raw, lastErr = streamResponse(requestContext, client, params, emit)
		cancel()
		if lastErr == nil {
			break
		}
		if !retryable(lastErr, ctx, raw) {
			break
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	defer p.refreshContextLocked()
	if raw != nil {
		p.rateLimit = parseRateLimit(raw.Header)
	}
	if lastErr != nil {
		if hasUserText && p.generation == generation {
			p.history = historyBeforeUser
		}
		return result, lastErr
	}

	outputItems := make([]responses.ResponseInputItemUnionParam, 0, len(completed.Output))
	for _, item := range completed.Output {
		param, ok := outputItemParam(item)
		if ok {
			outputItems = append(outputItems, param)
		}
		if call, ok := toolCallFromOutput(item); ok {
			result.ToolCalls = append(result.ToolCalls, call)
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
	if result.Text == "" && len(result.ToolCalls) == 0 {
		agent.Emit(emit, agent.Event{
			Kind: agent.EventNotice,
			Text: emptyOutputNotice(completed.Output),
		})
	}
	if p.generation == generation {
		p.history = append(staged, outputItems...)
		p.session.Add(result.Usage)
		p.sessionRequests++
		p.lastInputTokens = result.Usage.InputTokens
		p.updateTokenFactorLocked(staged)
	}
	return result, nil
}

func (p *Provider) requestParamsLocked(
	staged []responses.ResponseInputItemUnionParam,
	definitions []agent.ToolDefinition,
	finalStep bool,
) responses.ResponseNewParams {
	instructions := p.instructions
	if p.oauth && strings.TrimSpace(instructions) == "" {
		instructions = defaultCodexInstructions
	}
	lite := p.oauth && usesResponsesLite(p.model)
	tools := toolParams(definitions, p.ecoLevel, !p.oauth, lite)
	input := staged
	if lite && len(tools) > 0 {
		input = append([]responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfAdditionalTools(tools),
		}, staged...)
	}
	params := responses.ResponseNewParams{
		Model:             shared.ResponsesModel(p.model),
		Instructions:      openaisdk.String(instructions),
		Store:             openaisdk.Bool(false),
		ParallelToolCalls: openaisdk.Bool(!lite),
		PromptCacheKey:    openaisdk.String(promptCacheKey(p.model, instructions)),
		Reasoning: shared.ReasoningParam{
			Effort: shared.ReasoningEffort(p.effort),
		},
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Tools: tools,
	}
	if lite && len(tools) > 0 {
		// Responses Lite already has the catalog in additional_tools.
		params.Tools = nil
	}
	if lite {
		params.Reasoning.Context = shared.ReasoningContextAllTurns
	}
	if p.oauth {
		params.PromptCacheKey = openaisdk.String(p.sessionID)
		if len(tools) > 0 && !finalStep {
			params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
				OfToolChoiceMode: openaisdk.Opt(responses.ToolChoiceOptionsAuto),
			}
		}
	} else {
		params.Truncation = responses.ResponseNewParamsTruncationDisabled
		params.PromptCacheOptions = responses.ResponseNewParamsPromptCacheOptions{
			Mode: "implicit",
			Ttl:  "30m",
		}
		params.StreamOptions = responses.ResponseNewParamsStreamOptions{
			IncludeObfuscation: openaisdk.Bool(false),
		}
	}
	if !p.omitReasoning {
		params.Include = []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		}
	}
	if finalStep {
		params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openaisdk.Opt(responses.ToolChoiceOptionsNone),
		}
	}
	if p.fast {
		params.ServiceTier = responses.ResponseNewParamsServiceTierFast
	}
	return params
}

func promptCacheKey(model, instructions string) string {
	sum := sha256.Sum256([]byte(instructions))
	return fmt.Sprintf("gxx:%s:%s", model, hex.EncodeToString(sum[:4]))
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
	p.tokens = nil
	p.oauth = false
	p.baseURL = defaultAPIBaseURL
	p.client = newClient(apiKey)
	p.sessionID = newSessionID()
	p.generation++
	p.history = nil
	p.session = agent.Usage{}
	p.sessionRequests = 0
	p.rateLimit = agent.RateLimit{}
	p.lastInputTokens = 0
	p.tokenFactor = 1.0
	p.refreshContextLocked()
	return nil
}

func (p *Provider) refreshOAuthClient(ctx context.Context) error {
	p.mu.Lock()
	source := p.tokens
	oauth := p.oauth
	baseURL := p.baseURL
	p.mu.Unlock()
	if !oauth || source == nil {
		return nil
	}
	token, err := source.AccessToken(ctx)
	if err != nil {
		return err
	}
	accountID, err := source.AccountID(ctx)
	if err != nil {
		return err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return errors.New("ChatGPT account id is missing; run /login openai again")
	}
	p.mu.Lock()
	sessionID := p.sessionID
	p.client = newCodexClient(token, accountID, baseURL, sessionID)
	p.mu.Unlock()
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

func newCodexClient(accessToken, accountID, baseURL, sessionID string) openaisdk.Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = codexAPIBaseURL
	}
	opts := []option.RequestOption{
		option.WithAPIKey(accessToken),
		option.WithBaseURL(strings.TrimRight(baseURL, "/") + "/"),
		option.WithHeader("OpenAI-Beta", codexBetaHeader),
		option.WithHeader("originator", codexOriginator),
		option.WithMiddleware(sanitizeCodexRequest),
		option.WithMaxRetries(0),
	}
	if accountID = strings.TrimSpace(accountID); accountID != "" {
		opts = append(opts, option.WithHeader(codexAccountHdr, accountID))
	}
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		opts = append(opts,
			option.WithHeader("session-id", sessionID),
			option.WithHeader("session_id", sessionID),
		)
	}
	return openaisdk.NewClient(opts...)
}

func newSessionID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("gxx-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
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
	p.sessionID = newSessionID()
	p.history = nil
	p.lastInputTokens = 0
	p.refreshContextLocked()
}

func (p *Provider) ExportHistory() (string, json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.history) == 0 {
		return config.ProviderOpenAI, nil, nil
	}
	data, err := json.Marshal(p.history)
	if err != nil {
		return "", nil, fmt.Errorf("encode openai history: %w", err)
	}
	return config.ProviderOpenAI, data, nil
}

func (p *Provider) ImportHistory(provider string, history json.RawMessage) error {
	if strings.TrimSpace(provider) != config.ProviderOpenAI {
		return fmt.Errorf("history provider %q does not match openai", provider)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(history) == 0 {
		p.generation++
		p.sessionID = newSessionID()
		p.history = nil
		p.lastInputTokens = 0
		p.refreshContextLocked()
		return nil
	}
	var items []responses.ResponseInputItemUnionParam
	items, err := decodeOpenAIHistory(history)
	if err != nil {
		return err
	}
	p.generation++
	p.sessionID = newSessionID()
	p.history = items
	p.lastInputTokens = 0
	p.refreshContextLocked()
	return nil
}

func toolParams(definitions []agent.ToolDefinition, eco int, strict, programmatic bool) []responses.ToolUnionParam {
	if len(definitions) == 0 {
		// An empty slice serializes to "tools": [], which withdraws the tool
		// namespace. Omit the field instead.
		return nil
	}
	params := make([]responses.ToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		description := definition.Description
		parameters := definition.Parameters
		if eco > 0 {
			description = caveman.Compress(description, eco)
			parameters = compressToolParameters(parameters, eco)
		}
		function := responses.FunctionToolParam{
			Name:        definition.Name,
			Description: openaisdk.String(description),
			Parameters:  parameters,
		}
		if strict {
			function.Strict = openaisdk.Bool(true)
		}
		if programmatic {
			function.AllowedCallers = []string{"direct", "programmatic"}
		}
		params = append(params, responses.ToolUnionParam{OfFunction: &function})
	}
	return params
}

func compressToolParameters(parameters map[string]any, eco int) map[string]any {
	if len(parameters) == 0 {
		return parameters
	}
	compressed, ok := caveman.CompressDescriptions(cloneJSON(parameters), eco).(map[string]any)
	if !ok {
		return parameters
	}
	return compressed
}

func cloneJSON(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if json.Unmarshal(data, &cloned) != nil {
		return value
	}
	return cloned
}

func outputItemParam(item responses.ResponseOutputItemUnion) (responses.ResponseInputItemUnionParam, bool) {
	switch item.Type {
	case "message":
		message := item.AsMessage().ToParam()
		return responses.ResponseInputItemUnionParam{OfOutputMessage: &message}, true
	case "function_call":
		call := item.AsFunctionCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfFunctionCall: &call}, true
	case "custom_tool_call":
		call := item.AsCustomToolCall()
		return responses.ResponseInputItemParamOfFunctionCall(call.Input, call.CallID, call.Name), true
	case "shell_call":
		call, ok := toolCallFromOutput(item)
		if !ok {
			return responses.ResponseInputItemUnionParam{}, false
		}
		return responses.ResponseInputItemParamOfFunctionCall(string(call.Arguments), call.ID, call.Name), true
	case "local_shell_call":
		call, ok := toolCallFromOutput(item)
		if !ok {
			return responses.ResponseInputItemUnionParam{}, false
		}
		return responses.ResponseInputItemParamOfFunctionCall(string(call.Arguments), call.ID, call.Name), true
	case "apply_patch_call":
		call, ok := toolCallFromOutput(item)
		if !ok {
			return responses.ResponseInputItemUnionParam{}, false
		}
		return responses.ResponseInputItemParamOfFunctionCall(string(call.Arguments), call.ID, call.Name), true
	case "program":
		program := item.AsProgram()
		param := responses.ResponseInputItemProgramParam{
			ID:          program.ID,
			CallID:      program.CallID,
			Code:        program.Code,
			Fingerprint: program.Fingerprint,
		}
		return responses.ResponseInputItemUnionParam{OfProgram: &param}, true
	case "program_output":
		output := item.AsProgramOutput()
		param := responses.ResponseInputItemProgramOutputParam{
			ID:     output.ID,
			CallID: output.CallID,
			Result: output.Result,
			Status: output.Status,
		}
		return responses.ResponseInputItemUnionParam{OfProgramOutput: &param}, true
	case "reasoning":
		reasoning := item.AsReasoning().ToParam()
		return responses.ResponseInputItemUnionParam{OfReasoning: &reasoning}, true
	default:
		return responses.ResponseInputItemUnionParam{}, false
	}
}

func toolCallFromOutput(item responses.ResponseOutputItemUnion) (agent.ToolCall, bool) {
	name := strings.TrimSpace(item.Name)
	id := strings.TrimSpace(item.CallID)
	args := item.Arguments.OfString
	switch item.Type {
	case "function_call":
		call := item.AsFunctionCall()
		if name == "" {
			name = strings.TrimSpace(call.Name)
		}
		if id == "" {
			id = strings.TrimSpace(call.CallID)
		}
		if args == "" {
			args = call.Arguments
		}
	case "custom_tool_call":
		call := item.AsCustomToolCall()
		if name == "" {
			name = strings.TrimSpace(call.Name)
		}
		if id == "" {
			id = strings.TrimSpace(call.CallID)
		}
		if args == "" {
			args = call.Input
		}
		if args == "" {
			args = item.Input
		}
	case "shell_call":
		call := item.AsShellCall()
		if id == "" {
			id = strings.TrimSpace(call.CallID)
		}
		name = "run_command"
		command := strings.Join(call.Action.Commands, " && ")
		if strings.TrimSpace(command) == "" {
			return agent.ToolCall{}, false
		}
		encoded, err := json.Marshal(map[string]any{"command": command})
		if err != nil {
			return agent.ToolCall{}, false
		}
		args = string(encoded)
	case "local_shell_call":
		call := item.AsLocalShellCall()
		if id == "" {
			id = strings.TrimSpace(call.CallID)
		}
		name = "run_command"
		command := strings.Join(call.Action.Command, " ")
		if strings.TrimSpace(command) == "" {
			return agent.ToolCall{}, false
		}
		encoded, err := json.Marshal(map[string]any{"command": command})
		if err != nil {
			return agent.ToolCall{}, false
		}
		args = string(encoded)
	case "apply_patch_call":
		call := item.AsApplyPatchCall()
		if id == "" {
			id = strings.TrimSpace(call.CallID)
		}
		mapped, ok := applyPatchCallArgs(call)
		if !ok {
			return agent.ToolCall{}, false
		}
		name = "apply_patch"
		args = mapped
	default:
		return agent.ToolCall{}, false
	}
	if name == "" {
		return agent.ToolCall{}, false
	}
	if args == "" {
		args = "{}"
	}
	if !json.Valid([]byte(args)) {
		encoded, err := json.Marshal(args)
		if err != nil {
			return agent.ToolCall{}, false
		}
		args = string(encoded)
	}
	return agent.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}, true
}

func applyPatchCallArgs(call responses.ResponseApplyPatchToolCall) (string, bool) {
	path := strings.TrimSpace(call.Operation.Path)
	if path == "" {
		return "", false
	}
	change := map[string]any{"path": path}
	switch call.Operation.Type {
	case "delete_file":
		change["action"] = "delete"
	case "create_file":
		change["action"] = "add"
		change["content"] = call.Operation.Diff
	default:
		return "", false
	}
	encoded, err := json.Marshal(map[string]any{"changes": []map[string]any{change}})
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func emptyOutputNotice(items []responses.ResponseOutputItemUnion) string {
	if len(items) == 0 {
		return "Model finished without text or tool calls."
	}
	seen := make(map[string]bool, len(items))
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		kind := strings.TrimSpace(item.Type)
		if kind == "" {
			kind = "unknown"
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	return "Model finished without text or tool calls (output: " + strings.Join(kinds, ", ") + ")."
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
