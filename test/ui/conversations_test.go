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

package ui_test

import (
	"context"
	"io"
	"testing"
	"time"

	"gxx/internal/ui"
)

func TestConversationMenuSelectsEntry(t *testing.T) {
	menu := ui.NewConversationMenu([]ui.ConversationEntry{
		{ID: "a", Title: "First"},
		{ID: "b", Title: "Second"},
	})
	menu.Apply(ui.KeyDown)
	done, id := menu.Apply(ui.KeyEnter)
	if !done || id != "b" {
		t.Fatalf("select second = done=%v id=%q, want b", done, id)
	}
}

func TestConversationMenuEscCancels(t *testing.T) {
	menu := ui.NewConversationMenu([]ui.ConversationEntry{
		{ID: "a", Title: "First"},
	})
	done, id := menu.Apply(ui.KeyEsc)
	if !done || id != "" {
		t.Fatalf("esc = done=%v id=%q, want cancel", done, id)
	}
}

func TestConversationMenuEmptyEnterDismisses(t *testing.T) {
	menu := ui.NewConversationMenu(nil)
	done, id := menu.Apply(ui.KeyEnter)
	if !done || id != "" {
		t.Fatalf("enter on empty = done=%v id=%q", done, id)
	}
}

func TestReadConversationChoiceRequiresTerminal(t *testing.T) {
	_, err := ui.ReadConversationChoice(context.Background(), nil, io.Discard, nil, false)
	if err == nil {
		t.Fatal("expected terminal error")
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		when time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-30 * time.Hour), "yesterday"},
		{now.Add(-96 * time.Hour), "Aug 28"},
	}
	for _, tc := range cases {
		if got := ui.FormatRelativeTime(tc.when, now); got != tc.want {
			t.Fatalf("FormatRelativeTime(%v) = %q, want %q", tc.when, got, tc.want)
		}
	}
}
