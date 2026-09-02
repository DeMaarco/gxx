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
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"gxx/internal/auth"
	"gxx/internal/config"
	"gxx/internal/osutil"
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
	keyShiftTab
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
	keyCtrlO
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
	activeAccount     string
	availableModels   []string
	catalogLocked     bool
	loginIndex        int
	modeIndex         int
	exitArmed         bool
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
	if s.picker == pickerLogin && !strings.HasPrefix(s.text(), "/login") {
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
	s.exitArmed = false
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
	if matches[s.suggest].name == "/login" {
		s.setText("/login")
		s.startLoginPicker()
		return
	}
	s.setText(matches[s.suggest].name)
	s.afterEdit()
}

func (s *inputState) startPicker() {
	s.picker = pickerModels
	s.models = s.catalog()
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

func (s *inputState) catalog() []string {
	account := s.activeAccount
	live := s.availableModels
	if !s.catalogLocked {
		if account == "" {
			if config.IsClaudeModel(s.sessionModel) {
				account = config.AccountClaude
			} else if strings.TrimSpace(s.sessionModel) != "" {
				account = config.AccountAPI
			}
		}
		if live != nil {
			return catalogModels(s.sessionModel, account, live)
		}
		return catalogModels(s.sessionModel, account, nil)
	}
	return catalogModels(s.sessionModel, s.activeAccount, s.availableModels)
}

func (s *inputState) startLoginPicker() {
	s.picker = pickerLogin
	options := auth.Options(s.activeAccount)
	s.loginIndex = 0
	for index, option := range options {
		if option.ID == s.activeAccount {
			s.loginIndex = index
			break
		}
	}
	s.clampLoginIndex()
}

func (s *inputState) loginOptions() []auth.Option {
	return auth.Options(s.activeAccount)
}

func (s *inputState) clampLoginIndex() {
	options := s.loginOptions()
	if len(options) == 0 {
		s.loginIndex = 0
		return
	}
	if s.loginIndex >= len(options) {
		s.loginIndex = len(options) - 1
	}
	if s.loginIndex < 0 {
		s.loginIndex = 0
	}
}

func (s *inputState) selectedLogin() string {
	options := s.loginOptions()
	if len(options) == 0 || s.loginIndex < 0 || s.loginIndex >= len(options) {
		return ""
	}
	return options[s.loginIndex].ID
}

func (s *inputState) loginCommand() string {
	if id := s.selectedLogin(); id != "" {
		return "/login " + id
	}
	return strings.TrimSpace(s.text())
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
	if s.picker == pickerLogin {
		return s.loginCommand()
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
	if s.openLoginPicker() {
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
			if selected == "/login" {
				s.setText("/login")
				s.startLoginPicker()
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

func (s *inputState) openLoginPicker() bool {
	if s.picker != pickerClosed {
		return false
	}
	if strings.TrimSpace(s.text()) != "/login" {
		return false
	}
	s.startLoginPicker()
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
	if s.picker == pickerLogin {
		if s.loginIndex > 0 {
			s.loginIndex--
		}
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
	if s.picker == pickerLogin {
		if s.loginIndex < len(s.loginOptions())-1 {
			s.loginIndex++
		}
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
	if event.kind != keyCtrlC {
		s.exitArmed = false
	}
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
		} else if s.picker == pickerModels || s.picker == pickerModes || s.picker == pickerContext || s.picker == pickerLogin {
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
		if s.picker != pickerClosed || len(s.buffer) > 0 {
			s.exitArmed = false
			s.picker = pickerClosed
			s.setText("")
			s.afterEdit()
			break
		}
		if s.exitArmed {
			return "", true, true
		}
		s.exitArmed = true
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
	in        *os.File
	out       io.Writer
	color     bool
	columns   int
	rows      int
	cursorRow int
	state     inputState
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

func (e *lineEditor) Read(ctx context.Context, settings *REPLSettings) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("repl settings are required")
	}
	fd := int(e.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)
	if file, ok := e.out.(*os.File); ok {
		defer osutil.EnableConsoleVT(file)()
	}

	_, _ = io.WriteString(e.out, wrapOff)
	defer func() { _, _ = io.WriteString(e.out, wrapOn) }()

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
	e.state.activeAccount = settings.ActiveAccount
	e.state.availableModels = settings.Models
	e.state.catalogLocked = true
	e.state.picker = pickerClosed
	e.state.exitArmed = false
	e.cursorRow = 0
	e.color = settings.Color
	if err := e.render(*settings); err != nil {
		return "", err
	}

	type result struct {
		event keyEvent
		err   error
	}
	for {
		if err := ctx.Err(); err != nil {
			e.finish(*settings, "")
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
			osutil.InterruptRead(e.in)
			select {
			case <-read:
			case <-time.After(200 * time.Millisecond):
			}
			osutil.ClearReadDeadline(e.in)
			e.finish(*settings, "")
			return "", ctx.Err()
		case value := <-read:
			if value.err != nil {
				if errors.Is(value.err, io.EOF) {
					e.finish(*settings, "")
					return "", io.EOF
				}
				return "", value.err
			}
			event = value.event
		}
		if event.kind == keyShiftTab {
			e.state.exitArmed = false
			if e.state.picker == pickerClosed {
				cycleSession(settings)
			}
			if err := e.render(*settings); err != nil {
				return "", err
			}
			continue
		}
		if event.kind == keyCtrlO && e.state.picker == pickerClosed && settings.ChooseConversation != nil {
			e.finish(*settings, "")
			id, err := settings.ChooseConversation(ctx, e.out)
			if err != nil {
				return "", err
			}
			if id != "" {
				settings.PendingConversationID = id
				return "", nil
			}
			if err := e.render(*settings); err != nil {
				return "", err
			}
			continue
		}
		line, eof, submitted := e.state.apply(event)
		if eof {
			e.finish(*settings, "")
			return "", io.EOF
		}
		if submitted {
			line = strings.TrimSpace(line)
			if line == "" {
				if err := e.render(*settings); err != nil {
					return "", err
				}
				continue
			}
			e.state.remember(line)
			e.finish(*settings, line)
			return line, nil
		}
		if err := e.render(*settings); err != nil {
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
	var body strings.Builder
	for index, command := range matches {
		marker := "  "
		name := command.name
		help := paint(e.color, dim, command.help)
		if index == e.state.suggest {
			marker = paint(e.color, cyan, "▸ ")
			name = paint(e.color, bold+cyan, command.name)
		}
		body.WriteString(marker + name + "    " + help + "\r\n")
	}
	return e.paintFrame(settings, body.String(), len(matches), true)
}

func (e *lineEditor) renderPicker(settings REPLSettings) error {
	if e.state.picker == pickerModes {
		return e.renderModePicker(settings)
	}
	if e.state.picker == pickerContext {
		return e.renderContextPicker(settings)
	}
	if e.state.picker == pickerLogin {
		return e.renderLoginPicker(settings)
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
			if e.state.activeAccount == "" && e.state.catalogLocked {
				body.WriteString(paint(e.color, dim, "  no models — run /login") + "\r\n")
			} else {
				body.WriteString(paint(e.color, dim, "  no matching models") + "\r\n")
			}
		}
		width := e.termWidth()
		start, end := windowRange(len(visible), e.state.modelIndex, e.pickerListLimit(settings, 1))
		for index := start; index < end; index++ {
			model := visible[index]
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
			line := marker + label + note
			if width > 4 {
				line = truncateVisible(line, width-1)
			}
			body.WriteString(line + "\r\n")
		}
		hint := "tab options · enter apply · esc cancel"
		if end-start < len(visible) {
			hint = fmt.Sprintf("%s · %d/%d", hint, e.state.modelIndex+1, len(visible))
		}
		body.WriteString(paint(e.color, dim, hint) + "\r\n")
	}
	return e.paintFrame(settings, body.String(), strings.Count(body.String(), "\r\n"), true)
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
	return e.paintFrame(settings, body.String(), strings.Count(body.String(), "\r\n"), true)
}

func (e *lineEditor) renderLoginPicker(settings REPLSettings) error {
	e.state.clampLoginIndex()
	var body strings.Builder
	options := e.state.loginOptions()
	if len(options) == 0 {
		body.WriteString(paint(e.color, dim, "  no login options") + "\r\n")
	}
	for index, option := range options {
		active := option.ID == e.state.activeAccount
		selected := index == e.state.loginIndex
		marker := "  "
		label := option.Label
		help := paint(e.color, dim, "  "+option.Help)
		switch {
		case active:
			marker = paint(e.color, green, "● ")
			label = paint(e.color, bold+green, option.Label)
		case selected:
			marker = paint(e.color, cyan, "▸ ")
			label = paint(e.color, bold+cyan, option.Label)
		}
		body.WriteString(marker + label + help + "\r\n")
	}
	body.WriteString(paint(e.color, dim, "enter apply · esc cancel") + "\r\n")
	return e.paintFrame(settings, body.String(), strings.Count(body.String(), "\r\n"), true)
}

func (e *lineEditor) renderContextPicker(settings REPLSettings) error {
	text := strings.TrimRight(FormatContext(settings.contextUsage(), e.color), "\n")
	var body strings.Builder
	for _, line := range strings.Split(text, "\n") {
		body.WriteString(line + "\r\n")
	}
	body.WriteString(paint(e.color, dim, "enter / esc close") + "\r\n")
	return e.paintFrame(settings, body.String(), strings.Count(body.String(), "\r\n"), true)
}

func (e *lineEditor) paintFrame(settings REPLSettings, body string, bodyRows int, ghost bool) error {
	width := e.termWidth()
	prefix := promptPrefix(settings)
	text := string(e.state.buffer)
	ghostText := ""
	if ghost {
		ghostText = e.state.ghost()
	}
	display := prefix + text
	if ghostText != "" {
		display += paint(e.color, dim, ghostText)
	}
	cells := visibleWidth(display)
	cursorCells := visibleWidth(prefix) + visibleWidth(string(e.state.buffer[:e.state.cursor]))
	promptRows := promptRowCount(cells, width)
	cursorRow, cursorCol := promptCursorPos(cursorCells, width)

	var out strings.Builder
	out.WriteString(promptHome(e.cursorRow))
	lines := wrapVisible(display, width)
	for index, line := range lines {
		out.WriteString(line)
		if index < len(lines)-1 {
			out.WriteString("\r\n")
		}
	}
	if width > 1 && cells > 0 && cells%width == 0 {
		out.WriteString("\r\n")
	}
	out.WriteString("\r\n")
	if body != "" {
		out.WriteString("\r\n")
		bodyRows++
	}
	out.WriteString(body)
	out.WriteString(e.statusLine(settings))
	up := promptRows - cursorRow + bodyRows
	if up > 0 {
		fmt.Fprintf(&out, "\x1b[%dA", up)
	}
	col := cursorCol + 1
	if width > 1 && col > width {
		col = width
	}
	fmt.Fprintf(&out, "\x1b[%dG", col)
	e.cursorRow = cursorRow
	_, err := io.WriteString(e.out, out.String())
	return err
}

func (e *lineEditor) finish(settings REPLSettings, line string) {
	var out strings.Builder
	out.WriteString(promptHome(e.cursorRow))
	out.WriteString(wrapOn)
	prefix := promptPrefix(settings)
	display := prefix + line
	lines := wrapVisible(display, e.termWidth())
	for index, wrapped := range lines {
		out.WriteString(wrapped)
		if index < len(lines)-1 {
			out.WriteString("\r\n")
		}
	}
	out.WriteString(eraseDown)
	out.WriteString("\r\n")
	e.cursorRow = 0
	_, _ = io.WriteString(e.out, out.String())
}

func (e *lineEditor) statusLine(settings REPLSettings) string {
	if e.state.exitArmed {
		return paint(e.color, yellow, "Ctrl+C again to exit")
	}
	return formatStatusLine(settings)
}

func (e *lineEditor) termWidth() int {
	width, _ := e.termSize()
	return width
}

func (e *lineEditor) termHeight() int {
	_, height := e.termSize()
	return height
}

func (e *lineEditor) termSize() (width, height int) {
	width, height = 80, 24
	if e.columns > 1 {
		width = e.columns
	}
	if e.rows > 1 {
		height = e.rows
	}
	if e.columns > 1 && e.rows > 1 {
		return width, height
	}
	readSize := func(file *os.File) (int, int, bool) {
		if file == nil {
			return 0, 0, false
		}
		w, h, err := term.GetSize(int(file.Fd()))
		if err != nil || w < 2 {
			return 0, 0, false
		}
		if h < 2 {
			h = 24
		}
		return w, h, true
	}
	if e.columns <= 1 || e.rows <= 1 {
		if w, h, ok := readSize(e.in); ok {
			if e.columns <= 1 {
				width = w
			}
			if e.rows <= 1 {
				height = h
			}
		} else if file, ok := e.out.(*os.File); ok {
			if w, h, ok := readSize(file); ok {
				if e.columns <= 1 {
					width = w
				}
				if e.rows <= 1 {
					height = h
				}
			}
		} else if cols, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && cols > 1 && e.columns <= 1 {
			width = cols
		}
	}
	return width, height
}

func (e *lineEditor) pickerListLimit(settings REPLSettings, extraRows int) int {
	width, height := e.termSize()
	prefix := promptPrefix(settings)
	cells := visibleWidth(prefix + string(e.state.buffer))
	promptRows := promptRowCount(cells, width)
	reserved := promptRows + extraRows + 1
	limit := height - reserved
	if limit < 3 {
		return 3
	}
	return limit
}

func windowRange(n, index, limit int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	if index < 0 {
		index = 0
	}
	if index >= n {
		index = n - 1
	}
	if limit < 1 || n <= limit {
		return 0, n
	}
	start = index - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > n {
		start = n - limit
	}
	return start, start + limit
}

func promptRowCount(cells, width int) int {
	if width < 1 {
		return 1
	}
	if cells < 1 {
		return 1
	}
	rows := (cells + width - 1) / width
	if cells%width == 0 {
		rows++
	}
	return rows
}

func promptCursorPos(cells, width int) (row, col int) {
	if width < 1 {
		return 0, max(cells, 0)
	}
	if cells < 0 {
		cells = 0
	}
	return cells / width, cells % width
}

func promptHome(cursorRow int) string {
	if cursorRow <= 0 {
		return "\r" + eraseDown
	}
	return fmt.Sprintf("\r\x1b[%dA%s", cursorRow, eraseDown)
}

func wrapVisible(value string, width int) []string {
	if width < 1 {
		return []string{value}
	}
	var lines []string
	var current strings.Builder
	currentW := 0
	inEscape := false
	for i := 0; i < len(value); {
		if value[i] == '\x1b' {
			inEscape = true
			current.WriteByte(value[i])
			i++
			continue
		}
		if inEscape {
			current.WriteByte(value[i])
			if (value[i] >= 'A' && value[i] <= 'Z') || (value[i] >= 'a' && value[i] <= 'z') {
				inEscape = false
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(value[i:])
		if currentW == width {
			lines = append(lines, current.String())
			current.Reset()
			currentW = 0
		}
		current.WriteString(value[i : i+size])
		currentW++
		i += size
	}
	return append(lines, current.String())
}

func renderPromptFrame(out io.Writer, settings REPLSettings, text string, width, prevCursorRow int) (int, error) {
	e := &lineEditor{out: out, columns: width, cursorRow: prevCursorRow, color: settings.Color}
	e.state.setText(text)
	if err := e.render(settings); err != nil {
		return 0, err
	}
	return e.cursorRow, nil
}

func finishPromptFrame(out io.Writer, settings REPLSettings, text string, width, prevCursorRow int) {
	e := &lineEditor{out: out, columns: width, cursorRow: prevCursorRow, color: settings.Color}
	e.finish(settings, text)
}

const (
	eraseDown = "\x1b[J"
	wrapOff   = "\x1b[?7l"
	wrapOn    = "\x1b[?7h"
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
	case 0x0f:
		return keyEvent{kind: keyCtrlO}, nil
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
	seq, err := readCSI(reader, code)
	if err != nil {
		return keyEvent{kind: keyEsc}, nil
	}
	switch seq {
	case "A":
		return keyEvent{kind: keyUp}, nil
	case "B":
		return keyEvent{kind: keyDown}, nil
	case "C":
		return keyEvent{kind: keyRight}, nil
	case "D":
		return keyEvent{kind: keyLeft}, nil
	case "H":
		return keyEvent{kind: keyHome}, nil
	case "F":
		return keyEvent{kind: keyEnd}, nil
	case "Z", "1;2Z":
		return keyEvent{kind: keyShiftTab}, nil
	case "3~":
		return keyEvent{kind: keyDelete}, nil
	case "27;2;9~":
		return keyEvent{kind: keyShiftTab}, nil
	default:
		return keyEvent{kind: keyNone}, nil
	}
}

func readCSI(reader io.Reader, first byte) (string, error) {
	seq := []byte{first}
	if isCSIFinal(first) {
		return string(seq), nil
	}
	for len(seq) < 24 {
		next, err := readByteTimeout(reader, escapeWait)
		if err != nil {
			return string(seq), nil
		}
		seq = append(seq, next)
		if isCSIFinal(next) {
			return string(seq), nil
		}
	}
	return string(seq), nil
}

func isCSIFinal(value byte) bool {
	return value >= 0x40 && value <= 0x7e
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
