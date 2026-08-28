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

package openai_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gxx/internal/openai"

	"github.com/openai/openai-go/v3/responses"
)

func TestDropOldReasoningKeepsLatestTurn(t *testing.T) {
	oldReasoning := responses.ResponseInputItemUnionParam{
		OfReasoning: &responses.ResponseReasoningItemParam{ID: "rs_old"},
	}
	newReasoning := responses.ResponseInputItemUnionParam{
		OfReasoning: &responses.ResponseReasoningItemParam{ID: "rs_new"},
	}
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("first", responses.EasyInputMessageRoleUser),
		oldReasoning,
		responses.ResponseInputItemParamOfMessage("second", responses.EasyInputMessageRoleUser),
		newReasoning,
	}
	got := openai.DropOldReasoning(items)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[1].OfMessage == nil || got[2].OfReasoning == nil || got[2].OfReasoning.ID != "rs_new" {
		t.Fatalf("kept items = %#v", got)
	}
}

func TestDropOldTurnsKeepsLatestTurnAndNotice(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage(strings.Repeat("a", 200), responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("old answer", responses.EasyInputMessageRoleAssistant),
		responses.ResponseInputItemParamOfMessage(strings.Repeat("b", 200), responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfMessage("kept answer", responses.EasyInputMessageRoleAssistant),
	}
	got := openai.DropOldTurns(items, 40, "inst")
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
}

func TestPromptCacheKeyChangesWithInstructions(t *testing.T) {
	first := openai.PromptCacheKey("gpt-5.6-sol", "one")
	second := openai.PromptCacheKey("gpt-5.6-sol", "two")
	if first == second {
		t.Fatal("cache key ignored instructions")
	}
	if !strings.HasPrefix(first, "gxx:gpt-5.6-sol:") {
		t.Fatalf("cache key = %q, want gxx:model:hash", first)
	}
}

func TestUnmatchedCallIDsTracksOpenFunctionCalls(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfFunctionCall(`{}`, "call_1", "read_file"),
		responses.ResponseInputItemParamOfFunctionCallOutput("call_1", "ok"),
		responses.ResponseInputItemParamOfFunctionCall(`{}`, "call_2", "read_file"),
	}
	open := openai.UnmatchedCallIDs(items)
	if !open["call_2"] || open["call_1"] || len(open) != 1 {
		t.Fatalf("open = %#v", open)
	}
}

func TestSlimInputClipsOldToolOutput(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("old", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCallOutput("call_1", strings.Repeat("a", 80)),
		responses.ResponseInputItemParamOfMessage("task", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCallOutput("call_2", strings.Repeat("b", 80)),
	}
	got := openai.SlimInput(items, 1, 0, 40)
	data, _ := json.Marshal(got[1])
	if !strings.Contains(string(data), "eco clipped") {
		t.Fatalf("old tool output = %s, want clipped", data)
	}
	data, _ = json.Marshal(got[3])
	if strings.Contains(string(data), "eco clipped") {
		t.Fatalf("current-turn tool output was clipped: %s", data)
	}
}

func TestClipOldToolOutputsKeepsCurrentTurn(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("old", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCallOutput("old_1", strings.Repeat("a", 80)),
		responses.ResponseInputItemParamOfFunctionCallOutput("old_2", strings.Repeat("b", 80)),
		responses.ResponseInputItemParamOfMessage("now", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCallOutput("new_1", strings.Repeat("c", 80)),
		responses.ResponseInputItemParamOfFunctionCallOutput("new_2", strings.Repeat("d", 80)),
	}
	got := openai.ClipOldToolOutputs(items, 1, 40)
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

func TestSlimInputLevelThreeDropsReasoning(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage(strings.Repeat("old prompt ", 80), responses.EasyInputMessageRoleUser),
		{OfReasoning: &responses.ResponseReasoningItemParam{ID: "rs_1"}},
		responses.ResponseInputItemParamOfMessage("latest", responses.EasyInputMessageRoleUser),
	}
	got := openai.SlimInput(items, 3, 1, 256)
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), "rs_1") {
		t.Fatalf("level 3 kept reasoning: %s", all)
	}
	if !strings.Contains(string(all), "latest") {
		t.Fatalf("level 3 dropped the current prompt: %s", all)
	}
}

func TestKeepLatestReasoningDropsOlderBlobs(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("task", responses.EasyInputMessageRoleUser),
		{OfReasoning: &responses.ResponseReasoningItemParam{ID: "rs_1"}},
		responses.ResponseInputItemParamOfFunctionCall(`{}`, "call_1", "read_file"),
		{OfReasoning: &responses.ResponseReasoningItemParam{ID: "rs_2"}},
	}
	got := openai.KeepLatestReasoning(items)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[2].OfReasoning == nil || got[2].OfReasoning.ID != "rs_2" {
		t.Fatalf("kept = %#v, want latest reasoning", got)
	}
}

func TestDropAllReasoning(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("task", responses.EasyInputMessageRoleUser),
		{OfReasoning: &responses.ResponseReasoningItemParam{ID: "rs_1"}},
	}
	got := openai.DropAllReasoning(items)
	if len(got) != 1 || got[0].OfMessage == nil {
		t.Fatalf("got = %#v, want user message only", got)
	}
}

func TestSummarizeDroppedIncludesToolsAndErrors(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("inspect the repo", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall(`{}`, "call_1", "read_file"),
		responses.ResponseInputItemParamOfFunctionCallOutput("call_1", "error: missing file"),
	}
	got := openai.SummarizeDropped(items)
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
