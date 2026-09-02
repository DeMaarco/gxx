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
	"encoding/json"
	"strings"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/anthropic"
)

func TestDropOldTurnsKeepsLatestTurnAndNotice(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(strings.Repeat("a", 200))),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(strings.Repeat("b", 200))),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("kept answer")),
	}
	got := anthropic.DropOldTurns(messages, 40, "inst")
	if len(got) < 2 {
		t.Fatalf("len = %d, want compacted tail", len(got))
	}
	first, _ := json.Marshal(got[0])
	if !strings.Contains(string(first), "compacted") {
		t.Fatalf("notice = %s", first)
	}
	if !strings.Contains(string(first), "Prior user requests") {
		t.Fatalf("summary = %s, want dropped user prompt", first)
	}
	if !strings.Contains(string(first), strings.Repeat("a", 160)) {
		t.Fatalf("summary = %s, want clipped dropped prompt", first)
	}
	tail, _ := json.Marshal(got[len(got)-1])
	if !strings.Contains(string(tail), "kept answer") {
		t.Fatalf("tail = %s, want latest assistant turn", tail)
	}
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), strings.Repeat("a", 200)) {
		t.Fatalf("old turn was kept: %s", all)
	}
	if !anthropic.ToolPairsIntact(got) {
		t.Fatal("compaction broke tool pairs")
	}
}

func TestDropOldTurnsPreservesToolPairs(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(strings.Repeat("old-", 80))),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("old answer")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("recent")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call-1", map[string]any{}, "read_file")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call-1", strings.Repeat("out-", 40), false)),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("done")),
	}
	got := anthropic.DropOldTurns(messages, 50, "inst")
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), strings.Repeat("old-", 80)) {
		t.Fatalf("old turn kept: %s", all)
	}
	if !anthropic.ToolPairsIntact(got) {
		t.Fatalf("tool pairs broken: %s", all)
	}
	if strings.Contains(string(all), "call-1") {
		hasUse := strings.Contains(string(all), `"type":"tool_use"`) || strings.Contains(string(all), "tool_use")
		hasResult := strings.Contains(string(all), "tool_result")
		if hasResult && !hasUse {
			t.Fatalf("orphan tool_result: %s", all)
		}
	}
}

func TestSlimInputClipsOldToolOutput(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("old")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call_1", map[string]any{}, "read_file")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call_1", strings.Repeat("a", 80), false)),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("task")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call_2", map[string]any{}, "read_file")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call_2", strings.Repeat("b", 80), false)),
	}
	got := anthropic.SlimInput(messages, 1, 0, 40)
	oldOut, _ := json.Marshal(got[2])
	if !strings.Contains(string(oldOut), "eco clipped") {
		t.Fatalf("old tool output = %s, want clipped", oldOut)
	}
	newOut, _ := json.Marshal(got[5])
	if strings.Contains(string(newOut), "eco clipped") {
		t.Fatalf("current-turn tool output was clipped: %s", newOut)
	}
}

func TestClipOldToolOutputsKeepsCurrentTurn(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("old")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("old_1", strings.Repeat("a", 80), false)),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("old_2", strings.Repeat("b", 80), false)),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("now")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("new_1", strings.Repeat("c", 80), false)),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("new_2", strings.Repeat("d", 80), false)),
	}
	got := anthropic.ClipOldToolOutputs(messages, 1, 40)
	old1, _ := json.Marshal(got[1])
	old2, _ := json.Marshal(got[2])
	new1, _ := json.Marshal(got[4])
	new2, _ := json.Marshal(got[5])
	if !strings.Contains(string(old1), "eco clipped") {
		t.Fatalf("older prior-turn output = %s, want clipped", old1)
	}
	if strings.Contains(string(old2), "eco clipped") {
		t.Fatalf("kept prior-turn output was clipped: %s", old2)
	}
	if strings.Contains(string(new1), "eco clipped") || strings.Contains(string(new2), "eco clipped") {
		t.Fatalf("current-turn outputs were clipped: %s %s", new1, new2)
	}
}

func TestSummarizeDroppedIncludesToolsAndErrors(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("inspect the repo")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call_1", map[string]any{}, "read_file")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call_1", "error: missing file", true)),
	}
	got := anthropic.SummarizeDropped(messages)
	if !strings.Contains(got, "inspect the repo") {
		t.Fatalf("summary = %q, want user prompt", got)
	}
	if !strings.Contains(got, "read_file") {
		t.Fatalf("summary = %q, want tool name", got)
	}
	if !strings.Contains(got, "error: missing file") {
		t.Fatalf("summary = %q, want tool error", got)
	}
}

func TestEmergencyFitClipsCurrentTurnToolOutput(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("task")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call_1", strings.Repeat("a", 2000), false)),
	}
	got := anthropic.EmergencyFit(messages, 80, "inst")
	data, _ := json.Marshal(got[len(got)-1])
	if !strings.Contains(string(data), "context clipped") {
		t.Fatalf("current-turn tool output = %s, want emergency clip", data)
	}
}

func TestEmergencyFitKeepsLatestThinkingWithToolUse(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("old")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewThinkingBlock("sig_old", "old think")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("task")),
		anthropicsdk.NewAssistantMessage(
			anthropicsdk.NewThinkingBlock("sig_new", "keep this think"),
			anthropicsdk.NewToolUseBlock("call_1", map[string]any{"path": "a.go"}, "read_file"),
		),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call_1", strings.Repeat("x", 2000), false)),
	}
	got := anthropic.EmergencyFit(messages, 80, "inst")
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), "old think") {
		t.Fatalf("kept old thinking: %s", all)
	}
	if !strings.Contains(string(all), "keep this think") {
		t.Fatalf("dropped latest thinking: %s", all)
	}
	if !anthropic.ToolPairsIntact(got) {
		t.Fatalf("tool pairs broken: %s", all)
	}
}

func TestDropOldThinkingKeepsLatestTurn(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("first")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewThinkingBlock("sig_old", "old think")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("second")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewThinkingBlock("sig_new", "new think")),
	}
	got := anthropic.DropOldThinking(messages)
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), "old think") {
		t.Fatalf("kept old thinking: %s", all)
	}
	if !strings.Contains(string(all), "new think") {
		t.Fatalf("dropped current thinking: %s", all)
	}
}

func TestKeepLatestThinkingDropsOlderBlobs(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("task")),
		anthropicsdk.NewAssistantMessage(
			anthropicsdk.NewThinkingBlock("sig_1", "first"),
			anthropicsdk.NewToolUseBlock("call_1", map[string]any{}, "read_file"),
			anthropicsdk.NewThinkingBlock("sig_2", "second"),
		),
	}
	got := anthropic.KeepLatestThinking(messages)
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), "first") {
		t.Fatalf("kept older thinking: %s", all)
	}
	if !strings.Contains(string(all), "second") {
		t.Fatalf("dropped latest thinking: %s", all)
	}
}

func TestDropAllThinking(t *testing.T) {
	messages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("task")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewThinkingBlock("sig", "think")),
	}
	got := anthropic.DropAllThinking(messages)
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), "think") {
		t.Fatalf("kept thinking: %s", all)
	}
}
