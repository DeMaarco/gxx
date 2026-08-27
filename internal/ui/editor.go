package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"gxx/internal/config"
)

const (
	maxHistoryEntries = 200
	escapeWait        = 25 * time.Millisecond
)

type keyKind int

const (
	keyNone keyKind = iota
	keyRune
	keyEnter
	keyTab
	keyBackspace
	keyDelete
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyEsc
	keyCtrlC
	keyCtrlD
	keyCtrlU
	keyCtrlW
	keyCtrlL
)

type keyEvent struct {
	kind keyKind
	char rune
}

type inputState struct {
	buffer            []rune
	cursor            int
	suggest           int
	history           []string
	histPos           int
	draft             []rune
	picker            pickerMode
	models            []string
	modelIndex        int
	optionIndex       int
	pickContext       string
	pickEffort        string
	pickFast          bool
	sessionModel      string
	sessionContext    string
	sessionEffort     string
	sessionFast       bool
	sessionPermission string
	modeIndex         int
}

func (s *inputState) text() string {
	return string(s.buffer)
}

func (s *inputState) setText(value string) {
	s.buffer = []rune(value)
	s.cursor = len(s.buffer)
}

func (s *inputState) matches() []slashCommand {
	return matchingCommands(s.text())
}

func (s *inputState) ghost() string {
	if s.cursor != len(s.buffer) {
		return ""
	}
	matches := s.matches()
	if len(matches) == 0 {
		return ""
	}
	if s.suggest < 0 || s.suggest >= len(matches) {
		return ""
	}
	selected := matches[s.suggest].name
	current := s.text()
	if len(selected) <= len(current) || selected[:len(current)] != current {
		return ""
	}
	return selected[len(current):]
}

func (s *inputState) afterEdit() {
	s.histPos = len(s.history)
	if s.picker == pickerModels || s.picker == pickerOptions {
		if !strings.HasPrefix(s.text(), "/model") {
			s.picker = pickerClosed
		}
	}
	if s.picker == pickerModes && !isModePickerText(s.text()) {
		s.picker = pickerClosed
	}
	if s.picker == pickerContext && !strings.HasPrefix(s.text(), "/context") {
		s.picker = pickerClosed
	}
	if s.picker == pickerModels {
		s.clampModelIndex()
	}
	if s.picker == pickerModes {
		s.clampModeIndex()
	}
	matches := s.matches()
	if len(matches) == 0 {
		s.suggest = 0
		return
	}
	current := s.text()
	for index, match := range matches {
		if match.name == current {
			// Prefer an exact name so "/mode" is not stolen by "/model".
			s.suggest = index
			return
		}
	}
	if s.suggest >= len(matches) {
		s.suggest = len(matches) - 1
	}
}

func (s *inputState) insert(char rune) {
	if char < 32 && char != '\t' {
		return
	}
	s.buffer = append(s.buffer[:s.cursor], append([]rune{char}, s.buffer[s.cursor:]...)...)
	s.cursor++
	s.afterEdit()
}

func (s *inputState) backspace() {
	if s.cursor == 0 {
		return
	}
	s.buffer = append(s.buffer[:s.cursor-1], s.buffer[s.cursor:]...)
	s.cursor--
	s.afterEdit()
}

func (s *inputState) deleteForward() {
	if s.cursor >= len(s.buffer) {
		return
	}
	s.buffer = append(s.buffer[:s.cursor], s.buffer[s.cursor+1:]...)
	s.afterEdit()
}

func (s *inputState) moveLeft() {
	if s.cursor > 0 {
		s.cursor--
	}
}

func (s *inputState) moveRight() {
	if s.cursor < len(s.buffer) {
		s.cursor++
	}
}

func (s *inputState) deleteToStart() {
	s.buffer = append([]rune(nil), s.buffer[s.cursor:]...)
	s.cursor = 0
	s.afterEdit()
}

func (s *inputState) deleteWord() {
	if s.cursor == 0 {
		return
	}
	end := s.cursor
	for s.cursor > 0 && s.buffer[s.cursor-1] == ' ' {
		s.cursor--
	}
	for s.cursor > 0 && s.buffer[s.cursor-1] != ' ' {
		s.cursor--
	}
	s.buffer = append(s.buffer[:s.cursor], s.buffer[end:]...)
	s.afterEdit()
}

