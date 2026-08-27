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

package tools_test

import (
	"strings"
	"testing"

	"gxx/internal/tools"
)

func TestCompactDiffSplitsIndependentHunks(t *testing.T) {
	oldValue := "package main\n\nfunc first() {}\n\nfunc keep() {}\n\nfunc second() {}\n"
	newValue := "package main\n\nfunc one() {}\n\nfunc keep() {}\n\nfunc two() {}\n"
	got := tools.CompactDiff("main.go", oldValue, newValue)
	if strings.Count(got, "@@") != 2 {
		t.Fatalf("diff = %q, want two hunks", got)
	}
	if !strings.Contains(got, "-func first() {}") || !strings.Contains(got, "+func one() {}") {
		t.Fatalf("diff = %q, want first change", got)
	}
	if !strings.Contains(got, "-func second() {}") || !strings.Contains(got, "+func two() {}") {
		t.Fatalf("diff = %q, want second change", got)
	}
	if !strings.Contains(got, " func keep() {}") {
		t.Fatalf("diff = %q, want unchanged context", got)
	}
}

func TestCompactDiffShowsNewFile(t *testing.T) {
	got := tools.CompactDiff("new.txt", "", "hello\n")
	if !strings.Contains(got, "--- new.txt") || !strings.Contains(got, "+hello") {
		t.Fatalf("diff = %q", got)
	}
}
