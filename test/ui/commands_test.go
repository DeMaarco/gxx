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
