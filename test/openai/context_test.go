package openai_test

import (
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
