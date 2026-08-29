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

package models_test

import (
	"fmt"
	"reflect"
	"testing"

	"gxx/internal/config"
	"gxx/internal/models"
)

func TestCatalogEmptyWithoutAccount(t *testing.T) {
	if got := models.Catalog("claude-sonnet-5", "", nil); len(got) != 0 {
		t.Fatalf("catalog without account = %#v", got)
	}
}

func TestCatalogClaudeUsesLatestAliases(t *testing.T) {
	got := models.Latest(config.AccountClaude, []string{
		"claude-sonnet-4-6",
		"claude-sonnet-5",
		"claude-sonnet-5-20260801",
		"claude-opus-4-6",
		"claude-opus-5",
		"claude-haiku-4-5-20251001",
		"claude-fable-5",
		"claude-mythos-preview",
		"claude-mythos-5",
	})
	want := []string{"claude-fable-5", "claude-mythos-5", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("latest = %#v, want %#v", got, want)
	}
}

func TestCatalogClaudeDropsLegacyIDs(t *testing.T) {
	got := models.Latest(config.AccountClaude, []string{
		"claude-2.1",
		"claude-instant-1.2",
		"claude-sonnet-5",
	})
	if len(got) != 1 || got[0] != "claude-sonnet-5" {
		t.Fatalf("latest = %#v", got)
	}
}

func TestCatalogCapsAndSortsExtraLiveModels(t *testing.T) {
	live := []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	for i := 30; i >= 0; i-- {
		live = append(live, fmt.Sprintf("gpt-5.extra-%02d", i))
	}
	got := models.Latest(config.AccountAPI, live)
	if len(got) > 3+models.MaxCatalogExtras {
		t.Fatalf("latest len = %d, want at most %d", len(got), 3+models.MaxCatalogExtras)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] == got[i] {
			t.Fatalf("latest has a duplicate: %#v", got)
		}
	}
	again := models.Latest(config.AccountAPI, live)
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("latest shuffled extras: %#v then %#v", got, again)
	}
}

func TestCatalogHidesOtherFamily(t *testing.T) {
	got := models.Catalog("gpt-5.6-sol", config.AccountAPI, []string{
		"gpt-5.6-sol",
		"claude-sonnet-5",
		"gpt-5.6-terra",
	})
	for _, id := range got {
		if id == "claude-sonnet-5" {
			t.Fatalf("api catalog leaked Claude: %#v", got)
		}
	}
}
