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
	"io"
	"time"
)

const (
	ClearLine     = clearLine
	ColorBold     = bold
	ColorDim      = dim
	ColorRed      = red
	ColorGreen    = green
	ColorYellow   = yellow
	ColorCyan     = cyan
	KeyRune       = keyRune
	KeyEnter      = keyEnter
	KeyTab        = keyTab
	KeyShiftTab   = keyShiftTab
	KeyDelete     = keyDelete
	KeyUp         = keyUp
	KeyDown       = keyDown
	KeyRight      = keyRight
	KeyCtrlC      = keyCtrlC
	PickerClosed  = int(pickerClosed)
	PickerModels  = int(pickerModels)
	PickerOptions = int(pickerOptions)
	PickerModes   = int(pickerModes)
	PickerContext = int(pickerContext)
	OptionContext = optionContext
	OptionEffort  = optionEffort
	OptionFast    = optionFast
)

var (
	SpinnerFrames       = spinnerFrames
	LiveLine            = liveLine
	FormatElapsed       = formatElapsed
	FormatToolDuration  = formatToolDuration
	FormatCompactTokens = formatCompactTokens
	FormatTurnUsage     = formatTurnUsage
	HoldModelText       = holdModelText
	FormatMarkdown      = formatMarkdown
	ToolHint            = toolHint
	VisibleWidth        = visibleWidth
	CompactRunningLabel = compactRunningLabelForTest
	Paint               = paint
	WriteChrome         = writeChrome
	FormatStatus        = formatStatus
	PromptPrefix        = promptPrefix
	CatalogModels       = catalogModels
	ParseModelCommand   = parseModelCommand
	ParseModeCommand    = parseModeCommand
	EncodeModelCommand  = encodeModelCommand
	EncodeModeCommand   = encodeModeCommand
	MatchingCommands    = matchingCommands
	LookupSlashCommand  = lookupSlashCommand
	TogglePlan          = togglePlan
	ContextPercentColor = contextPercentColor
	IsModePickerText    = isModePickerText
	ReadKey             = readKeyForTest
	ReadLine            = readLine
	RenderPromptFrame   = renderPromptFrame
	WrapVisible         = wrapVisible
	PromptHome          = promptHome
)

type ModelCommand = modelCommand
type ModeCommand = modeCommand
type KeyKind = keyKind

type RunningTool struct {
	Name string
	Hint string
}

type KeyEvent struct {
	Kind KeyKind
	Char rune
}

type InputState struct {
	inner inputState
}

type TurnGate struct {
	inner turnGate
}

func (c slashCommand) Name() string {
	return c.name
}

func (r *Renderer) SetLive(live bool) {
	r.live = live
}

func (r *Renderer) SetSpinEvery(every time.Duration) {
	r.spinEvery = every
}

func (r *Renderer) SetNow(now func() time.Time) {
	r.now = now
}

func (r *Renderer) SetColumns(columns int) {
	r.columns = columns
}

func compactRunningLabelForTest(color bool, tools []RunningTool) string {
	converted := make([]runningTool, len(tools))
	for i, tool := range tools {
		converted[i] = runningTool{name: tool.Name, hint: tool.Hint}
	}
	return compactRunningLabel(color, converted)
}

func readKeyForTest(reader io.Reader) (KeyEvent, error) {
	event, err := readKey(reader)
	return KeyEvent{Kind: event.kind, Char: event.char}, err
}

func (s *InputState) Insert(char rune) { s.inner.insert(char) }
func (s *InputState) Remember(line string) {
	s.inner.remember(line)
}
func (s *InputState) SetText(value string) { s.inner.setText(value) }
func (s *InputState) Text() string         { return s.inner.text() }
func (s *InputState) Ghost() string        { return s.inner.ghost() }
func (s *InputState) SelectedModel() string {
	return s.inner.selectedModel()
}
func (s *InputState) SelectedPermission() string {
	return s.inner.selectedPermission()
}
func (s *InputState) Apply(kind KeyKind) (string, bool, bool) {
	return s.inner.apply(keyEvent{kind: keyKind(kind)})
}
func (s *InputState) ExitArmed() bool { return s.inner.exitArmed }
func (s *InputState) SetHistPosToEnd() {
	s.inner.histPos = len(s.inner.history)
}
func (s *InputState) Picker() int { return int(s.inner.picker) }
func (s *InputState) SetOptionIndex(index int) {
	s.inner.optionIndex = index
}
func (s *InputState) PickEffort() string  { return s.inner.pickEffort }
func (s *InputState) PickContext() string { return s.inner.pickContext }
func (s *InputState) PickFast() bool      { return s.inner.pickFast }
func (s *InputState) SetSession(model, contextValue, effort, permission string) {
	s.inner.sessionModel = model
	s.inner.sessionContext = contextValue
	s.inner.sessionEffort = effort
	s.inner.sessionPermission = permission
}

func (g *TurnGate) Start(parent context.Context) (context.Context, context.CancelFunc) {
	return g.inner.start(parent)
}

func (g *TurnGate) Handle(cancelSession context.CancelFunc) {
	g.inner.handle(cancelSession)
}
