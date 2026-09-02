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

package budget_test

import (
	"strings"
	"testing"

	"gxx/internal/budget"
)

func TestClipHelpers(t *testing.T) {
	if got := budget.ClipRunes("abcdef", 3); got != "abc…" {
		t.Fatalf("ClipRunes = %q", got)
	}
	if got := budget.ClipBytes("abcdef", 3); got != "abc" {
		t.Fatalf("ClipBytes = %q", got)
	}
	got := budget.LastStrings([]string{"a", "b", "c", "d"}, 2)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("LastStrings = %#v", got)
	}
}

func TestFormatDroppedSummary(t *testing.T) {
	got := budget.FormatDroppedSummary(
		[]string{"old request"},
		[]string{"read_file"},
		[]string{"error: missing"},
	)
	if !strings.HasPrefix(got, budget.CompactNotice) {
		t.Fatalf("prefix = %q", got)
	}
	for _, want := range []string{"Prior user requests", "old request", "Tools used: read_file", "Recent tool errors", "error: missing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q: %s", want, got)
		}
	}
}
