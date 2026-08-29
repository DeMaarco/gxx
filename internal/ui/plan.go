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

package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"gxx/internal/osutil"
)

const implementPlanPrompt = "Implement the approved plan."

type PlanChoice int

const (
	PlanExecute PlanChoice = iota
	PlanRevise
	PlanCancel
)

type planOption struct {
	Label  string
	Help   string
	Choice PlanChoice
}

type planMenu struct {
	options []planOption
	index   int
}

func newPlanMenu() planMenu {
	return planMenu{
		options: []planOption{
			{Label: "Execute plan", Help: "leave plan mode and implement", Choice: PlanExecute},
			{Label: "Request changes", Help: "stay in plan mode and describe edits", Choice: PlanRevise},
			{Label: "Cancel", Help: "keep the plan and return to the prompt", Choice: PlanCancel},
		},
	}
}

func (m *planMenu) apply(event keyEvent) (done bool, choice PlanChoice) {
	switch event.kind {
	case keyUp:
		if m.index > 0 {
			m.index--
		}
	case keyDown:
		if m.index < len(m.options)-1 {
			m.index++
		}
	case keyEnter:
		return true, m.options[m.index].Choice
	case keyEsc, keyCtrlC:
		return true, PlanCancel
	}
	return false, PlanCancel
}

// ReadPlanChoice shows the post-plan menu. Esc cancels.
func ReadPlanChoice(ctx context.Context, in *os.File, out io.Writer, color bool) (PlanChoice, error) {
	if err := ctx.Err(); err != nil {
		return PlanCancel, err
	}
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return PlanCancel, fmt.Errorf("plan menu requires a terminal")
	}
	fd := int(in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return PlanCancel, err
	}
	defer term.Restore(fd, state)

	osutil.DrainReadyInput(in)
	menu := newPlanMenu()
	stop := context.AfterFunc(ctx, func() {
		osutil.InterruptRead(in)
	})
	defer stop()
	defer osutil.ClearReadDeadline(in)

	rows := 0
	redraw := func() {
		if rows > 0 {
			_, _ = io.WriteString(out, fmt.Sprintf("\x1b[%dA", rows))
		}
		var body strings.Builder
		body.WriteString(paint(color, dim, "What next?") + "\r\n")
		for i, option := range menu.options {
			marker := "  "
			label := option.Label
			help := paint(color, dim, "  "+option.Help)
			if i == menu.index {
				marker = paint(color, cyan, "▸ ")
				label = paint(color, bold+cyan, option.Label)
			}
			body.WriteString(marker + label + help + "\r\n")
		}
		body.WriteString(paint(color, dim, "↑↓ select · enter confirm · esc cancel") + "\r\n")
		text := body.String()
		for _, line := range strings.Split(strings.TrimRight(text, "\r\n"), "\r\n") {
			_, _ = io.WriteString(out, clearLine+line+"\r\n")
		}
		rows = strings.Count(text, "\r\n")
		if !strings.HasSuffix(text, "\r\n") {
			rows++
		}
	}
	redraw()
	for {
		event, err := readKey(in)
		if err != nil {
			if ctx.Err() != nil {
				return PlanCancel, ctx.Err()
			}
			return PlanCancel, err
		}
		done, choice := menu.apply(event)
		if done {
			_, _ = io.WriteString(out, "\r\n")
			return choice, nil
		}
		redraw()
	}
}
