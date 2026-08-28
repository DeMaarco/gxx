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
	"strings"
)

const (
	headingStyle = bold + cyan
	codeStyle    = yellow
	blockStyle   = dim
	quoteStyle   = dim
	bulletGlyph  = "• "
	quoteGlyph   = "│ "
	blockIndent  = "  "
	ruleGlyph    = "───"
	// maxMarkerScan bounds how far the renderer waits for a line opener to
	// become unambiguous, so a stream of spaces or digits cannot stall output.
	maxMarkerScan = 16
)

// markdownState carries block context between streamed chunks. A fence, a
// heading, or a list marker can be split across deltas, so the style of a byte
// is not decidable from that byte alone.
type markdownState struct {
	hold      string
	lineStyle string
	lineStart bool
	skipLine  bool
	inFence   bool
	fenceChar byte
	fenceLen  int
}

func newMarkdownState() markdownState {
	return markdownState{lineStart: true}
}

func formatMarkdown(color bool, value string) string {
	state := newMarkdownState()
	return renderMarkdown(color, &state, value, true)
}

// renderMarkdown renders one streamed chunk. Input that is still ambiguous is
// kept in the state and rendered once more arrives; flush treats whatever is
// left as literal text.
func renderMarkdown(color bool, state *markdownState, value string, flush bool) string {
	rest := state.hold + value
	state.hold = ""

	var out strings.Builder
	for rest != "" {
		if state.skipLine {
			index := strings.IndexByte(rest, '\n')
			if index < 0 {
				return out.String()
			}
			rest = rest[index+1:]
			state.beginLine()
			continue
		}
		if state.lineStart {
			consumed, decoration, decided := state.openLine(color, rest, flush)
			if !decided {
				state.hold = rest
				return out.String()
			}
			out.WriteString(decoration)
			rest = rest[consumed:]
			state.lineStart = false
			continue
		}
		consumed, text, held := state.renderInline(color, rest, flush)
		out.WriteString(text)
		rest = rest[consumed:]
		if held {
			state.hold = rest
			return out.String()
		}
	}
	if flush {
		out.WriteString(state.closeStyle(color))
		state.lineStyle = ""
	}
	return out.String()
}

func (s *markdownState) beginLine() {
	s.lineStart = true
	s.skipLine = false
	s.lineStyle = ""
}

// beginStyle opens a style that runs to the end of the line. Emitting it once
// per line rather than once per chunk keeps a byte-sized delta from wrapping
// every character in its own escape sequence.
func (s *markdownState) beginStyle(color bool, style string) string {
	s.lineStyle = style
	if !color {
		return ""
	}
	return style
}

func (s *markdownState) closeStyle(color bool) string {
	if !color || s.lineStyle == "" {
		return ""
	}
	return reset
}

// span renders an inline run and restores whatever style the line opened with.
func (s *markdownState) span(color bool, style, text string) string {
	if !color || text == "" {
		return text
	}
	if s.lineStyle == "" {
		return style + text + reset
	}
	return reset + style + text + reset + s.lineStyle
}

// openLine decides how a logical line starts. It reports the bytes consumed
// from value, the decoration to print in their place, and whether the chunk
// held enough input to decide at all.
func (s *markdownState) openLine(color bool, value string, flush bool) (int, string, bool) {
	if s.inFence {
		return s.openFencedLine(color, value, flush)
	}

	indent := leadingSpaces(value)
	if indent == len(value) {
		if flush || indent >= maxMarkerScan {
			return indent, value[:indent], true
		}
		return 0, "", false
	}
	rest := value[indent:]

	// Each opener that does not match falls through to the next one: `***` is a
	// break, `* ` a bullet, and `**x**` neither.
	switch rest[0] {
	case '`', '~':
		consumed, decided := s.openFence(rest, flush)
		if !decided {
			return 0, "", false
		}
		if consumed > 0 {
			return indent + consumed, "", true
		}
	case '#':
		consumed, decided := s.openHeading(rest, flush)
		if !decided {
			return 0, "", false
		}
		if consumed > 0 {
			return indent + consumed, s.beginStyle(color, headingStyle), true
		}
	case '>':
		if len(rest) == 1 && !flush {
			return 0, "", false
		}
		consumed := 1
		if len(rest) > 1 && rest[1] == ' ' {
			consumed = 2
		}
		return indent + consumed, spaces(indent) + s.beginStyle(color, quoteStyle) + quoteGlyph, true
	}

	if strings.IndexByte("-*_", rest[0]) >= 0 {
		consumed, decided := horizontalRule(rest, rest[0], flush)
		if !decided {
			return 0, "", false
		}
		if consumed > 0 {
			s.skipLine = true
			return indent + consumed, spaces(indent) + paint(color, dim, ruleGlyph) + "\n", true
		}
	}

	if strings.IndexByte("-*+", rest[0]) >= 0 {
		if len(rest) == 1 && !flush {
			return 0, "", false
		}
		if len(rest) > 1 && rest[1] == ' ' {
			return indent + 2, spaces(indent) + paint(color, dim, bulletGlyph), true
		}
	}

	if isDigit(rest[0]) {
		consumed, marker, decided := orderedMarker(rest, flush)
		if !decided {
			return 0, "", false
		}
		if consumed > 0 {
			return indent + consumed, spaces(indent) + paint(color, dim, marker), true
		}
	}

	return indent, value[:indent], true
}

