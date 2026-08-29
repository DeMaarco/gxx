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

	"gxx/internal/approval"
	"gxx/internal/ui"
)

func TestApprovalMenuDefaultsToDeny(t *testing.T) {
	menu := ui.NewApprovalMenu(true)
	if got := menu.Labels(); len(got) != 3 || got[0] != "Deny" || got[1] != "Approve" || got[2] != "Allow for session" {
		t.Fatalf("labels = %v, want Deny, Approve, Allow for session", got)
	}
	done, decision := menu.Apply(ui.KeyEnter)
	if !done || decision.Approved || decision.Remember {
		t.Fatalf("enter on default = done=%v decision=%+v, want deny", done, decision)
	}
}

func TestApprovalMenuArrowsSelectApproveAndSession(t *testing.T) {
	menu := ui.NewApprovalMenu(true)
	menu.Apply(ui.KeyDown)
	if menu.Index() != 1 {
		t.Fatalf("index = %d, want 1 after down", menu.Index())
	}
	done, decision := menu.Apply(ui.KeyEnter)
	if !done || !decision.Approved || decision.Remember {
		t.Fatalf("approve = done=%v decision=%+v", done, decision)
	}

	menu = ui.NewApprovalMenu(true)
	menu.Apply(ui.KeyDown)
	menu.Apply(ui.KeyDown)
	menu.Apply(ui.KeyDown)
	if menu.Index() != 2 {
		t.Fatalf("index = %d, want last option after extra down", menu.Index())
	}
	done, decision = menu.Apply(ui.KeyEnter)
	if !done || !decision.Approved || !decision.Remember {
		t.Fatalf("session allow = done=%v decision=%+v", done, decision)
	}
}

func TestApprovalMenuWriteOmitsSessionAllow(t *testing.T) {
	menu := ui.NewApprovalMenu(false)
	if got := menu.Labels(); len(got) != 2 || got[1] != "Approve" {
		t.Fatalf("labels = %v, want Deny and Approve", got)
	}
	menu.Apply(ui.KeyDown)
	menu.Apply(ui.KeyDown)
	if menu.Index() != 1 {
		t.Fatalf("index = %d, want Approve after extra down", menu.Index())
	}
}

func TestApprovalMenuEscAndCtrlCDeny(t *testing.T) {
	for _, kind := range []ui.KeyKind{ui.KeyEsc, ui.KeyCtrlC} {
		menu := ui.NewApprovalMenu(true)
		menu.Apply(ui.KeyDown)
		done, decision := menu.Apply(kind)
		if !done || decision.Approved {
			t.Fatalf("%v = done=%v decision=%+v, want deny", kind, done, decision)
		}
	}
}

func TestReadApprovalChoiceRequiresTerminal(t *testing.T) {
	_, err := ui.ReadApprovalChoice(context.Background(), nil, io.Discard, false, approval.Action{})
	if err == nil {
		t.Fatal("expected terminal error")
	}
}
