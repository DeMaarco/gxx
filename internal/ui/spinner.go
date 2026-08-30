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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
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
	id      string
	name    string
	hint    string
	start   time.Time
	detail  string
	added   int
	deleted int
	removed bool
	lines   []activityLine
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
	r.paintLiveLinesLocked(r.composeLiveLocked())
}

func (r *Renderer) composeLiveLocked() []string {
	width := r.termWidth()
	status := formatLiveStatus(
		r.color,
		r.frame,
		r.liveGlyphColorLocked(),
		r.liveLabelLocked(),
		r.liveElapsedLocked(),
		r.usage,
		width,
	)
	lines := []string{status}
	for _, detail := range r.liveDetailLinesLocked() {
		lines = append(lines, detail)
		if len(lines) >= maxLiveDetails+1 {
			break
		}
	}
	return lines
}

func (r *Renderer) liveDetailLinesLocked() []string {
	var lines []string
	for _, tool := range r.running {
		if tool.detail != "" {
			lines = append(lines, "  "+paint(r.color, dim, tool.detail))
		}
	}
	return lines
}

func (r *Renderer) liveGlyphColorLocked() string {
	if len(r.running) == 1 {
		return verbColor(r.running[0].name)
	}
	return cyan
}

func (r *Renderer) paintLiveLinesLocked(lines []string) {
	if len(lines) == 0 {
		r.clearLiveLocked()
		return
	}
	width := r.termWidth()
	if width < 20 {
		width = 20
	}
	width--
	if r.liveHeight > 0 {
		_, _ = io.WriteString(r.writer, "\r")
		if r.liveHeight > 1 {
			_, _ = io.WriteString(r.writer, cursorUpN(r.liveHeight-1))
		}
	}
	for i, line := range lines {
		_, _ = io.WriteString(r.writer, clearLine)
		_, _ = io.WriteString(r.writer, truncateVisible(line, width))
		if i < len(lines)-1 {
			_, _ = io.WriteString(r.writer, "\n")
		}
	}
	_, _ = io.WriteString(r.writer, eraseDown)
	r.liveHeight = len(lines)
	r.liveOpen = true
}

func (r *Renderer) clearLiveLocked() {
	if !r.liveOpen && r.liveHeight == 0 {
		return
	}
	height := r.liveHeight
	if height < 1 {
		height = 1
	}
	_, _ = io.WriteString(r.writer, "\r")
	if height > 1 {
		_, _ = io.WriteString(r.writer, cursorUpN(height-1))
	}
	_, _ = io.WriteString(r.writer, clearLine)
	_, _ = io.WriteString(r.writer, eraseDown)
	r.liveHeight = 0
	r.liveOpen = false
}

func cursorUpN(n int) string {
	if n <= 1 {
		return cursorUp
	}
	return fmt.Sprintf("\x1b[%dA", n)
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
	label := compactRunningLabel(r.color, r.running)
	if len(r.running) != 1 {
		return label
	}
	tool := r.running[0]
	stats := formatLineCounts(r.color, "", tool.added, tool.deleted, tool.removed)
	if stats == "" {
		return label
	}
	return label + "  " + stats
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
	name := strings.TrimSpace(call.Name)
	tool := runningTool{
		id:    call.ID,
		name:  name,
		hint:  toolHint(call.Arguments),
		start: started,
	}
	if name == "apply_patch" {
		tool.added, tool.deleted, tool.removed, tool.lines = patchActivity(call.Arguments)
	}
	return tool
}

func runningToolLabel(color bool, tool runningTool) string {
	name := paint(color, verbColor(tool.name), safeTerminalText(toolVerb(tool.name)))
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
		label := paint(color, verbColor(group.name), safeTerminalText(toolVerb(group.name)))
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
	return clearLine + formatLiveStatus(color, frame, cyan, label, elapsed, usage, width)
}

