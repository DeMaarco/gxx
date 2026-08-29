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

// Package models ranks live provider model IDs onto the gxx catalog.
package models

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gxx/internal/config"
)

// MaxCatalogExtras caps live IDs that are not a known family. An unbounded
// /models dump overflows the picker and the terminal reprint stacks.
const MaxCatalogExtras = 8

var (
	BundledOpenAI = []string{
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
	}
	BundledClaude = []string{
		"claude-fable-5",
		"claude-mythos-5",
		"claude-opus-5",
		"claude-sonnet-5",
		"claude-haiku-4-5",
	}
)

var datedSuffix = regexp.MustCompile(`-\d{8}$`)

// BundledFor returns the fallback catalog for a connected account.
func BundledFor(account string) []string {
	switch account {
	case config.AccountClaude:
		return append([]string(nil), BundledClaude...)
	case config.AccountOpenAI, config.AccountAPI:
		return append([]string(nil), BundledOpenAI...)
	default:
		return nil
	}
}

// Catalog returns selectable models for the active account.
// live == nil means "use bundled"; a non-nil empty slice means none.
func Catalog(current, account string, live []string) []string {
	if account == "" {
		return nil
	}
	pool := live
	if pool == nil {
		pool = BundledFor(account)
	}
	latest := Latest(account, pool)
	current = config.CanonicalModel(current)
	if current != "" && allowed(account, current) && !contains(latest, current) {
		latest = append([]string{current}, latest...)
	} else if current != "" && contains(latest, current) {
		latest = moveFront(latest, current)
	}
	return latest
}

// Latest keeps the newest alias per family from a live or bundled list.
func Latest(account string, ids []string) []string {
	type picked struct {
		id    string
		score int
		dated bool
	}
	best := map[string]picked{}
	order := familyOrder(account)
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || !allowed(account, id) {
			continue
		}
		family := familyOf(account, id)
		if family == "" {
			continue
		}
		item := picked{
			id:    preferAlias(id),
			score: versionScore(id),
			dated: datedSuffix.MatchString(id),
		}
		prev, ok := best[family]
		if !ok || item.score > prev.score || (item.score == prev.score && prev.dated && !item.dated) {
			best[family] = item
		}
	}
	out := make([]string, 0, len(order))
	seen := map[string]struct{}{}
	for _, family := range order {
		if item, ok := best[family]; ok {
			if _, exists := seen[item.id]; exists {
				continue
			}
			seen[item.id] = struct{}{}
			out = append(out, item.id)
			delete(best, family)
		}
	}
	extras := make([]string, 0, len(best))
	for _, item := range best {
		if _, exists := seen[item.id]; exists {
			continue
		}
		seen[item.id] = struct{}{}
		extras = append(extras, item.id)
	}
	sort.Strings(extras)
	if len(extras) > MaxCatalogExtras {
		extras = extras[:MaxCatalogExtras]
	}
	return append(out, extras...)
}

func allowed(account, id string) bool {
	switch account {
	case config.AccountClaude:
		return config.IsClaudeModel(id)
	case config.AccountOpenAI, config.AccountAPI:
		return config.IsOpenAIModel(id) && isGPTCodingModel(id)
	default:
		return false
	}
}

func isGPTCodingModel(id string) bool {
	lower := strings.ToLower(id)
	for _, skip := range []string{"embed", "whisper", "tts", "transcribe", "audio", "realtime", "image", "dall", "moderation", "search"} {
		if strings.Contains(lower, skip) {
			return false
		}
	}
	return strings.HasPrefix(lower, "gpt-5") || strings.HasPrefix(lower, "gpt-4.1")
}

func familyOrder(account string) []string {
	if account == config.AccountClaude {
		return []string{"fable", "mythos", "opus", "sonnet", "haiku"}
	}
	return []string{"sol", "terra", "luna"}
}

func familyOf(account, id string) string {
	lower := strings.ToLower(id)
	if account == config.AccountClaude {
		switch {
		case strings.Contains(lower, "fable"):
			return "fable"
		case strings.Contains(lower, "mythos"):
			return "mythos"
		case strings.Contains(lower, "opus"):
			return "opus"
		case strings.Contains(lower, "sonnet"):
			return "sonnet"
		case strings.Contains(lower, "haiku"):
			return "haiku"
		default:
			return ""
		}
	}
	switch {
	case strings.Contains(lower, "sol"):
		return "sol"
	case strings.Contains(lower, "terra"):
		return "terra"
	case strings.Contains(lower, "luna"):
		return "luna"
	default:
		return preferAlias(lower)
	}
}

func preferAlias(id string) string {
	return datedSuffix.ReplaceAllString(id, "")
}

var versionPattern = regexp.MustCompile(`(\d+)(?:[.-](\d+))?`)

func versionScore(id string) int {
	best := 0
	for _, match := range versionPattern.FindAllStringSubmatch(preferAlias(id), -1) {
		major, _ := strconv.Atoi(match[1])
		minor := 0
		if match[2] != "" {
			minor, _ = strconv.Atoi(match[2])
		}
		score := major*1000 + minor
		if score > best {
			best = score
		}
	}
	return best
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func moveFront(values []string, want string) []string {
	out := make([]string, 0, len(values))
	out = append(out, want)
	for _, value := range values {
		if value != want {
			out = append(out, value)
		}
	}
	return out
}