func (s *inputState) complete() {
	if s.picker == pickerModels {
		s.picker = pickerOptions
		s.optionIndex = 0
		return
	}
	if s.picker == pickerOptions {
		s.picker = pickerModels
		return
	}
	if s.picker == pickerModes {
		return
	}
	if s.picker == pickerContext {
		return
	}
	matches := s.matches()
	if len(matches) == 0 {
		return
	}
	if matches[s.suggest].name == "/model" {
		s.setText("/model")
		s.startPicker()
		return
	}
	if matches[s.suggest].name == "/mode" {
		s.setText("/mode")
		s.startModePicker()
		return
	}
	if matches[s.suggest].name == "/context" {
		s.setText("/context")
		s.startContextPicker()
		return
	}
	s.setText(matches[s.suggest].name)
	s.afterEdit()
}

func (s *inputState) startPicker() {
	s.picker = pickerModels
	s.models = catalogModels(s.sessionModel)
	s.modelIndex = 0
	for index, model := range s.models {
		if model == s.sessionModel {
			s.modelIndex = index
			break
		}
	}
	s.optionIndex = 0
	s.pickContext = s.sessionContext
	if s.pickContext == "" {
		s.pickContext = "272k"
	}
	s.pickEffort = s.sessionEffort
	if s.pickEffort == "" {
		s.pickEffort = "medium"
	}
	s.pickFast = s.sessionFast
	s.clampModelIndex()
}

func (s *inputState) modelQuery() string {
	return strings.TrimSpace(strings.TrimPrefix(s.text(), "/model"))
}

func (s *inputState) visibleModels() []string {
	query := strings.ToLower(s.modelQuery())
	if query == "" {
		return append([]string(nil), s.models...)
	}
	visible := make([]string, 0, len(s.models))
	for _, model := range s.models {
		if strings.Contains(strings.ToLower(model), query) {
			visible = append(visible, model)
		}
	}
	return visible
}

func (s *inputState) clampModelIndex() {
	visible := s.visibleModels()
	if len(visible) == 0 {
		s.modelIndex = 0
		return
	}
	if s.modelIndex >= len(visible) {
		s.modelIndex = len(visible) - 1
	}
	if s.modelIndex < 0 {
		s.modelIndex = 0
	}
}

func (s *inputState) selectedModel() string {
	visible := s.visibleModels()
	if len(visible) == 0 {
		return strings.TrimSpace(s.sessionModel)
	}
	return visible[s.modelIndex]
}

func (s *inputState) modelCommand() string {
	model := s.selectedModel()
	if model == "" {
		return ""
	}
	return encodeModelCommand(model, s.pickContext, s.pickEffort, s.pickFast)
}

func (s *inputState) startModePicker() {
	s.picker = pickerModes
	s.modeIndex = 0
	for index, mode := range config.PermissionModes {
		if mode == s.sessionPermission {
			s.modeIndex = index
			break
		}
	}
	s.clampModeIndex()
}

func (s *inputState) modeQuery() string {
	return strings.TrimSpace(strings.TrimPrefix(s.text(), "/mode"))
}

func (s *inputState) visibleModes() []string {
	query := strings.ToLower(s.modeQuery())
	if query == "" {
		return append([]string(nil), config.PermissionModes...)
	}
	visible := make([]string, 0, len(config.PermissionModes))
	for _, mode := range config.PermissionModes {
		if strings.Contains(mode, query) {
			visible = append(visible, mode)
		}
	}
	return visible
}

func (s *inputState) clampModeIndex() {
	visible := s.visibleModes()
	if len(visible) == 0 {
		s.modeIndex = 0
		return
	}
	if s.modeIndex >= len(visible) {
		s.modeIndex = len(visible) - 1
	}
	if s.modeIndex < 0 {
		s.modeIndex = 0
	}
}

func (s *inputState) selectedPermission() string {
	visible := s.visibleModes()
	if len(visible) == 0 {
		return ""
	}
	return visible[s.modeIndex]
}

func (s *inputState) modeCommand() string {
	if mode := s.selectedPermission(); mode != "" {
		return encodeModeCommand(mode)
	}
	return strings.TrimSpace(s.text())
}

func (s *inputState) startContextPicker() {
	s.picker = pickerContext
}

