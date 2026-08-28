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

package config_test

import (
	"testing"

	"gxx/internal/config"
)

func TestApplyEcoLeavesModelAndSessionKnobs(t *testing.T) {
	base := config.Config{
		Model:              "gpt-5.6-luna",
		Effort:             "max",
		Context:            "1m",
		Fast:               true,
		MaxSteps:           40,
		MaxToolResultBytes: 64 * 1024,
	}
	for _, level := range []int{0, 1, 2, 3} {
		got := config.ApplyEco(base, level)
		if got.Model != base.Model || got.Effort != "max" || got.Context != "1m" || !got.Fast || got.MaxSteps != 40 {
			t.Fatalf("level %d changed session knobs: %+v", level, got)
		}
	}
}

func TestApplyEcoSlenderizesInputByLevel(t *testing.T) {
	base := config.Config{MaxToolResultBytes: 64 * 1024}
	off := config.ApplyEco(base, 0)
	if !off.IncludeReasoning || off.CompactNumer != 2 || off.CompactDenom != 3 || off.ToolOutputClip != 0 {
		t.Fatalf("level 0 = %+v", off)
	}

	light := config.ApplyEco(base, 1)
	if light.MaxToolResultBytes != 32*1024 || light.ToolOutputKeep != 4 || light.ToolOutputClip != 2*1024 {
		t.Fatalf("level 1 = %+v", light)
	}
	if light.CompactNumer != 1 || light.CompactDenom != 2 || !light.IncludeReasoning {
		t.Fatalf("level 1 budget = %+v", light)
	}

	medium := config.ApplyEco(base, 2)
	if medium.ToolOutputKeep != 2 || medium.ToolOutputClip != 512 || !medium.IncludeReasoning {
		t.Fatalf("level 2 = %+v", medium)
	}

	max := config.ApplyEco(base, 3)
	if max.IncludeReasoning || max.ToolOutputKeep != 1 || max.ToolOutputClip != 256 {
		t.Fatalf("level 3 = %+v", max)
	}
	if max.MaxToolResultBytes != 8*1024 {
		t.Fatalf("level 3 tool cap = %d", max.MaxToolResultBytes)
	}
}

func TestApplyEcoDoesNotRaiseToolLimit(t *testing.T) {
	got := config.ApplyEco(config.Config{MaxToolResultBytes: 2048}, 1)
	if got.MaxToolResultBytes != 2048 {
		t.Fatalf("should keep tighter tool limit: %+v", got)
	}
}
