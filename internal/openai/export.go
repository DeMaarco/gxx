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
	UnmatchedCallIDs  = unmatchedCallIDs
	OutputItemParam   = outputItemParam
	ResponseText      = responseText
	ToolParams        = toolParams
	FetchAccountUsage = fetchAccountUsage
	ParseAPIError     = parseAPIError
	ParseRateLimit    = parseRateLimit
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
