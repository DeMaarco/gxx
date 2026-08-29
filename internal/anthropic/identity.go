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

import anthropicsdk "github.com/anthropics/anthropic-sdk-go"

// oauthIdentity is the first system block required by subscription OAuth
// tokens. Isolated here so a header or wording change stays in one file.
const oauthIdentity = "You are Claude Code, Anthropic's official CLI for Claude."

const (
	oauthBetaHeader = "oauth-2025-04-20,claude-code-20250219"
	oauthAppHeader  = "cli"
)

func systemBlocks(instructions string) []anthropicsdk.TextBlockParam {
	blocks := []anthropicsdk.TextBlockParam{{Text: oauthIdentity}}
	if instructions != "" {
		blocks = append(blocks, anthropicsdk.TextBlockParam{Text: instructions})
	}
	return blocks
}
