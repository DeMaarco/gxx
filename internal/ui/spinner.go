package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"gxx/internal/agent"
)

const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type runningTool struct {
	id    string
	name  string
	hint  string
	start time.Time
}

func liveOutput(writer io.Writer) bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func (r *Renderer) timestamp() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *Renderer) shouldSpinLocked() bool {
	return r.live && !r.textOpen && (r.thinking || len(r.running) > 0)
}

func (r *Renderer) startAnim() {
	if !r.live || r.spinEvery <= 0 {
		return
	}
	r.animMu.Lock()
	defer r.animMu.Unlock()
	if r.animStop != nil {
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	r.animStop = stop
	r.animDone = done
	r.mu.Lock()
	r.hideCursorLocked()
	r.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(r.spinEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.mu.Lock()
				if r.shouldSpinLocked() {
					r.frame++
					r.drawLiveLocked()
				}
				r.mu.Unlock()
			}
		}
	}()
}

func (r *Renderer) stopAnim() {
	r.animMu.Lock()
	stop := r.animStop
	done := r.animDone
	r.animStop = nil
	r.animDone = nil
	r.animMu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

func (r *Renderer) drawLiveLocked() {
	if !r.live {
		return
	}
	_, _ = io.WriteString(r.writer, liveLine(
		r.color,
		r.frame,
		r.liveLabelLocked(),
		r.liveElapsedLocked(),
	))
	r.liveOpen = true
}

func (r *Renderer) clearLiveLocked() {
	if !r.liveOpen {
		return
	}
	_, _ = io.WriteString(r.writer, clearLine)
	r.liveOpen = false
}

func (r *Renderer) hideCursorLocked() {
	if r.cursorHide {
		return
	}
	_, _ = io.WriteString(r.writer, hideCursor)
	r.cursorHide = true
}

func (r *Renderer) showCursorLocked() {
	if !r.cursorHide {
		return
	}
	_, _ = io.WriteString(r.writer, showCursor)
	r.cursorHide = false
}

func (r *Renderer) liveLabelLocked() string {
	if len(r.running) == 0 {
		return paint(r.color, cyan, "thinking")
	}
	parts := make([]string, 0, len(r.running))
	for _, tool := range r.running {
		parts = append(parts, runningToolLabel(r.color, tool))
	}
	return strings.Join(parts, paint(r.color, dim, " · "))
}

func (r *Renderer) liveElapsedLocked() time.Duration {
	start := r.thinkStart
	if len(r.running) > 0 {
		start = r.running[0].start
		for _, tool := range r.running[1:] {
			if tool.start.Before(start) {
				start = tool.start
			}
		}
	}
	elapsed := r.timestamp().Sub(start)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func (r *Renderer) removeRunning(result *agent.ToolResult) {
	if result == nil || len(r.running) == 0 {
		return
	}
	for index, tool := range r.running {
		if result.CallID != "" && tool.id == result.CallID {
			r.running = append(r.running[:index], r.running[index+1:]...)
			return
		}
	}
	if result.CallID != "" {
		return
	}
	for index, tool := range r.running {
		if tool.name == result.Name {
			r.running = append(r.running[:index], r.running[index+1:]...)
			return
		}
	}
}

func newRunningTool(call *agent.ToolCall, started time.Time) runningTool {
	return runningTool{
		id:    call.ID,
		name:  strings.TrimSpace(call.Name),
		hint:  toolHint(call.Arguments),
		start: started,
	}
}

func runningToolLabel(color bool, tool runningTool) string {
	name := paint(color, cyan, safeTerminalText(tool.name))
	if tool.hint == "" {
		return name
	}
	return name + "  " + paint(color, dim, tool.hint)
}

func liveLine(color bool, frame int, label string, elapsed time.Duration) string {
	if frame < 0 {
		frame = 0
	}
	glyph := spinnerFrames[frame%len(spinnerFrames)]
	return clearLine +
		paint(color, cyan, glyph) +
		" " +
		label +
		"  " +
		paint(color, dim, formatElapsed(elapsed))
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed < time.Minute {
		return fmt.Sprintf("%.1fs", elapsed.Seconds())
	}
	minutes := int(elapsed.Minutes())
	seconds := int(elapsed.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func toolHint(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return ""
	}
	for _, key := range []string{"path", "command", "query"} {
		value, _ := arguments[key].(string)
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			continue
		}
		if len(value) > 48 {
			value = value[:48] + "…"
		}
		return safeTerminalText(value)
	}
	return ""
}