func (s *inputState) cycleOption(delta int) {
	switch s.optionIndex {
	case optionContext:
		s.pickContext = cycleValue(config.ContextSizes, s.pickContext, delta)
	case optionEffort:
		s.pickEffort = cycleValue(effortValues, s.pickEffort, delta)
	case optionFast:
		s.pickFast = !s.pickFast
	}
}

func (s *inputState) submit() string {
	if s.picker == pickerModels || s.picker == pickerOptions {
		return s.modelCommand()
	}
	if s.picker == pickerModes {
		return s.modeCommand()
	}
	if s.picker == pickerContext {
		s.picker = pickerClosed
		s.setText("")
		s.afterEdit()
		return ""
	}
	if s.openModelPicker() {
		return ""
	}
	if s.openModePicker() {
		return ""
	}
	if s.openContextPicker() {
		return ""
	}
	matches := s.matches()
	if len(matches) > 0 {
		current := s.text()
		selected := matches[s.suggest].name
		if current == "/" || (len(selected) >= len(current) && selected[:len(current)] == current) {
			if selected == "/model" {
				s.setText("/model")
				s.startPicker()
				return ""
			}
			if selected == "/mode" {
				s.setText("/mode")
				s.startModePicker()
				return ""
			}
			if selected == "/context" {
				s.setText("/context")
				s.startContextPicker()
				return ""
			}
			return selected
		}
	}
	return s.text()
}

func (s *inputState) openModelPicker() bool {
	if s.picker != pickerClosed {
		return false
	}
	if strings.TrimSpace(s.text()) != "/model" {
		return false
	}
	s.startPicker()
	return true
}

func (s *inputState) openModePicker() bool {
	if s.picker != pickerClosed {
		return false
	}
	if strings.TrimSpace(s.text()) != "/mode" {
		return false
	}
	s.startModePicker()
	return true
}

func (s *inputState) openContextPicker() bool {
	if s.picker != pickerClosed {
		return false
	}
	if strings.TrimSpace(s.text()) != "/context" {
		return false
	}
	s.startContextPicker()
	return true
}

func (s *inputState) up() {
	if s.picker == pickerModels {
		if s.modelIndex > 0 {
			s.modelIndex--
		}
		return
	}
	if s.picker == pickerOptions {
		if s.optionIndex > 0 {
			s.optionIndex--
		}
		return
	}
	if s.picker == pickerModes {
		if s.modeIndex > 0 {
			s.modeIndex--
		}
		return
	}
	if s.picker == pickerContext {
		return
	}
	if matches := s.matches(); len(matches) > 0 {
		if s.suggest > 0 {
			s.suggest--
		}
		return
	}
	if len(s.history) == 0 || s.histPos == 0 {
		return
	}
	if s.histPos == len(s.history) {
		s.draft = append([]rune(nil), s.buffer...)
	}
	s.histPos--
	s.setText(s.history[s.histPos])
}

func (s *inputState) down() {
	if s.picker == pickerModels {
		if s.modelIndex < len(s.visibleModels())-1 {
			s.modelIndex++
		}
		return
	}
	if s.picker == pickerOptions {
		if s.optionIndex < optionCount-1 {
			s.optionIndex++
		}
		return
	}
	if s.picker == pickerModes {
		if s.modeIndex < len(s.visibleModes())-1 {
			s.modeIndex++
		}
		return
	}
	if s.picker == pickerContext {
		return
	}
	if matches := s.matches(); len(matches) > 0 {
		if s.suggest < len(matches)-1 {
			s.suggest++
		}
		return
	}
	if s.histPos >= len(s.history) {
		return
	}
	s.histPos++
	if s.histPos == len(s.history) {
		s.setText(string(s.draft))
		return
	}
	s.setText(s.history[s.histPos])
}

func (s *inputState) remember(line string) {
	if line == "" {
		return
	}
	if n := len(s.history); n > 0 && s.history[n-1] == line {
		s.histPos = n
		return
	}
	s.history = append(s.history, line)
	if len(s.history) > maxHistoryEntries {
		s.history = s.history[len(s.history)-maxHistoryEntries:]
	}
	s.histPos = len(s.history)
}

