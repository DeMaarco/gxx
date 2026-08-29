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
	"net/http"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/agent"
)

var (
	OAuthIdentity   = oauthIdentity
	SystemBlocks    = systemBlocks
	ToolParams      = toolParams
	ThinkingFor     = thinkingForEffort
	UnmatchedIDs    = unmatchedToolIDs
	AssistantText   = assistantText
	FetchOAuthUsage = fetchOAuthUsage
	ParseOAuthUsage = parseOAuthUsage
	OAuthUsageURL   = oauthUsageURL
)

func (p *Provider) SetHistory(items []anthropicsdk.MessageParam) {
	p.history = items
}

func (p *Provider) History() []anthropicsdk.MessageParam {
	return p.history
}

func (p *Provider) SetHTTPClient(client *http.Client) {
	p.httpClient = client
}

func (p *Provider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

func (p *Provider) SetSource(source TokenSource) {
	p.source = source
}

func (p *Provider) AccessToken() string {
	return p.accessToken
}

func (p *Provider) RefreshContext() {
	p.refreshContextLocked()
}

func (p *Provider) OverBudget(items []anthropicsdk.MessageParam) bool {
	return p.overBudget(items)
}

func StaticToken(token string) TokenSource {
	return staticToken(token)
}

type staticToken string

func (s staticToken) AccessToken(context.Context) (string, error) {
	return string(s), nil
}

func ToolCalls(message anthropicsdk.Message) []agent.ToolCall {
	return assistantToolCalls(message)
}
