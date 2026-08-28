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
	DropOldReasoning  = dropOldReasoning
	DropOldTurns      = dropOldTurns
	SummarizeDropped  = summarizeDropped
	UnmatchedCallIDs  = unmatchedCallIDs
	OutputItemParam   = outputItemParam
	ResponseText      = responseText
	ToolParams        = toolParams
	FetchAccountUsage = fetchAccountUsage
	ParseAPIError     = parseAPIError
	ParseRateLimit    = parseRateLimit
	Retryable         = retryable
	PromptCacheKey    = promptCacheKey
)

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
