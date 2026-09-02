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
	if len(provider.History()) != 0 {
		t.Fatalf("history after overflow = %d items, want user append rolled back", len(provider.History()))
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

func TestTokenFactorCalibratesContextSnapshot(t *testing.T) {
	provider := openai.New("test-key", "gpt-5.6", strings.Repeat("x", 64), time.Second)
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("hello from the user", responses.EasyInputMessageRoleUser),
	})
	provider.SetTokenFactor(1.0)
	provider.RefreshContext()
	base := provider.ContextSnapshot()

	provider.SetTokenFactor(2.0)
	provider.RefreshContext()
	scaled := provider.ContextSnapshot()
	if scaled.UsedTokens < base.UsedTokens*2-1 || scaled.UsedTokens > base.UsedTokens*2+1 {
		t.Fatalf("factor 2.0 used = %d, base = %d", scaled.UsedTokens, base.UsedTokens)
	}
	if scaled.UsedTokens <= base.UsedTokens {
		t.Fatalf("used should rise with factor: base=%d scaled=%d", base.UsedTokens, scaled.UsedTokens)
	}
}

func TestTokenFactorDefaultIsOneWithoutUsage(t *testing.T) {
	provider := openai.New("test-key", "gpt-5.6", "inst", time.Second)
	if provider.TokenFactor() != 1.0 {
		t.Fatalf("default factor = %v, want 1.0", provider.TokenFactor())
	}
	provider.SetHistory([]responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("hello", responses.EasyInputMessageRoleUser),
	})
	provider.RefreshContext()
	withFactor := provider.ContextSnapshot()
	provider.SetTokenFactor(1.0)
	provider.RefreshContext()
	baseline := provider.ContextSnapshot()
	if withFactor.UsedTokens != baseline.UsedTokens {
		t.Fatalf("unused factor should match 1.0: got %d want %d", withFactor.UsedTokens, baseline.UsedTokens)
	}
}