// openFencedLine handles a line that starts inside a fenced code block: either
// the closing fence, which is dropped, or another line of code.
func (s *markdownState) openFencedLine(color bool, value string, flush bool) (int, string, bool) {
	indent := 0
	for indent < len(value) && indent < 3 && value[indent] == ' ' {
		indent++
	}
	run := runLength(value[indent:], s.fenceChar)
	if run >= s.fenceLen {
		s.inFence = false
		s.skipLine = true
		return indent + run, "", true
	}
	if indent+run == len(value) && !flush && indent+run > 0 {
		return 0, "", false
	}
	return 0, blockIndent + s.beginStyle(color, blockStyle), true
}

// openFence opens a fenced code block and swallows the info string. Runs
// shorter than three characters are inline code and are left to renderInline.
func (s *markdownState) openFence(rest string, flush bool) (int, bool) {
	marker := rest[0]
	run := runLength(rest, marker)
	if run >= 3 {
		s.inFence = true
		s.fenceChar = marker
		s.fenceLen = run
		s.skipLine = true
		return run, true
	}
	if run == len(rest) && !flush {
		return 0, false
	}
	return 0, true
}

func (s *markdownState) openHeading(rest string, flush bool) (int, bool) {
	level := runLength(rest, '#')
	if level > 6 {
		return 0, true
	}
	if level == len(rest) {
		if !flush {
			return 0, false
		}
		return 0, true
	}
	if rest[level] != ' ' {
		return 0, true
	}
	s.lineStyle = headingStyle
	return level + 1, true
}

// renderInline renders marker-aware text up to the end of the current line.
// held reports that the chunk stopped inside an unfinished marker.
func (s *markdownState) renderInline(color bool, value string, flush bool) (int, string, bool) {
	var out strings.Builder
	i := 0
	for i < len(value) {
		if value[i] == '\n' {
			out.WriteString(s.closeStyle(color))
			out.WriteByte('\n')
			s.beginLine()
			return i + 1, out.String(), false
		}
		if s.inFence {
			end := lineEnd(value[i:])
			out.WriteString(value[i : i+end])
			i += end
			continue
		}

		switch {
		case value[i] == '`':
			body, complete := currentLine(value[i+1:])
			if end := strings.IndexByte(body, '`'); end >= 0 {
				out.WriteString(s.span(color, codeStyle, body[:end]))
				i += end + 2
				continue
			}
			if !complete && !flush {
				return i, out.String(), true
			}
			out.WriteByte('`')
			i++

		case strings.HasPrefix(value[i:], "**"):
			body, complete := currentLine(value[i+2:])
			if end := strings.Index(body, "**"); end >= 0 {
				out.WriteString(s.span(color, bold, body[:end]))
				i += end + 4
				continue
			}
			if !complete && !flush {
				return i, out.String(), true
			}
			out.WriteString("**")
			i += 2

		case value[i] == '*' && i == len(value)-1 && !flush:
			return i, out.String(), true

		default:
			// Always past the current byte: reaching here means it starts no
			// marker, and a zero-length run would spin forever.
			end := i + 1
			for end < len(value) && plainByte(value, end) {
				end++
			}
			out.WriteString(value[i:end])
			i = end
		}
	}
	return i, out.String(), false
}

// plainByte reports that the byte at index carries no inline marker, so a run
// of them can be written without re-inspecting each one.
func plainByte(value string, index int) bool {
	switch value[index] {
	case '\n', '`':
		return false
	case '*':
		return false
	}
	return true
}

// horizontalRule reports the length of a thematic break. It needs the end of
// the line to tell `---` from the start of `***bold***`.
func horizontalRule(value string, marker byte, flush bool) (int, bool) {
	run := runLength(value, marker)
	if run == len(value) && !flush {
		// The run may still grow into a break on the next chunk.
		return 0, false
	}
	if run < 3 {
		return 0, true
	}
	i := run
	for i < len(value) && (value[i] == ' ' || value[i] == '\t' || value[i] == marker) {
		i++
	}
	if i == len(value) {
		if flush {
			return i, true
		}
		return 0, false
	}
	if value[i] == '\n' {
		return i, true
	}
	return 0, true
}

func orderedMarker(rest string, flush bool) (int, string, bool) {
	digits := 0
	for digits < len(rest) && isDigit(rest[digits]) {
		digits++
	}
	if digits > 9 {
		return 0, "", true
	}
	if digits == len(rest) {
		if !flush {
			return 0, "", false
		}
		return 0, "", true
	}
	if rest[digits] != '.' && rest[digits] != ')' {
		return 0, "", true
	}
	if digits+1 == len(rest) {
		if !flush {
			return 0, "", false
		}
		return 0, "", true
	}
	if rest[digits+1] != ' ' {
		return 0, "", true
	}
	return digits + 2, rest[:digits+1] + " ", true
}

// currentLine returns value up to the next newline and whether that newline
// was present, so an inline marker never pairs across a line break.
func currentLine(value string) (string, bool) {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index], true
	}
	return value, false
}

func lineEnd(value string) int {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return index
	}
	return len(value)
}

func leadingSpaces(value string) int {
	count := 0
	for count < len(value) && value[count] == ' ' {
		count++
	}
	return count
}

func runLength(value string, marker byte) int {
	count := 0
	for count < len(value) && value[count] == marker {
		count++
	}
	return count
}

func spaces(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(" ", count)
}

func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}
