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
	"strings"
	"testing"

	"gxx/internal/ui"
)

func TestLookupSlashCommand(t *testing.T) {
	tests := []struct {
		line    string
		name    string
		wantErr string
	}{
		{line: "/help", name: "/help"},
		{line: "/login", name: "/login"},
		{line: "/login openai", name: "/login"},
		{line: "/logout claude", name: "/logout"},
		{line: "/quit", name: "/exit"},
		{line: "/model terra", name: "/model"},
		{line: "/mode auto", name: "/mode"},
		{line: "/eco 2", name: "/eco"},
		{line: "/skills", name: "/skills"},
		{line: "/skill frontend-design build it", wantErr: "unknown command /skill"},
		{line: "/foo", wantErr: "unknown command /foo"},
		{line: "/help extra", wantErr: "unexpected argument for /help"},
		{line: "/clear now", wantErr: "unexpected argument for /clear"},
		{line: "/skills list", wantErr: "unexpected argument for /skills"},
		{line: "/modelxyz", wantErr: "unknown command /modelxyz"},
	}
	for _, test := range tests {
		name, _, err := ui.LookupSlashCommand(test.line)
		if test.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("lookupSlashCommand(%q) error = %v, want %q", test.line, err, test.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("lookupSlashCommand(%q) error = %v", test.line, err)
		}
		if name != test.name {
			t.Fatalf("lookupSlashCommand(%q) name = %q, want %q", test.line, name, test.name)
		}
	}
}

func TestMatchSkillName(t *testing.T) {
	skills := []string{"frontend-design", "code-review"}
	if got, ok := ui.MatchSkillName("frontend-design", skills); !ok || got != "frontend-design" {
		t.Fatalf("exact = %q %v", got, ok)
	}
	if got, ok := ui.MatchSkillName("fronted-design", skills); !ok || got != "frontend-design" {
		t.Fatalf("typo = %q %v, want frontend-design", got, ok)
	}
	if _, ok := ui.MatchSkillName("unrelated", skills); ok {
		t.Fatal("unrelated should not match")
	}
}

func TestUnknownSlashHintForSkill(t *testing.T) {
	err := errorString("unknown command /frontend-design")
	got := ui.UnknownSlashHint(err, "/frontend-design", []string{"frontend-design"})
	if !strings.Contains(got, "usage: /frontend-design <request>") {
		t.Fatalf("hint = %q", got)
	}
	typo := ui.UnknownSlashHint(errorString("unknown command /fronted-design"), "/fronted-design", []string{"frontend-design"})
	if !strings.Contains(typo, "Did you mean /frontend-design <request>?") {
		t.Fatalf("typo hint = %q", typo)
	}
}

func TestRewriteSkillPrompt(t *testing.T) {
	got, err := ui.RewriteSkillPrompt("/fronted-design diseña una página", []string{"frontend-design"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `Call read_skill for "frontend-design" before any other tool`) {
		t.Fatalf("rewritten = %q, want skill-first instruction", got)
	}
	if !strings.Contains(got, "diseña una página") {
		t.Fatalf("rewritten = %q, want request", got)
	}
	if _, err := ui.RewriteSkillPrompt("/frontend-design", []string{"frontend-design"}); err == nil {
		t.Fatal("expected usage error without request")
	}
	both, err := ui.RewriteSkillPrompt("/frontend-design y /agent-browser diseña una página", []string{"frontend-design", "agent-browser"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(both, `"frontend-design"`) || !strings.Contains(both, `"agent-browser"`) {
		t.Fatalf("rewritten = %q, want both skills", both)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