func (s *inputState) apply(event keyEvent) (submitted string, eof bool, handled bool) {
	switch event.kind {
	case keyRune:
		s.insert(event.char)
	case keyEnter:
		return s.submit(), false, true
	case keyTab:
		s.complete()
	case keyBackspace:
		s.backspace()
	case keyDelete:
		s.deleteForward()
	case keyUp:
		s.up()
	case keyDown:
		s.down()
	case keyLeft:
		if s.picker == pickerOptions {
			s.cycleOption(-1)
		} else {
			s.moveLeft()
		}
	case keyRight:
		if s.picker == pickerOptions {
			s.cycleOption(1)
		} else if ghost := s.ghost(); ghost != "" && s.cursor == len(s.buffer) {
			s.complete()
		} else {
			s.moveRight()
		}
	case keyHome:
		s.cursor = 0
	case keyEnd:
		s.cursor = len(s.buffer)
	case keyEsc:
		if s.picker == pickerOptions {
			s.picker = pickerModels
		} else if s.picker == pickerModels || s.picker == pickerModes || s.picker == pickerContext {
			s.picker = pickerClosed
			s.setText("")
			s.afterEdit()
		} else if len(s.matches()) > 0 {
			s.suggest = 0
			s.setText("")
			s.afterEdit()
		}
	case keyCtrlU:
		s.deleteToStart()
	case keyCtrlW:
		s.deleteWord()
	case keyCtrlC:
		s.picker = pickerClosed
		s.setText("")
		s.afterEdit()
	case keyCtrlD:
		if len(s.buffer) == 0 {
			return "", true, true
		}
		s.deleteForward()
	case keyCtrlL, keyNone:
	default:
	}
	return "", false, false
}

type lineEditor struct {
	in    *os.File
	out   io.Writer
	color bool
	state inputState
}

