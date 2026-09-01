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
	"net/http"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

var (
	DropOldReasoning        = dropOldReasoning
	DropOldPrograms         = dropOldPrograms
	DropAllPrograms         = dropAllPrograms
	DropAllReasoning        = dropAllReasoning
	KeepLatestReasoning     = keepLatestReasoning
	SlimInput               = slimInput
	ClipOldToolOutputs      = clipOldToolOutputs
	DropOldTurns            = dropOldTurns
	SummarizeDropped        = summarizeDropped
	UnmatchedCallIDs        = unmatchedCallIDs
	OutputItemParam         = outputItemParam
	ToolCallFromOutput      = toolCallFromOutput
	ResponseText            = responseText
	FetchAccountUsage       = fetchAccountUsage
	FetchChatGPTUsage       = fetchChatGPTUsage
	ParseChatGPTUsage       = parseChatGPTUsage
	ChatGPTUsageURL         = chatgptUsageURL
	ParseAPIError           = parseAPIError
	ParseRateLimit          = parseRateLimit
	Retryable               = retryable
	RetryDelay              = retryDelay
	MaxRetryAfter           = maxRetryAfter
	PromptCacheKey          = promptCacheKey
	EmergencyFit            = emergencyFit
	FunctionCallOutputParam = functionCallOutputParam
	SanitizeCodexPayload    = sanitizeCodexPayload
)

func ToolParams(definitions []agent.ToolDefinition) []responses.ToolUnionParam {
	return toolParams(definitions, 0, true, false)
}

func (p *Provider) SetHistory(items []responses.ResponseInputItemUnionParam) {
	p.history = items
}

func (p *Provider) History() []responses.ResponseInputItemUnionParam {
	return p.history
}

func (p *Provider) SetClient(client openaisdk.Client) {
	p.client = client
}

func (p *Provider) SetHTTPClient(client *http.Client) {
	p.httpClient = client
}

func (p *Provider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

func (p *Provider) APIKey() string {
	return p.apiKey
}

func (p *Provider) UsingOAuth() bool {
	return p.oauth
}

func (p *Provider) BaseURL() string {
	return p.baseURL
}

func CodexAPIBaseURL() string {
	return codexAPIBaseURL
}

func CodexAccountHeader() string {
	return codexAccountHdr
}

func CodexOriginator() string {
	return codexOriginator
}

func CodexBetaHeader() string {
	return codexBetaHeader
}

func CodexResponsesLiteHeader() string {
	return codexResponsesLiteHeader
}

func UsesResponsesLite(model string) bool {
	return usesResponsesLite(model)
}

func (p *Provider) ContextTokens() int {
	return p.contextTokens
}

func (p *Provider) SetContextTokens(n int) {
	p.contextTokens = n
}

func (p *Provider) SetLastInputTokens(n int64) {
	p.lastInputTokens = n
}

func (p *Provider) SessionUsage() agent.Usage {
	return p.session
}

func (p *Provider) SessionRequests() int64 {
	return p.sessionRequests
}

func (p *Provider) RateLimitState() agent.RateLimit {
	return p.rateLimit
}

func (p *Provider) OverBudget(items []responses.ResponseInputItemUnionParam) bool {
	return p.overBudget(items)
}

func (p *Provider) RefreshContext() {
	p.refreshContextLocked()
}