func formatLiveStatus(color bool, frame int, glyphColor, label string, elapsed time.Duration, usage agent.Usage, width int) string {
	if frame < 0 {
		frame = 0
	}
	if width < 20 {
		width = 20
	}
	// Leave the last column empty. Writing into it wraps on most terminals,
	// and \r then only clears the leftover row.
	width--
	if glyphColor == "" {
		glyphColor = cyan
	}
	glyph := spinnerFrames[frame%len(spinnerFrames)]
	prefix := paint(color, glyphColor, glyph) + " "
	suffix := liveMeta(color, elapsed, usage)
	budget := width - visibleWidth(prefix) - visibleWidth(suffix)
	if budget < 4 {
		budget = 4
	}
	return prefix + truncateVisible(label, budget) + suffix
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
		return safeTerminalText(truncateRunes(value, maxHintRunes))
	}
	if rawChanges, ok := arguments["changes"].([]any); ok && len(rawChanges) > 0 {
		seen := map[string]struct{}{}
		var paths []string
		for _, raw := range rawChanges {
			change, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			value, _ := change["path"].(string)
			value = strings.Join(strings.Fields(value), " ")
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			paths = append(paths, value)
		}
		if len(paths) == 0 {
			return ""
		}
		value := paths[0]
		if len(paths) > 1 {
			value = fmt.Sprintf("%s · %d files", value, len(paths))
		}
		return safeTerminalText(truncateRunes(value, maxHintRunes))
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
	stats     string
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
	emit, hold := splitHeld(value)
	if hold != "" {
		return true
	}
	return strings.TrimSpace(value) != "" && strings.TrimSpace(emit) == ""
}

func splitHeld(value string) (emit, hold string) {
	var output strings.Builder
	rest := value
	for rest != "" {
		if i := incompleteLeakAt(rest); i >= 0 && leakStart(rest[i:]) < 0 {
			output.WriteString(rest[:i])
			return output.String(), rest[i:]
		}
		cut := leakStart(rest)
		if j := jsonToolIndex(rest); j >= 0 && (cut < 0 || j < cut) {
			cut = j
		}
		if cut < 0 {
			output.WriteString(rest)
			return output.String(), ""
		}
		output.WriteString(rest[:cut])
		next, done := skipLeakBlock(rest[cut:])
		if !done {
			return strings.TrimRight(output.String(), " \t"), rest[cut:]
		}
		if output.Len() > 0 && looksLikeProse(next) && !strings.HasSuffix(output.String(), "\n") {
			output.WriteByte('\n')
		}
		rest = next
	}
	return output.String(), ""
}

var leakToolNames = []string{
	"apply_patch", "generate_image", "read_file", "list_files", "search_files", "run_command",
	"functions", "multi_tool_use", "parallel",
}

var leakMarkers = []string{
	"to=functions", "to=multi_tool_use", "to=apply_patch", "to=generate_image", "to=read_file",
	"to=list_files", "to=search_files", "to=run_command", "to=parallel",
	"<|recipient|>", "<|content|>", "<|constrain|>",
	"<function=", "</function>",
	"functions.apply_patch", "functions.generate_image", "functions.read_file", "functions.list_files",
	"functions.search_files", "functions.run_command",
	"recipient=functions", "recipient=apply_patch", "recipient=generate_image", "recipient=read_file",
	"```tool", "```json",
}

