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

package anthropic_test

import (
	"context"
	"strings"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/agent"
	"gxx/internal/anthropic"
	"gxx/internal/config"
)

func TestSystemBlocksStartWithOAuthIdentity(t *testing.T) {
	blocks := anthropic.SystemBlocks("You are gxx.")
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	if blocks[0].Text != anthropic.OAuthIdentity {
		t.Fatalf("first block = %q", blocks[0].Text)
	}
	if blocks[1].Text != "You are gxx." {
		t.Fatalf("second block = %q", blocks[1].Text)
	}
}

func TestToolParamsMapsDefinitions(t *testing.T) {
	params := anthropic.ToolParams([]agent.ToolDefinition{{
		Name:        "list_files",
		Description: "List workspace files",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}}, 0)
	if len(params) != 1 || params[0].OfTool == nil {
		t.Fatalf("params = %#v", params)
	}
	if params[0].OfTool.Name != "list_files" {
		t.Fatalf("name = %q", params[0].OfTool.Name)
	}
	if params[0].OfTool.Description.Value != "List workspace files" {
		t.Fatalf("description = %#v", params[0].OfTool.Description)
	}
}

func TestThinkingForEffort(t *testing.T) {
	thinking, maxTokens := anthropic.ThinkingFor("none", false)
	if thinking.OfDisabled == nil || maxTokens <= 0 {
		t.Fatalf("none = %#v %d", thinking, maxTokens)
	}
	thinking, _ = anthropic.ThinkingFor("high", false)
	if thinking.OfEnabled == nil || thinking.OfEnabled.BudgetTokens != 8192 {
		t.Fatalf("high = %#v", thinking)
	}
	thinking, _ = anthropic.ThinkingFor("medium", false)
	if thinking.OfAdaptive == nil {
		t.Fatalf("medium = %#v", thinking)
	}
	thinking, _ = anthropic.ThinkingFor("medium", true)
	if thinking.OfDisabled == nil {
		t.Fatalf("omit reasoning = %#v", thinking)
	}
}

func TestUnmatchedToolIDsAndAbsorb(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", "hi", time.Second)
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call-1", map[string]any{"path": "."}, "list_files")),
	})
	open := anthropic.UnmatchedIDs(provider.History())
	if !open["call-1"] {
		t.Fatalf("open = %#v", open)
	}
	provider.AbsorbToolResults([]agent.ToolResult{{
		CallID: "call-1",
		Name:   "list_files",
		Output: "ok",
	}})
	if leftover := anthropic.UnmatchedIDs(provider.History()); len(leftover) != 0 {
		t.Fatalf("leftover = %#v", leftover)
	}
}

func TestContextSnapshotCountsInstructions(t *testing.T) {
	provider := anthropic.New(anthropic.StaticToken("tok"), "claude-sonnet-4-6", strings.Repeat("x", 64), time.Second)
	empty := provider.ContextSnapshot()
	if empty.InstructionsTokens <= 0 || empty.WindowTokens != int64(config.ContextTokensFor(config.ProviderAnthropic, config.DefaultContext)) {
		t.Fatalf("empty snapshot = %+v", empty)
	}
	provider.SetHistory([]anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hello there")),
	})
	provider.RefreshContext()
	used := provider.ContextSnapshot()
	if used.UserTokens <= 0 || used.UsedTokens <= empty.UsedTokens {
		t.Fatalf("used snapshot = %+v empty = %+v", used, empty)
	}
}

func TestRespondRequiresToken(t *testing.T) {
	provider := anthropic.New(nil, "claude-sonnet-4-6", "hi", time.Second)
	_, err := provider.Respond(context.Background(), agent.ModelInput{UserText: "hi"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("error = %v", err)
	}
}
