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

	"gxx/internal/ui"
)

func TestPlanMenuDefaultsToExecute(t *testing.T) {
	menu := ui.NewPlanMenu()
	if got := menu.Labels(); len(got) != 3 ||
		got[0] != "Execute plan" ||
		got[1] != "Request changes" ||
		got[2] != "Cancel" {
		t.Fatalf("labels = %v, want Execute plan, Request changes, Cancel", got)
	}
	done, choice := menu.Apply(ui.KeyEnter)
	if !done || choice != ui.PlanExecute {
		t.Fatalf("enter on default = done=%v choice=%v, want execute", done, choice)
	}
}

func TestPlanMenuArrowsSelectReviseAndCancel(t *testing.T) {
	menu := ui.NewPlanMenu()
	menu.Apply(ui.KeyDown)
	done, choice := menu.Apply(ui.KeyEnter)
	if !done || choice != ui.PlanRevise {
		t.Fatalf("revise = done=%v choice=%v", done, choice)
	}

	menu = ui.NewPlanMenu()
	menu.Apply(ui.KeyDown)
	menu.Apply(ui.KeyDown)
	menu.Apply(ui.KeyDown)
	if menu.Index() != 2 {
		t.Fatalf("index = %d, want last option after extra down", menu.Index())
	}
	done, choice = menu.Apply(ui.KeyEnter)
	if !done || choice != ui.PlanCancel {
		t.Fatalf("cancel = done=%v choice=%v", done, choice)
	}
}

func TestPlanMenuEscCancels(t *testing.T) {
	menu := ui.NewPlanMenu()
	done, choice := menu.Apply(ui.KeyEsc)
	if !done || choice != ui.PlanCancel {
		t.Fatalf("esc = done=%v choice=%v, want cancel", done, choice)
	}
}

func TestReadPlanChoiceRequiresTerminal(t *testing.T) {
	_, err := ui.ReadPlanChoice(context.Background(), nil, io.Discard, false)
	if err == nil {
		t.Fatal("expected terminal error")
	}
}
