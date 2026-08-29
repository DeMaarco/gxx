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

	"gxx/internal/approval"
	"gxx/internal/osutil"
)

type approvalChoice struct {
	Label    string
	Help     string
	Decision approval.Decision
}

type approvalMenu struct {
	choices []approvalChoice
	index   int
}

func newApprovalMenu(allowSession bool) approvalMenu {
	choices := []approvalChoice{
		{Label: "Deny", Help: "skip this action"},
		{Label: "Approve", Help: "run this once", Decision: approval.Decision{Approved: true}},
	}
	if allowSession {
		choices = append(choices, approvalChoice{
			Label:    "Allow for session",
			Help:     "remember this exact command",
			Decision: approval.Decision{Approved: true, Remember: true},
		})
	}
	return approvalMenu{choices: choices}
}

func (m *approvalMenu) apply(event keyEvent) (done bool, decision approval.Decision) {
	switch event.kind {
	case keyUp:
		if m.index > 0 {
			m.index--
		}
	case keyDown:
		if m.index < len(m.choices)-1 {
			m.index++
		}
	case keyEnter:
		return true, m.choices[m.index].Decision
	case keyEsc, keyCtrlC:
		return true, approval.Decision{}
	}
	return false, approval.Decision{}
}

// ReadApprovalChoice shows a selectable deny / approve menu. Deny is the default.
func ReadApprovalChoice(ctx context.Context, in *os.File, out io.Writer, color bool, action approval.Action) (approval.Decision, error) {
	if err := ctx.Err(); err != nil {
		return approval.Decision{}, err
	}
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return approval.Decision{}, fmt.Errorf("approval menu requires a terminal")
	}
	fd := int(in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return approval.Decision{}, err
	}
	defer term.Restore(fd, state)

	menu := newApprovalMenu(action.RepeatKey != "")
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
		body.WriteString(paint(color, dim, "Choose a response") + "\r\n")
		for i, choice := range menu.choices {
			marker := "  "
			label := choice.Label
			help := paint(color, dim, "  "+choice.Help)
			if i == menu.index {
				marker = paint(color, cyan, "▸ ")
				label = paint(color, bold+cyan, choice.Label)
			}
			body.WriteString(marker + label + help + "\r\n")
		}
		body.WriteString(paint(color, dim, "↑↓ select · enter confirm · esc deny") + "\r\n")
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
				return approval.Decision{}, ctx.Err()
			}
			return approval.Decision{}, err
		}
		done, decision := menu.apply(event)
		if done {
			_, _ = io.WriteString(out, "\r\n")
			return decision, nil
		}
		redraw()
	}
}
