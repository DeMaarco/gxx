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

package pricing

// Bundled snapshot of official per-1M rates (August 2026). Refresh overwrites
// these from the live pricing pages when a fetch succeeds.
func bundledRates() map[rateKey]Rate {
	rates := map[rateKey]Rate{
		{model: "gpt-5.6-sol"}:                           {Input: 4, Cached: 0.40, CacheWrite: 5, Output: 20},
		{model: "gpt-5.6-sol", long: true}:               {Input: 8, Cached: 0.80, CacheWrite: 10, Output: 30},
		{model: "gpt-5.6-sol", fast: true}:               {Input: 8, Cached: 0.80, CacheWrite: 10, Output: 40},
		{model: "gpt-5.6-sol", fast: true, long: true}:   {Input: 16, Cached: 1.60, CacheWrite: 20, Output: 60},
		{model: "gpt-5.6-terra"}:                         {Input: 2, Cached: 0.20, CacheWrite: 2.50, Output: 12},
		{model: "gpt-5.6-terra", long: true}:             {Input: 4, Cached: 0.40, CacheWrite: 5, Output: 18},
		{model: "gpt-5.6-terra", fast: true}:             {Input: 4, Cached: 0.40, CacheWrite: 5, Output: 24},
		{model: "gpt-5.6-terra", fast: true, long: true}: {Input: 8, Cached: 0.80, CacheWrite: 10, Output: 36},
		{model: "gpt-5.6-luna"}:                          {Input: 0.20, Cached: 0.02, CacheWrite: 0.25, Output: 1.20},
		{model: "gpt-5.6-luna", long: true}:              {Input: 0.40, Cached: 0.04, CacheWrite: 0.50, Output: 1.80},
		{model: "gpt-5.6-luna", fast: true}:              {Input: 0.40, Cached: 0.04, CacheWrite: 0.50, Output: 2.40},
		{model: "gpt-5.6-luna", fast: true, long: true}:  {Input: 0.80, Cached: 0.08, CacheWrite: 1, Output: 3.60},

		{model: "claude-fable-5"}:            {Input: 10, Cached: 1, CacheWrite: 12.50, Output: 50},
		{model: "claude-mythos-5"}:           {Input: 10, Cached: 1, CacheWrite: 12.50, Output: 50},
		{model: "claude-opus-5"}:             {Input: 5, Cached: 0.50, CacheWrite: 6.25, Output: 25},
		{model: "claude-opus-5", fast: true}: {Input: 10, Cached: 1, CacheWrite: 12.50, Output: 50},
		{model: "claude-sonnet-5"}:           {Input: 2, Cached: 0.20, CacheWrite: 2.50, Output: 10},
		{model: "claude-haiku-4-5"}:          {Input: 1, Cached: 0.10, CacheWrite: 1.25, Output: 5},
	}
	return rates
}
