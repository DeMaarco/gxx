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

// Codex backend identity. Isolated so a header or host change stays in one file.
// Do not impersonate the official Codex CLI originator.
const (
	codexAPIBaseURL = "https://chatgpt.com/backend-api/codex"
	codexBetaHeader = "responses=experimental"
	codexOriginator = "gxx"
	codexAccountHdr = "ChatGPT-Account-ID"

	defaultCodexInstructions = "You are gxx, a local coding agent."
)
