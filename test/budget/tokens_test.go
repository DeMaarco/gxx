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
	"testing"

	"gxx/internal/budget"
)

func TestEstimateTokens(t *testing.T) {
	if got := budget.EstimateTokens(0); got != 0 {
		t.Fatalf("zero = %d", got)
	}
	if got := budget.EstimateTokens(7); got != 1 {
		t.Fatalf("7 bytes = %d, want 1", got)
	}
	if got := budget.EstimateTokens(400); got != 100 {
		t.Fatalf("400 bytes = %d, want 100", got)
	}
}

func TestCalibrateAndUpdateFactor(t *testing.T) {
	if got := budget.Calibrate(100, 0); got != 100 {
		t.Fatalf("default factor = %d", got)
	}
	if got := budget.Calibrate(100, 2); got != 200 {
		t.Fatalf("2x = %d", got)
	}
	factor := budget.UpdateFactor(1.0, 100, 200)
	if factor <= 1.0 || factor > 2.0 {
		t.Fatalf("factor = %v, want between 1 and 2", factor)
	}
	if got := budget.UpdateFactor(1.0, 0, 100); got != 1.0 {
		t.Fatalf("zero estimate should keep factor: %v", got)
	}
	clamped := budget.UpdateFactor(1.0, 10, 1000)
	want := 0.3*2.0 + 0.7*1.0
	if clamped < want-1e-9 || clamped > want+1e-9 {
		t.Fatalf("clamp high = %v, want %v", clamped, want)
	}
}

func TestCompactTarget(t *testing.T) {
	if got := budget.CompactTarget(300, 2, 3, 128); got != 200 {
		t.Fatalf("2/3 of 300 = %d, want 200", got)
	}
	if got := budget.CompactTarget(0, 2, 3, 128); got != 128 {
		t.Fatalf("fallback = %d", got)
	}
	if got := budget.CompactTarget(300, 0, 0, 128); got != 200 {
		t.Fatalf("default ratio = %d", got)
	}
}
