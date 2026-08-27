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
	tail, _ := json.Marshal(got[len(got)-1])
	if !strings.Contains(string(tail), "kept answer") {
		t.Fatalf("tail = %s, want latest assistant turn", tail)
	}
	all, _ := json.Marshal(got)
	if strings.Contains(string(all), strings.Repeat("a", 200)) {
		t.Fatalf("old turn was kept: %s", all)
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
