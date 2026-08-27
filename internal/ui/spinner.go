package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
		r.usage,
		r.termWidth(),
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
	return compactRunningLabel(r.color, r.running)
}

func (r *Renderer) termWidth() int {
	if r.columns > 0 {
		return r.columns
	}
	file, ok := r.writer.(*os.File)
	if !ok {
		return 80
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width < 20 {
		return 80
	}
	return width
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

func (r *Renderer) takeRunning(result *agent.ToolResult) runningTool {
	if result == nil || len(r.running) == 0 {
		if result == nil {
			return runningTool{}
		}
		return runningTool{name: result.Name}
	}
	for index, tool := range r.running {
		if result.CallID != "" && tool.id == result.CallID {
			r.running = append(r.running[:index], r.running[index+1:]...)
			return tool
		}
	}
	if result.CallID != "" {
		return runningTool{name: result.Name}
	}
	for index, tool := range r.running {
		if tool.name == result.Name {
			r.running = append(r.running[:index], r.running[index+1:]...)
			return tool
		}
	}
	return runningTool{name: result.Name}
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

func compactRunningLabel(color bool, tools []runningTool) string {
	if len(tools) == 0 {
		return paint(color, cyan, "thinking")
	}
	type group struct {
		name  string
		count int
		hint  string
	}
	groups := make([]group, 0, len(tools))
	indexOf := make(map[string]int, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.name)
		if name == "" {
			name = "tool"
		}
		if index, ok := indexOf[name]; ok {
			groups[index].count++
			continue
		}
		indexOf[name] = len(groups)
		groups = append(groups, group{name: name, count: 1, hint: tool.hint})
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		label := paint(color, cyan, safeTerminalText(group.name))
		if group.count > 1 {
			label += paint(color, dim, fmt.Sprintf(" ×%d", group.count))
		}
		if group.hint != "" && len(groups) == 1 {
			label += "  " + paint(color, dim, group.hint)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, paint(color, dim, " · "))
}

func liveLine(color bool, frame int, label string, elapsed time.Duration, usage agent.Usage, width int) string {
	if frame < 0 {
		frame = 0
	}
	if width < 20 {
		width = 20
	}
	// Leave the last column empty. Writing into it wraps on most terminals,
	// and \r then only clears the leftover row.
	width--
	glyph := spinnerFrames[frame%len(spinnerFrames)]
	prefix := paint(color, cyan, glyph) + " "
	suffix := liveMeta(color, elapsed, usage)
	budget := width - visibleWidth(prefix) - visibleWidth(suffix)
	if budget < 4 {
		budget = 4
	}
	return clearLine + prefix + truncateVisible(label, budget) + suffix
}

func liveMeta(color bool, elapsed time.Duration, usage agent.Usage) string {
	parts := []string{formatElapsed(elapsed)}
	if tokens := usageTokens(usage); tokens > 0 {
		parts = append(parts, formatCompactTokens(tokens)+" tok")
	}
	return "  " + paint(color, dim, strings.Join(parts, " · "))
}

func usageTokens(usage agent.Usage) int64 {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.InputTokens + usage.OutputTokens
}

func formatTurnUsage(color bool, usage agent.Usage) string {
	total := usageTokens(usage)
	if total <= 0 {
		return ""
	}
	parts := []string{formatCompactTokens(total) + " tok"}
	if usage.InputTokens > 0 {
		parts = append(parts, formatCompactTokens(usage.InputTokens)+" in")
	}
	if usage.CachedTokens > 0 {
		parts = append(parts, formatCompactTokens(usage.CachedTokens)+" cached")
	}
	if usage.OutputTokens > 0 {
		parts = append(parts, formatCompactTokens(usage.OutputTokens)+" out")
	}
	if usage.ReasoningTokens > 0 {
		parts = append(parts, formatCompactTokens(usage.ReasoningTokens)+" reason")
	}
	return paint(color, dim, strings.Join(parts, " · "))
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

func formatToolDuration(ms int64) string {
	if ms < 1 {
		return "<1ms"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func visibleWidth(value string) int {
	width := 0
	inEscape := false
	for i := 0; i < len(value); {
		if value[i] == '\x1b' {
			inEscape = true
			i++
			continue
		}
		if inEscape {
			if (value[i] >= 'A' && value[i] <= 'Z') || (value[i] >= 'a' && value[i] <= 'z') {
				inEscape = false
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(value[i:])
		i += size
		width++
	}
	return width
}

func truncateVisible(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if visibleWidth(value) <= max {
		return value
	}
	if max == 1 {
		return "…"
	}
	var output strings.Builder
	width := 0
	inEscape := false
	for i := 0; i < len(value); {
		if value[i] == '\x1b' {
			inEscape = true
			output.WriteByte(value[i])
			i++
			continue
		}
		if inEscape {
			output.WriteByte(value[i])
			if (value[i] >= 'A' && value[i] <= 'Z') || (value[i] >= 'a' && value[i] <= 'z') {
				inEscape = false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if width+1 > max-1 {
			output.WriteRune('…')
			output.WriteString(reset)
			break
		}
		output.WriteRune(r)
		i += size
		width++
	}
	return output.String()
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
	if rawChanges, ok := arguments["changes"].([]any); ok && len(rawChanges) > 0 {
		if first, ok := rawChanges[0].(map[string]any); ok {
			value, _ := first["path"].(string)
			value = strings.Join(strings.Fields(value), " ")
			if value != "" {
				if len(value) > 48 {
					value = value[:48] + "…"
				}
				return safeTerminalText(value)
			}
		}
	}
	return ""
}

type pendingDoneGroup struct {
	name      string
	count     int
	hints     []string
	seenHint  map[string]struct{}
	duration  int64
	truncated bool
}

type doneLine struct {
	name      string
	hints     string
	count     int
	duration  int64
	truncated bool
	failed    bool
	detail    string
}

func (g *pendingDoneGroup) add(name, hint string, duration int64, truncated bool) {
	if g.count == 0 {
		g.name = name
		g.seenHint = map[string]struct{}{}
	}
	g.count++
	if duration > g.duration {
		g.duration = duration
	}
	g.truncated = g.truncated || truncated
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return
	}
	if _, ok := g.seenHint[hint]; ok {
		return
	}
	g.seenHint[hint] = struct{}{}
	g.hints = append(g.hints, hint)
}

func compactHints(hints []string) string {
	const maxHints = 3
	if len(hints) == 0 {
		return ""
	}
	if len(hints) <= maxHints {
		return strings.Join(hints, ", ")
	}
	return strings.Join(hints[:maxHints], ", ") + ", …"
}

func holdModelText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "{") {
		return false
	}
	rest := value
	for rest != "" {
		if !strings.HasPrefix(rest, "{") {
			return false
		}
		decoder := json.NewDecoder(strings.NewReader(rest))
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return true
		}
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil || !toolishObject(object) {
			return false
		}
		rest = strings.TrimSpace(rest[decoder.InputOffset():])
	}
	return true
}

func toolishObject(object map[string]any) bool {
	for _, key := range []string{
		"path", "command", "query", "offset_line", "limit_lines", "timeout_seconds",
	} {
		if _, ok := object[key]; ok {
			return true
		}
	}
	return false
}