func leakStart(value string) int {
	idx := -1
	for _, marker := range leakMarkers {
		if i := strings.Index(value, marker); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	return idx
}

func incompleteLeakAt(value string) int {
	if i := strings.LastIndex(value, "to="); i >= 0 {
		if _, incomplete := parseToHeader(value[i:]); incomplete {
			return i
		}
	}
	if i := strings.LastIndex(value, "functions."); i >= 0 {
		if _, incomplete := parseFunctionsHeader(value[i:]); incomplete {
			return i
		}
	}
	if i := strings.LastIndex(value, "<|"); i >= 0 {
		suffix := value[i+2:]
		if !strings.Contains(suffix, "|>") && !strings.Contains(suffix, "\n") {
			for _, name := range []string{"recipient", "content", "constrain"} {
				if suffix == "" || strings.HasPrefix(name, suffix) || strings.HasPrefix(suffix, name) {
					return i
				}
			}
		}
	}
	if i := strings.LastIndex(value, "<function"); i >= 0 {
		if !strings.Contains(value[i:], ">") && !strings.Contains(value[i:], "\n") {
			return i
		}
	}
	if i := strings.LastIndex(value, "```"); i >= 0 {
		lang := value[i+3:]
		if lang != "tool" && lang != "json" && !strings.ContainsAny(lang, " \t\n") {
			if strings.HasPrefix("tool", lang) || strings.HasPrefix("json", lang) {
				return i
			}
		}
	}
	return -1
}

func jsonToolIndex(value string) int {
	i := 0
	for i < len(value) && (value[i] == ' ' || value[i] == '\t' || value[i] == '\n' || value[i] == '\r') {
		i++
	}
	if i >= len(value) || !strings.HasPrefix(value[i:], `{"`) {
		return -1
	}
	decoder := json.NewDecoder(strings.NewReader(value[i:]))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return i
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || !toolishObject(object) {
		return -1
	}
	return i
}

func skipLeakBlock(value string) (string, bool) {
	rest := value
	progressed := false
	for {
		trimmed := strings.TrimLeft(rest, " \t\n\r")
		if trimmed == "" {
			return "", true
		}
		n, incomplete := leakHeaderLen(trimmed)
		if incomplete {
			return value, false
		}
		if n > 0 {
			rest = trimmed[n:]
			progressed = true
			continue
		}
		if strings.HasPrefix(trimmed, "{") {
			next, done := skipJSONObject(trimmed, !progressed)
			if !done {
				return value, false
			}
			if next == trimmed {
				break
			}
			rest = next
			progressed = true
			continue
		}
		token, leftover := nextGarbageToken(trimmed)
		if isLeakGarbage(token) || (progressed && isLeakResidue(token)) {
			rest = leftover
			progressed = true
			continue
		}
		if !progressed {
			for _, marker := range leakMarkers {
				if strings.HasPrefix(trimmed, marker) {
					rest = trimmed[len(marker):]
					progressed = true
					break
				}
			}
			if progressed {
				continue
			}
		}
		break
	}
	if !progressed {
		return value, false
	}
	return rest, true
}

func leakHeaderLen(s string) (int, bool) {
	if strings.HasPrefix(s, "<|") {
		if j := strings.Index(s, "|>"); j >= 0 {
			return j + 2, false
		}
		if strings.Contains(s, "\n") {
			return strings.IndexByte(s, '\n'), false
		}
		return 0, true
	}
	if strings.HasPrefix(s, "</function>") {
		return len("</function>"), false
	}
	if strings.HasPrefix(s, "<function") {
		if j := strings.Index(s, ">"); j >= 0 {
			return j + 1, false
		}
		if strings.Contains(s, "\n") {
			return strings.IndexByte(s, '\n'), false
		}
		return 0, true
	}
	if strings.HasPrefix(s, "```") {
		end := 3
		for end < len(s) && s[end] != '\n' && s[end] != '\r' && s[end] != ' ' && s[end] != '\t' {
			end++
		}
		word := s[3:end]
		if word == "tool" || word == "json" {
			return end, false
		}
		if end == len(s) && word != "" && (strings.HasPrefix("tool", word) || strings.HasPrefix("json", word)) {
			return 0, true
		}
		return 0, false
	}
	if strings.HasPrefix(s, "to=") {
		return parseToHeader(s)
	}
	if strings.HasPrefix(s, "recipient=") {
		return parseRecipientHeader(s)
	}
	if strings.HasPrefix(s, "functions.") {
		return parseFunctionsHeader(s)
	}
	return 0, false
}

func parseToHeader(s string) (int, bool) {
	name, complete := readIdent(s[3:])
	match, incomplete := identLooksLikeTool(name, !complete)
	if incomplete {
		return 0, true
	}
	if !match {
		return 0, false
	}
	return consumeCallSuffix(s, 3+len(name)), false
}

func parseFunctionsHeader(s string) (int, bool) {
	name, complete := readIdent(s[len("functions."):])
	match, incomplete := identLooksLikeTool("functions."+name, !complete)
	if incomplete {
		return 0, true
	}
	if !match {
		return 0, false
	}
	n := len("functions.") + len(name)
	if n < len(s) && s[n] == '(' {
		n++
	}
	return consumeCallSuffix(s, n), false
}

func parseRecipientHeader(s string) (int, bool) {
	name, complete := readIdent(s[len("recipient="):])
	match, incomplete := identLooksLikeTool(name, !complete)
	if incomplete {
		return 0, true
	}
	if !match {
		return 0, false
	}
	return consumeCallSuffix(s, len("recipient=")+len(name)), false
}

func readIdent(s string) (string, bool) {
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-' {
			i++
			continue
		}
		return s[:i], true
	}
	return s, false
}

