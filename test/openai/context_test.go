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
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gxx/internal/openai"

	"github.com/openai/openai-go/v3/responses"

	"gxx/internal/agent"
)

func TestContextSnapshotCountsInstructionsAndHistory(t *testing.T) {
	provider := openai.New("test-key", "gpt-5.6", strings.Repeat("x", 64), time.Second)
	empty := provider.ContextSnapshot()
	if empty.WindowTokens != 272_000 {
		t.Fatalf("window = %d", empty.WindowTokens)
	}
	if empty.InstructionsTokens <= 0 || empty.UsedTokens != empty.InstructionsTokens {
		t.Fatalf("empty snapshot = %+v", empty)
	}

	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("hello from the user", responses.EasyInputMessageRoleUser),
	})
	provider.RefreshContext()
	used := provider.ContextSnapshot()
	if used.UserTokens <= 0 {
		t.Fatalf("user tokens = %+v", used)
	}
	if used.UsedTokens <= used.InstructionsTokens {
		t.Fatalf("used should grow with history: %+v", used)
	}
	if used.Percent != agent.ContextPercent(used.UsedTokens, used.WindowTokens) {
		t.Fatalf("percent = %d", used.Percent)
	}
}

func TestRespondFailsClosedWhenHistoryExceedsWindow(t *testing.T) {
	provider := openai.New("test-key", "gpt-5.6-sol", "inst", time.Second)
	provider.SetContextTokens(40)
	_, err := provider.Respond(
		context.Background(),
		agent.ModelInput{UserText: strings.Repeat("x", 4000)},
		nil,
		nil,
	)
	if !errors.Is(err, openai.ErrContextOverflow) {
		t.Fatalf("Respond() error = %v, want context overflow", err)
	}
}

func TestEmergencyFitClipsCurrentTurnToolOutput(t *testing.T) {
	items := []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("task", responses.EasyInputMessageRoleUser),
		openai.FunctionCallOutputParam("call_1", strings.Repeat("a", 2000)),
	}
	got := openai.EmergencyFit(items, 80, "inst")
	data, _ := json.Marshal(got[len(got)-1])
	if !strings.Contains(string(data), "context clipped") {
		t.Fatalf("current-turn tool output = %s, want emergency clip", data)
	}
}
