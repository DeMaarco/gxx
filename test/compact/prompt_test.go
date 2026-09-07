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

package compact_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"gxx/internal/compact"
)

func TestTranscriptSelectionPreservesGoalAndRecentCorrection(t *testing.T) {
	transcript := "ORIGINAL GOAL: preserve data\n" + strings.Repeat("middle é界\n", 10000) + "LATEST: use beta; tests failed; do not deploy\n"
	got := compact.SelectTranscript(transcript, compact.MaxTranscriptBytes)
	if len(got) > compact.MaxTranscriptBytes || !utf8.ValidString(got) {
		t.Fatal("selection violated byte or UTF-8 bounds")
	}
	for _, want := range []string{"ORIGINAL GOAL", "LATEST: use beta", "middle of transcript omitted"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, size := range []int{0, 1, 40, 41, 42, 100, 1000} {
		part := compact.SelectTranscript(transcript, size)
		if len(part) > size || !utf8.ValidString(part) {
			t.Fatalf("bad bound %d", size)
		}
	}
}

func TestCompactionCarriesConstraintsAndVerification(t *testing.T) {
	for _, want := range []string{"User constraints", "Acceptance criteria", "Verification and outcomes", "Next action", "previous summaries"} {
		if !strings.Contains(compact.SystemPrompt(), want) {
			t.Errorf("missing summary field %q", want)
		}
	}
	text := "Goal: implement save\nUser constraints: do not deploy\nVerification: tests failed\nNext action: fix tests"
	if got := compact.SelectTranscript(text, compact.MaxTranscriptBytes); got != text {
		t.Fatal("short transcript changed")
	}
	fallback := compact.FallbackSummary(text)
	if !strings.Contains(fallback, "Partial continuity") || !strings.Contains(fallback, "tests failed") {
		t.Fatal("fallback concealed missing summary or lost evidence")
	}
}
