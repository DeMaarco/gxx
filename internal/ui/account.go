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
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"gxx/internal/auth"
)

// ReadLoginChoice shows a selectable login menu. The active account is green.
func ReadLoginChoice(in *os.File, out io.Writer, color bool, active string) (string, error) {
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return "", fmt.Errorf("choose openai, claude, or api")
	}
	options := auth.Options(active)
	if len(options) == 0 {
		return "", fmt.Errorf("no login options")
	}
	index := 0
	for i, option := range options {
		if option.ID == active {
			index = i
			break
		}
	}
	fd := int(in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	rows := 0
	redraw := func() error {
		if rows > 0 {
			_, _ = io.WriteString(out, fmt.Sprintf("\x1b[%dA", rows))
		}
		var body strings.Builder
		body.WriteString(paint(color, dim, "Choose an account") + "\r\n")
		for i, option := range options {
			activeRow := option.ID == active
			selected := i == index
			marker := "  "
			label := option.Label
			help := paint(color, dim, "  "+option.Help)
			switch {
			case activeRow:
				marker = paint(color, green, "● ")
				label = paint(color, bold+green, option.Label)
			case selected:
				marker = paint(color, cyan, "▸ ")
				label = paint(color, bold+cyan, option.Label)
			}
			body.WriteString(marker + label + help + "\r\n")
		}
		body.WriteString(paint(color, dim, "enter apply · esc cancel") + "\r\n")
		text := body.String()
		for _, line := range strings.Split(strings.TrimRight(text, "\r\n"), "\r\n") {
			_, _ = io.WriteString(out, clearLine+line+"\r\n")
		}
		rows = strings.Count(text, "\r\n")
		if !strings.HasSuffix(text, "\r\n") {
			rows++
		}
		return nil
	}
	if err := redraw(); err != nil {
		return "", err
	}
	for {
		event, err := readKey(in)
		if err != nil {
			return "", err
		}
		switch event.kind {
		case keyUp:
			if index > 0 {
				index--
			}
		case keyDown:
			if index < len(options)-1 {
				index++
			}
		case keyEnter:
			_, _ = io.WriteString(out, "\r\n")
			return options[index].ID, nil
		case keyEsc, keyCtrlC:
			_, _ = io.WriteString(out, "\r\n")
			return "", auth.ErrCanceled
		default:
			continue
		}
		if err := redraw(); err != nil {
			return "", err
		}
	}
}
