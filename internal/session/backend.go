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

package session

import (
	"gxx/internal/agent"
	anthropicProvider "gxx/internal/anthropic"
	"gxx/internal/auth/claude"
	openaiAuth "gxx/internal/auth/openai"
	"gxx/internal/config"
	openaiProvider "gxx/internal/openai"
	"gxx/internal/workspace"
	"strings"
)

func NewBackend(settings config.Config, ws *workspace.Workspace) (agent.Backend, error) {
	instructions := ""
	if ws != nil {
		instructions = agent.SystemPromptWithOptions(ws, false, false, 0)
	}
	switch config.ProviderForModel(settings.Model) {
	case config.ProviderAnthropic:
		provider := anthropicProvider.New(
			claude.NewSource(nil),
			settings.Model,
			instructions,
			settings.APITimeout,
		)
		provider.SetEffort(settings.Effort)
		provider.SetContext(settings.Context)
		provider.SetFast(settings.Fast)
		return provider, nil
	default:
		var provider *openaiProvider.Provider
		if settings.HasOpenAIAPIKey() {
			provider = openaiProvider.New(
				settings.APIKey,
				settings.Model,
				instructions,
				settings.APITimeout,
			)
		} else if strings.TrimSpace(settings.OpenAITokens.AccessToken) != "" {
			provider = openaiProvider.NewWithSource(
				openaiAuth.NewSource(nil),
				settings.Model,
				instructions,
				settings.APITimeout,
			)
		} else {
			provider = openaiProvider.New(
				"",
				settings.Model,
				instructions,
				settings.APITimeout,
			)
		}
		provider.SetEffort(settings.Effort)
		provider.SetContext(settings.Context)
		provider.SetFast(settings.Fast)
		return provider, nil
	}
}
