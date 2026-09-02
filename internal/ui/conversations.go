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
	"time"

	"golang.org/x/term"

	"gxx/internal/osutil"
)

// ConversationEntry is a saved conversation shown in the history menu.
type ConversationEntry struct {
	ID        string
	Title     string
	Model     string
	UpdatedAt time.Time
}

type conversationMenu struct {
	entries []ConversationEntry
	index   int
}

func newConversationMenu(entries []ConversationEntry) conversationMenu {
	return conversationMenu{entries: entries}
}

func (m *conversationMenu) apply(event keyEvent) (done bool, id string) {
	if len(m.entries) == 0 {
		switch event.kind {
		case keyEnter, keyEsc, keyCtrlC:
			return true, ""
		}
		return false, ""
	}
	switch event.kind {
	case keyUp:
		if m.index > 0 {
			m.index--
		}
	case keyDown:
		if m.index < len(m.entries)-1 {
			m.index++
		}
	case keyEnter:
		return true, m.entries[m.index].ID
	case keyEsc, keyCtrlC:
		return true, ""
	}
	return false, ""
}

// ReadConversationChoice shows the saved-conversation menu. Esc cancels.
func ReadConversationChoice(
	ctx context.Context,
	in *os.File,
	out io.Writer,
	entries []ConversationEntry,
	color bool,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return "", fmt.Errorf("conversation menu requires a terminal")
	}
	fd := int(in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	osutil.DrainReadyInput(in)
	menu := newConversationMenu(entries)
	stop := context.AfterFunc(ctx, func() {
		osutil.InterruptRead(in)
	})
	defer stop()
	defer osutil.ClearReadDeadline(in)

	rows := 0
	now := time.Now()
	redraw := func() {
		if rows > 0 {
			_, _ = io.WriteString(out, fmt.Sprintf("\x1b[%dA", rows))
		}
		var body strings.Builder
		body.WriteString(paint(color, dim, "Conversations") + paint(color, dim, "  Ctrl+O") + "\r\n")
		if len(menu.entries) == 0 {
			body.WriteString(paint(color, dim, "No saved conversations for this workspace.") + "\r\n")
		} else {
			for i, entry := range menu.entries {
				marker := "  "
				label := entry.Title
				meta := paint(color, dim, "  "+formatConversationMeta(entry, now))
				if i == menu.index {
					marker = paint(color, cyan, "▸ ")
					label = paint(color, bold+cyan, entry.Title)
				}
				body.WriteString(marker + label + meta + "\r\n")
			}
		}
		body.WriteString(paint(color, dim, "↑↓ select · enter load · esc cancel") + "\r\n")
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
				return "", ctx.Err()
			}
			return "", err
		}
		done, id := menu.apply(event)
		if done {
			_, _ = io.WriteString(out, "\r\n")
			return id, nil
		}
		redraw()
	}
}

func formatConversationMeta(entry ConversationEntry, now time.Time) string {
	model := strings.TrimSpace(entry.Model)
	when := formatRelativeTime(entry.UpdatedAt, now)
	if model == "" {
		return when
	}
	return when + " · " + model
}

func formatRelativeTime(when, now time.Time) string {
	when = when.UTC()
	now = now.UTC()
	if when.After(now) {
		when = now
	}
	delta := now.Sub(when)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		minutes := int(delta / time.Minute)
		if minutes == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", minutes)
	case delta < 24*time.Hour:
		hours := int(delta / time.Hour)
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case delta < 48*time.Hour:
		return "yesterday"
	default:
		return when.Format("Jan 2")
	}
}
