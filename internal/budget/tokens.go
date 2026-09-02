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

package budget

const FallbackHistoryItems = 256

// EstimateTokens converts serialized byte length into a rough token count.
func EstimateTokens(bytes int) int64 {
	if bytes <= 0 {
		return 0
	}
	return int64(bytes / 4)
}

// Calibrate scales an estimate by a session factor (default 1.0).
func Calibrate(tokens int64, factor float64) int64 {
	if factor <= 0 {
		factor = 1.0
	}
	return int64(float64(tokens) * factor)
}

// UpdateFactor blends observed API input tokens into the running EMA factor.
// estimated and lastInput must both be positive; otherwise old is returned.
func UpdateFactor(old float64, estimated, lastInput int64) float64 {
	if estimated <= 0 || lastInput <= 0 {
		if old <= 0 {
			return 1.0
		}
		return old
	}
	observed := float64(lastInput) / float64(estimated)
	if observed < 0.5 {
		observed = 0.5
	} else if observed > 2.0 {
		observed = 2.0
	}
	if old <= 0 {
		old = 1.0
	}
	return 0.3*observed + 0.7*old
}

// CompactTarget is the soft ceiling used to trigger compaction (window * numer/denom).
func CompactTarget(window, numer, denom, fallbackHalf int) int64 {
	if window <= 0 {
		if fallbackHalf <= 0 {
			fallbackHalf = FallbackHistoryItems / 2
		}
		return int64(fallbackHalf)
	}
	if numer <= 0 || denom <= 0 {
		numer, denom = 2, 3
	}
	return int64(window) * int64(numer) / int64(denom)
}