func lineEditorEnabled(stdin *os.File, stdout io.Writer) bool {
	if stdin == nil || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := stdout.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func (e *lineEditor) Read(ctx context.Context, settings REPLSettings) (string, error) {
	fd := int(e.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	e.state.buffer = nil
	e.state.cursor = 0
	e.state.suggest = 0
	e.state.histPos = len(e.state.history)
	e.state.draft = nil
	e.state.sessionModel = settings.Model
	e.state.sessionContext = settings.Context
	e.state.sessionEffort = settings.Effort
	e.state.sessionFast = settings.Fast
	e.state.sessionPermission = settings.PermissionMode
	e.state.picker = pickerClosed
	e.color = settings.Color
	if err := e.render(settings); err != nil {
		return "", err
	}

	type result struct {
		event keyEvent
		err   error
	}
	for {
		if err := ctx.Err(); err != nil {
			e.finish("")
			return "", err
		}
		read := make(chan result, 1)
		go func() {
			event, err := readKey(e.in)
			read <- result{event: event, err: err}
		}()
		var event keyEvent
		select {
		case <-ctx.Done():
			_ = e.in.SetReadDeadline(time.Now())
			select {
			case <-read:
			case <-time.After(200 * time.Millisecond):
			}
			_ = e.in.SetReadDeadline(time.Time{})
			e.finish("")
			return "", ctx.Err()
		case value := <-read:
			if value.err != nil {
				if errors.Is(value.err, io.EOF) {
					e.finish("")
					return "", io.EOF
				}
				return "", value.err
			}
			event = value.event
		}
		line, eof, submitted := e.state.apply(event)
		if eof {
			e.finish("")
			return "", io.EOF
		}
		if submitted {
			line = strings.TrimSpace(line)
			if line == "" {
				if err := e.render(settings); err != nil {
					return "", err
				}
				continue
			}
			e.state.remember(line)
			e.finish(line)
			return line, nil
		}
		if err := e.render(settings); err != nil {
			return "", err
		}
	}
}

func (e *lineEditor) render(settings REPLSettings) error {
	if e.state.picker != pickerClosed {
		return e.renderPicker(settings)
	}
	matches := e.state.matches()
	if e.state.suggest >= len(matches) {
		e.state.suggest = max(len(matches)-1, 0)
	}
	var line string
	line += "\r" + eraseLine + "> " + string(e.state.buffer)
	if ghost := e.state.ghost(); ghost != "" {
		line += paint(e.color, dim, ghost)
	}
	line += eraseDown + "\r\n"
	for index, command := range matches {
		marker := "  "
		name := command.name
		help := paint(e.color, dim, command.help)
		if index == e.state.suggest {
			marker = paint(e.color, cyan, "▸ ")
			name = paint(e.color, bold+cyan, command.name)
		}
		line += marker + name + "  " + help + "\r\n"
	}
	line += formatStatus(settings)
	up := 1 + len(matches)
	line += fmt.Sprintf("\x1b[%dA\x1b[%dG", up, 3+e.state.cursor)
	_, err := io.WriteString(e.out, line)
	return err
}

func (e *lineEditor) renderPicker(settings REPLSettings) error {
	if e.state.picker == pickerModes {
		return e.renderModePicker(settings)
	}
	if e.state.picker == pickerContext {
		return e.renderContextPicker(settings)
	}
	e.state.clampModelIndex()
	var body strings.Builder
	if e.state.picker == pickerOptions {
		fast := "off"
		if e.state.pickFast {
			fast = "on"
		}
		options := []struct {
			name  string
			value string
		}{
			{"context", e.state.pickContext},
			{"effort", e.state.pickEffort},
			{"fast", fast},
		}
		for index, option := range options {
			marker := "  "
			name := option.name
			value := option.value
			if index == e.state.optionIndex {
				marker = paint(e.color, cyan, "▸ ")
				name = paint(e.color, bold+cyan, option.name)
				value = paint(e.color, cyan, option.value)
			}
			body.WriteString(marker + name + "  " + value + "\r\n")
		}
		body.WriteString(paint(e.color, dim, "← → cycle · tab models · enter apply") + "\r\n")
	} else {
		visible := e.state.visibleModels()
		if len(visible) == 0 {
			body.WriteString(paint(e.color, dim, "  no matching models") + "\r\n")
		}
		for index, model := range visible {
			marker := "  "
			label := model
			note := ""
			if model == e.state.sessionModel {
				note = paint(e.color, dim, "  in use")
			}
			if index == e.state.modelIndex {
				marker = paint(e.color, cyan, "▸ ")
				label = paint(e.color, bold+cyan, model)
			}
			body.WriteString(marker + label + note + "\r\n")
		}
		body.WriteString(paint(e.color, dim, "tab options · enter apply · esc cancel") + "\r\n")
	}
	rows := strings.Count(body.String(), "\r\n")
	output := "\r" + eraseLine + "> " + string(e.state.buffer) + eraseDown + "\r\n"
	output += body.String()
	output += formatStatus(settings)
	output += fmt.Sprintf("\x1b[%dA\x1b[%dG", 1+rows, 3+e.state.cursor)
	_, err := io.WriteString(e.out, output)
	return err
}

func (e *lineEditor) renderModePicker(settings REPLSettings) error {
	e.state.clampModeIndex()
	var body strings.Builder
	visible := e.state.visibleModes()
	if len(visible) == 0 {
		body.WriteString(paint(e.color, dim, "  no matching modes") + "\r\n")
	}
	for index, mode := range visible {
		marker := "  "
		label := paintPermission(e.color, mode)
		note := paint(e.color, dim, "  "+permissionHelp(mode))
		if mode == e.state.sessionPermission {
			note += paint(e.color, dim, "  in use")
		}
		if index == e.state.modeIndex {
			marker = paint(e.color, cyan, "▸ ")
			if mode != config.PermissionAuto {
				label = paint(e.color, bold+cyan, mode)
			}
		}
		body.WriteString(marker + label + note + "\r\n")
	}
	body.WriteString(paint(e.color, dim, "enter apply · esc cancel") + "\r\n")
	rows := strings.Count(body.String(), "\r\n")
	output := "\r" + eraseLine + "> " + string(e.state.buffer) + eraseDown + "\r\n"
	output += body.String()
	output += formatStatus(settings)
	output += fmt.Sprintf("\x1b[%dA\x1b[%dG", 1+rows, 3+e.state.cursor)
	_, err := io.WriteString(e.out, output)
	return err
}

func (e *lineEditor) renderContextPicker(settings REPLSettings) error {
	text := strings.TrimRight(FormatContext(settings.contextUsage(), e.color), "\n")
	var body strings.Builder
	for _, line := range strings.Split(text, "\n") {
		body.WriteString(line + "\r\n")
	}
	body.WriteString(paint(e.color, dim, "enter / esc close") + "\r\n")
	rows := strings.Count(body.String(), "\r\n")
	output := "\r" + eraseLine + "> " + string(e.state.buffer) + eraseDown + "\r\n"
	output += body.String()
	output += formatStatus(settings)
	output += fmt.Sprintf("\x1b[%dA\x1b[%dG", 1+rows, 3+e.state.cursor)
	_, err := io.WriteString(e.out, output)
	return err
}

func (e *lineEditor) finish(line string) {
	_, _ = io.WriteString(e.out, "\r"+eraseLine+"> "+line+eraseDown+"\r\n")
}

const (
	eraseLine = "\x1b[2K"
	eraseDown = "\x1b[J"
)

func readKey(reader io.Reader) (keyEvent, error) {
	first, err := readByte(reader)
	if err != nil {
		return keyEvent{}, err
	}
	switch first {
	case 0x0d, 0x0a:
		return keyEvent{kind: keyEnter}, nil
	case 0x09:
		return keyEvent{kind: keyTab}, nil
	case 0x7f, 0x08:
		return keyEvent{kind: keyBackspace}, nil
	case 0x01:
		return keyEvent{kind: keyHome}, nil
	case 0x05:
		return keyEvent{kind: keyEnd}, nil
	case 0x02:
		return keyEvent{kind: keyLeft}, nil
	case 0x06:
		return keyEvent{kind: keyRight}, nil
	case 0x10:
		return keyEvent{kind: keyUp}, nil
	case 0x0e:
		return keyEvent{kind: keyDown}, nil
	case 0x15:
		return keyEvent{kind: keyCtrlU}, nil
	case 0x17:
		return keyEvent{kind: keyCtrlW}, nil
	case 0x0c:
		return keyEvent{kind: keyCtrlL}, nil
	case 0x03:
		return keyEvent{kind: keyCtrlC}, nil
	case 0x04:
		return keyEvent{kind: keyCtrlD}, nil
	case 0x1b:
		return readEscape(reader)
	default:
		return readRuneEvent(reader, first)
	}
}

func readEscape(reader io.Reader) (keyEvent, error) {
	bracket, err := readByteTimeout(reader, escapeWait)
	if err != nil {
		return keyEvent{kind: keyEsc}, nil
	}
	if bracket != '[' {
		return keyEvent{kind: keyEsc}, nil
	}
	code, err := readByteTimeout(reader, escapeWait)
	if err != nil {
		return keyEvent{kind: keyEsc}, nil
	}
	switch code {
	case 'A':
		return keyEvent{kind: keyUp}, nil
	case 'B':
		return keyEvent{kind: keyDown}, nil
	case 'C':
		return keyEvent{kind: keyRight}, nil
	case 'D':
		return keyEvent{kind: keyLeft}, nil
	case 'H':
		return keyEvent{kind: keyHome}, nil
	case 'F':
		return keyEvent{kind: keyEnd}, nil
	case '3':
		if tilde, err := readByteTimeout(reader, escapeWait); err == nil && tilde == '~' {
			return keyEvent{kind: keyDelete}, nil
		}
		return keyEvent{kind: keyNone}, nil
	default:
		return keyEvent{kind: keyNone}, nil
	}
}

func readRuneEvent(reader io.Reader, first byte) (keyEvent, error) {
	if first < 0x20 {
		return keyEvent{kind: keyNone}, nil
	}
	if first < 0x80 {
		return keyEvent{kind: keyRune, char: rune(first)}, nil
	}
	size := 2
	switch {
	case first >= 0xf0:
		size = 4
	case first >= 0xe0:
		size = 3
	}
	buf := make([]byte, size)
	buf[0] = first
	for index := 1; index < size; index++ {
		next, err := readByte(reader)
		if err != nil {
			return keyEvent{}, err
		}
		buf[index] = next
	}
	char, width := utf8.DecodeRune(buf)
	if char == utf8.RuneError && width == 1 {
		return keyEvent{kind: keyNone}, nil
	}
	return keyEvent{kind: keyRune, char: char}, nil
}

func readByte(reader io.Reader) (byte, error) {
	var buf [1]byte
	_, err := io.ReadFull(reader, buf[:])
	return buf[0], err
}

func readByteTimeout(reader io.Reader, wait time.Duration) (byte, error) {
	file, ok := reader.(*os.File)
	if !ok {
		return readByte(reader)
	}
	_ = file.SetReadDeadline(time.Now().Add(wait))
	defer file.SetReadDeadline(time.Time{})
	value, err := readByte(file)
	if err != nil {
		return 0, err
	}
	return value, nil
}
