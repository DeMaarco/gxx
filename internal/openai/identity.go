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

import "strings"

// Codex backend identity. Isolated so a header or host change stays in one file.
// Do not impersonate the official Codex CLI originator.
const (
	codexAPIBaseURL          = "https://chatgpt.com/backend-api/codex"
	codexBetaHeader          = "responses=experimental"
	codexOriginator          = "gxx"
	codexAccountHdr          = "ChatGPT-Account-ID"
	codexResponsesLiteHeader = "x-openai-internal-codex-responses-lite"

	defaultCodexInstructions = "You are gxx, a local coding agent."
)

// usesResponsesLite reports whether ChatGPT Codex expects the Responses Lite
// wire format for this model: tools in an additional_tools input item and the
// x-openai-internal-codex-responses-lite header. gpt-5.6-sol/terra/luna emit
// program items instead of function_call unless that contract is used.
func usesResponsesLite(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-5.6-")
	}
}
