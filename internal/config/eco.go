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

package config

const (
	EcoOff = 0
	EcoMin = 1
	EcoMax = 3
)

// EcoState slims request input. It never changes the selected model.
type EcoState struct {
	Config
	Level            int
	CompactNumer     int
	CompactDenom     int
	IncludeReasoning bool
	ToolOutputKeep   int
	ToolOutputClip   int
}

// ApplyEco returns input-slimming knobs for the given eco level.
// Model, effort, context, and fast are left as they are.
func ApplyEco(c Config, level int) EcoState {
	state := EcoState{
		Config:           c,
		Level:            level,
		CompactNumer:     2,
		CompactDenom:     3,
		IncludeReasoning: true,
	}
	switch level {
	case 1:
		state.MaxToolResultBytes = minPositive(c.MaxToolResultBytes, 32*1024)
		state.CompactNumer, state.CompactDenom = 1, 2
		state.ToolOutputKeep = 4
		state.ToolOutputClip = 2 * 1024
	case 2:
		state.MaxToolResultBytes = minPositive(c.MaxToolResultBytes, 16*1024)
		state.CompactNumer, state.CompactDenom = 2, 5
		state.ToolOutputKeep = 2
		state.ToolOutputClip = 512
	case 3:
		state.MaxToolResultBytes = minPositive(c.MaxToolResultBytes, 8*1024)
		state.CompactNumer, state.CompactDenom = 1, 3
		state.IncludeReasoning = false
		state.ToolOutputKeep = 1
		state.ToolOutputClip = 256
	default:
		state.Level = EcoOff
	}
	return state
}

func minPositive(base, cap int) int {
	if cap <= 0 {
		return base
	}
	if base <= 0 || base > cap {
		return cap
	}
	return base
}