func identLooksLikeTool(name string, atEnd bool) (bool, bool) {
	if isKnownToolName(name) {
		return true, false
	}
	if atEnd && (name == "" || (len(name) >= 3 && isStrictPrefixOfTool(name))) {
		return true, true
	}
	return false, false
}

func isKnownToolName(name string) bool {
	if name == "" {
		return false
	}
	parts := strings.Split(name, ".")
	last := parts[len(parts)-1]
	for _, tool := range leakToolNames {
		if name == tool {
			return true
		}
		if last == tool && tool != "functions" {
			return true
		}
	}
	return false
}

func isStrictPrefixOfTool(name string) bool {
	for _, tool := range leakToolNames {
		if strings.HasPrefix(tool, name) && name != tool {
			return true
		}
	}
	if strings.HasPrefix(name, "functions.") {
		rest := name[len("functions."):]
		for _, tool := range leakToolNames {
			if tool != "functions" && strings.HasPrefix(tool, rest) && rest != tool {
				return true
			}
		}
	}
	if strings.HasPrefix("functions.", name) {
		return true
	}
	return false
}

func consumeCallSuffix(s string, n int) int {
	for n < len(s) {
		switch s[n] {
		case ' ', '\t':
			n++
			continue
		case ':':
			return n + 1
		case '(':
			close := strings.IndexByte(s[n:], ')')
			if close < 0 {
				return n
			}
			inner := strings.ToLower(strings.TrimSpace(s[n+1 : n+close]))
			if inner == "commentary" || inner == "code" || strings.HasPrefix(inner, "commentary") {
				n += close + 1
				continue
			}
			return n
		}
		rest := s[n:]
		if strings.HasPrefix(rest, "code") && (len(rest) == 4 || !isIdentChar(rest[4])) {
			n += 4
			continue
		}
		if strings.HasPrefix(rest, "commentary") && (len(rest) == 10 || !isIdentChar(rest[10])) {
			n += 10
			continue
		}
		break
	}
	return n
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func skipJSONObject(value string, toolishOnly bool) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(value))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return value, false
	}
	if toolishOnly {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil || !toolishObject(object) {
			return value, true
		}
	}
	return value[decoder.InputOffset():], true
}

func isLeakResidue(token string) bool {
	if token == ")" || token == "(" || token == ":" || token == "," || token == ";" {
		return true
	}
	trimmed := strings.Trim(token, "():,")
	switch strings.ToLower(trimmed) {
	case "code", "commentary", "function", "functions", "content", "recipient", "constrain", "tool", "json":
		return true
	}
	if strings.HasPrefix(token, "recipient=") || strings.HasPrefix(token, "to=") || strings.HasPrefix(token, "functions.") {
		return true
	}
	return false
}

func nextGarbageToken(value string) (string, string) {
	i := 0
	for i < len(value) {
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == ' ' || r == '\t' || r == '\n' {
			break
		}
		i += size
	}
	if i == 0 {
		return "", value
	}
	return value[:i], value[i:]
}

func isLeakGarbage(token string) bool {
	letters, nonLatin := 0, 0
	for _, r := range token {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if r > unicode.MaxASCII {
			nonLatin++
		}
	}
	return letters > 0 && nonLatin*2 >= letters
}

func looksLikeProse(value string) bool {
	value = strings.TrimLeft(value, " \t")
	if value == "" || leakStart(value) == 0 || strings.HasPrefix(value, `{"`) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(value)
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func toolishObject(object map[string]any) bool {
	for _, key := range []string{
		"path", "command", "query", "offset_line", "limit_lines", "timeout_seconds",
		"changes", "action", "old_text", "new_text", "glob", "max_depth", "max_results",
		"tool_uses", "recipient_name",
	} {
		if _, ok := object[key]; ok {
			return true
		}
	}
	return false
}
