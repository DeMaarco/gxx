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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
	"unicode"

	"gxx/internal/agent"
	"gxx/internal/config"
)

const maxPromptBytes = 1024 * 1024

type REPLSettings struct {
	Version          string
	Model            string
	PermissionMode   string
	Effort           string
	Context          string
	Fast             bool
	Plan             bool
	Workspace        string
	Color            bool
	Stdin            *os.File
	APIKeyConfigured bool
	ReadAPIKey       func(context.Context) (string, error)
	SaveAPIKey       func(string) (string, error)
	FetchUsage       func(context.Context) (agent.UsageReport, error)
	FetchContext     func() agent.ContextUsage
	SyncSession      func(REPLSettings) error
	SetPlan          func(bool) error
}

type Renderer struct {
	writer    io.Writer
	color     bool
	live      bool
	spinEvery time.Duration
	now       func() time.Time
	columns   int

	mu         sync.Mutex
	sawText    bool
	textOpen   bool
	liveOpen   bool
	liveHeight int
	cursorHide bool
	thinking   bool
	thinkStart time.Time
	frame      int
	running    []runningTool
	pending    pendingDoneGroup
	reveal     []activityLine
	heldText   string
	usage      agent.Usage

	animMu   sync.Mutex
	animStop chan struct{}
	animDone chan struct{}
}

func NewRenderer(writer io.Writer) *Renderer {
	return NewRendererWithColor(writer, false)
}

func NewRendererWithColor(writer io.Writer, color bool) *Renderer {
	return &Renderer{
		writer:    writer,
		color:     color,
		live:      liveOutput(writer),
		spinEvery: 80 * time.Millisecond,
	}
}

func (r *Renderer) StartTurn() {
	r.stopAnim()
	r.mu.Lock()
	r.sawText = false
	r.textOpen = false
	r.running = nil
	r.pending = pendingDoneGroup{}
	r.reveal = nil
	r.heldText = ""
	r.frame = 0
	r.liveHeight = 0
	r.liveOpen = false
	r.usage = agent.Usage{}
	r.thinking = r.live
	r.thinkStart = r.timestamp()
	spin := r.shouldSpinLocked()
	if spin {
		r.drawLiveLocked()
	}
	r.mu.Unlock()
	if spin {
		r.startAnim()
	}
}

func (r *Renderer) Event(event agent.Event) {
	if event.Kind == agent.EventToolProgress {
		r.mu.Lock()
		r.handleProgressLocked(event)
		if r.live {
			r.drawLiveLocked()
		}
		r.mu.Unlock()
		return
	}
	r.stopAnim()
	r.mu.Lock()
	spin := r.handleEventLocked(event)
	if !spin {
		r.showCursorLocked()
	}
	r.mu.Unlock()
	if spin {
		r.startAnim()
	}
}

func (r *Renderer) handleEventLocked(event agent.Event) bool {
	switch event.Kind {
	case agent.EventTextDelta:
		r.clearLiveLocked()
		r.flushRevealLocked()
		r.flushPendingDoneLocked()
		r.queueTextLocked(event.Text)
	case agent.EventToolCall:
		r.clearLiveLocked()
		r.flushRevealLocked()
		r.dropHeldTextLocked()
		r.flushPendingDoneLocked()
		r.thinking = false
	case agent.EventToolStarted:
		r.clearLiveLocked()
		r.flushRevealLocked()
		r.dropHeldTextLocked()
		name := ""
		if event.ToolCall != nil {
			name = strings.TrimSpace(event.ToolCall.Name)
		}
		if r.pending.count > 0 && r.pending.name != name {
			r.flushPendingDoneLocked()
		}
		r.endTextLine()
		r.thinking = false
		if event.ToolCall != nil {
			r.running = append(r.running, newRunningTool(event.ToolCall, r.timestamp()))
			if !r.live {
				_, _ = fmt.Fprintf(
					r.writer,
					"%s %s\n",
					paint(r.color, dim, "→"),
					runningToolLabel(r.color, r.running[len(r.running)-1]),
				)
			}
		}
	case agent.EventToolDone:
		r.clearLiveLocked()
		r.endTextLine()
		tool := r.takeRunning(event.Result)
		r.noteToolDoneLocked(event.Result, tool)
		if r.live && len(r.running) == 0 && len(r.reveal) == 0 {
			r.thinking = true
			r.thinkStart = r.timestamp()
		}
	case agent.EventNotice:
		r.clearLiveLocked()
		r.flushRevealLocked()
		r.flushPendingDoneLocked()
		r.endTextLine()
		if event.Text != "" {
			_, _ = fmt.Fprintf(r.writer, "%s\n", paint(r.color, dim, safeTerminalText(event.Text)))
		}
	case agent.EventUsage:
		r.usage = event.Usage
	}
	if r.shouldSpinLocked() {
		r.drawLiveLocked()
		return true
	}
	return false
}

func (r *Renderer) handleProgressLocked(event agent.Event) {
	detail := strings.TrimSpace(event.Text)
	if detail == "" {
		return
	}
	id := ""
	name := ""
	if event.ToolCall != nil {
		id = event.ToolCall.ID
		name = strings.TrimSpace(event.ToolCall.Name)
	}
	for i := range r.running {
		if id != "" && r.running[i].id == id {
			r.running[i].detail = safeTerminalText(detail)
			return
		}
	}
	if id != "" {
		return
	}
	for i := range r.running {
		if name == "" || r.running[i].name == name {
			r.running[i].detail = safeTerminalText(detail)
			return
		}
	}
}

func (r *Renderer) noteToolDoneLocked(result *agent.ToolResult, tool runningTool) {
	if result == nil {
		return
	}
	extra := doneExtraLines(result, tool)
	stats := formatLineCounts(r.color, "", tool.added, tool.deleted, tool.removed)
	if result.IsError {
		r.flushPendingDoneLocked()
		r.flushRevealLocked()
		r.writeToolDoneLocked(doneLine{
			name:      result.Name,
			hints:     compactHints([]string{tool.hint}),
			count:     1,
			duration:  result.DurationMS,
			truncated: result.Truncated,
			failed:    true,
			detail:    firstLine(result.Output),
		})
		return
	}
	if len(extra) > 0 || stats != "" {
		r.flushPendingDoneLocked()
		r.writeToolDoneLocked(doneLine{
			name:      result.Name,
			hints:     compactHints([]string{tool.hint}),
			stats:     stats,
			count:     1,
			duration:  result.DurationMS,
			truncated: result.Truncated,
		})
		r.queueRevealLocked(extra)
		return
	}
	if r.pending.count > 0 && r.pending.name != result.Name {
		r.flushPendingDoneLocked()
	}
	r.pending.add(result.Name, tool.hint, result.DurationMS, result.Truncated)
}

func (r *Renderer) queueRevealLocked(lines []activityLine) {
	if len(lines) == 0 {
		return
	}
	if !r.live || r.spinEvery <= 0 {
		for _, line := range lines {
			r.writeActivityLineLocked(line)
		}
		return
	}
	r.reveal = append(r.reveal, lines...)
}

func (r *Renderer) flushRevealLocked() {
	if len(r.reveal) == 0 {
		return
	}
	for _, line := range r.reveal {
		r.writeActivityLineLocked(line)
	}
	r.reveal = nil
}

func (r *Renderer) writeActivityLineLocked(line activityLine) {
	_, _ = fmt.Fprintln(r.writer, formatActivityLine(r.color, line))
}

func (r *Renderer) flushPendingDoneLocked() {
	if r.pending.count == 0 {
		return
	}
	r.writeToolDoneLocked(doneLine{
		name:      r.pending.name,
		hints:     compactHints(r.pending.hints),
		count:     r.pending.count,
		duration:  r.pending.duration,
		truncated: r.pending.truncated,
	})
	r.pending = pendingDoneGroup{}
}

func (r *Renderer) writeToolDoneLocked(line doneLine) {
	name := paint(r.color, green, safeTerminalText(line.name))
	mark := paint(r.color, green, "✓")
	if line.failed {
		name = paint(r.color, red, safeTerminalText(line.name))
		mark = paint(r.color, red, "✗")
	}
	if line.count > 1 {
		name += paint(r.color, dim, fmt.Sprintf(" ×%d", line.count))
	}
	if line.hints != "" {
		name += "  " + safeTerminalText(line.hints)
	}
	if line.stats != "" {
		name += "  " + line.stats
	}
	detail := formatToolDuration(line.duration)
	if line.truncated {
		detail += ", truncated"
	}
	if line.failed {
		_, _ = fmt.Fprintf(
			r.writer,
			"%s %s  %s: %s\n",
			mark,
			name,
			paint(r.color, dim, "("+detail+")"),
			safeTerminalText(line.detail),
		)
		return
	}
	_, _ = fmt.Fprintf(
		r.writer,
		"%s %s  %s\n",
		mark,
		name,
		paint(r.color, dim, "("+detail+")"),
	)
}

func (r *Renderer) queueTextLocked(text string) {
	r.thinking = false
	r.heldText += text
	if holdModelText(r.heldText) {
		return
	}
	r.writeTextLocked(r.heldText)
	r.heldText = ""
}

func (r *Renderer) writeTextLocked(text string) {
	text = safeTerminalText(text)
	if text == "" {
		return
	}
	_, _ = io.WriteString(r.writer, text)
	r.sawText = true
	r.textOpen = !strings.HasSuffix(text, "\n")
}

func (r *Renderer) dropHeldTextLocked() {
	if holdModelText(r.heldText) {
		r.heldText = ""
	}
}

func (r *Renderer) Finish(answer string) {
	r.stopAnim()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearLiveLocked()
	r.flushRevealLocked()
	r.flushPendingDoneLocked()
	r.showCursorLocked()
	r.thinking = false
	r.running = nil
	r.reveal = nil
	if holdModelText(r.heldText) {
		r.heldText = ""
	} else if r.heldText != "" {
		r.writeTextLocked(r.heldText)
		r.heldText = ""
	}
	if !r.sawText && strings.TrimSpace(answer) != "" && !holdModelText(answer) {
		r.writeTextLocked(answer)
	}
	r.endTextLine()
	if line := formatTurnUsage(r.color, r.usage); line != "" {
		_, _ = fmt.Fprintln(r.writer, line)
	}
}

func (r *Renderer) endTextLine() {
	if r.textOpen {
		_, _ = io.WriteString(r.writer, "\n")
		r.textOpen = false
	}
}

func RunREPL(
	ctx context.Context,
	loop *agent.Loop,
	reader *bufio.Reader,
	renderer *Renderer,
	writer io.Writer,
	settings REPLSettings,
) error {
	if !settings.APIKeyConfigured {
		fmt.Fprintln(writer, paint(settings.Color, dim, "OpenAI API key is not configured. Run /config."))
		fmt.Fprintln(writer)
	}

	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	var turns turnGate
	go watchInterrupts(sessionCtx, interrupts, cancelSession, &turns)

	var editor *lineEditor
	if lineEditorEnabled(settings.Stdin, writer) {
		editor = &lineEditor{in: settings.Stdin, out: writer, color: settings.Color}
	}

	for {
		if err := sessionCtx.Err(); err != nil {
			return err
		}
		var line string
		var err error
		if editor != nil {
			if err := writeHeader(writer, settings); err != nil {
				return err
			}
			line, err = editor.Read(sessionCtx, &settings)
		} else {
			if err := writeChrome(writer, settings); err != nil {
				return err
			}
			line, err = readLine(sessionCtx, reader, settings.Stdin)
			clearStatusLine(writer, settings.Color)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if editor == nil && !settings.Color {
					_, _ = fmt.Fprintln(writer)
				}
				return nil
			}
			return err
		}
		if len(line) > maxPromptBytes {
			fmt.Fprintln(writer, paint(settings.Color, red, "error: prompt is too large"))
			continue
		}

		prompt := strings.TrimSpace(line)
		if prompt == "" {
			if settings.Color {
				_, _ = fmt.Fprint(writer, cursorUp+clearLine)
			}
			continue
		}
		if strings.HasPrefix(prompt, "/") {
			name, _, slashErr := lookupSlashCommand(prompt)
			if slashErr != nil {
				fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+slashErr.Error()))
				fmt.Fprintln(writer)
				continue
			}
			switch name {
			case "/exit":
				return nil
			case "/clear":
				loop.Reset()
				fmt.Fprintln(writer, paint(settings.Color, dim, "Conversation cleared."))
				fmt.Fprintln(writer)
				continue
			case "/config":
				if err := configureAPIKey(sessionCtx, loop, writer, settings); err != nil {
					fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
				}
				fmt.Fprintln(writer)
				continue
			case "/help":
				printREPLHelp(writer, settings)
				fmt.Fprintln(writer)
				continue
			case "/usage":
				if err := showUsage(sessionCtx, writer, settings); err != nil {
					fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
				}
				fmt.Fprintln(writer)
				continue
			case "/context":
				printContext(writer, settings.Color, settings.contextUsage())
				fmt.Fprintln(writer)
				continue
			case "/model":
				changedModel, err := applyModelCommand(writer, &settings, prompt)
				if err != nil {
					fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
				} else if changedModel {
					loop.Reset()
					fmt.Fprintln(writer, paint(settings.Color, dim, "Conversation cleared."))
				}
				fmt.Fprintln(writer)
				continue
			case "/mode":
				if err := applyModeCommand(writer, &settings, prompt); err != nil {
					fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
				}
				fmt.Fprintln(writer)
				continue
			}
		}

		turnCtx, finishTurn := turns.start(sessionCtx)
		renderer.StartTurn()
		result, err := loop.Run(turnCtx, prompt, renderer.Event)
		renderer.Finish(result.Answer)
		finishTurn()
		if sessionCtx.Err() != nil {
			return sessionCtx.Err()
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(writer, paint(settings.Color, red, "interrupted"))
			} else {
				fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
			}
		}
		fmt.Fprintln(writer)
	}
}

type turnGate struct {
	mu          sync.Mutex
	cancelTurn  context.CancelFunc
	active      bool
	interrupted bool
}

func (g *turnGate) start(parent context.Context) (context.Context, context.CancelFunc) {
	turnCtx, cancel := context.WithCancel(parent)
	g.mu.Lock()
	g.cancelTurn = cancel
	g.active = true
	g.interrupted = false
	g.mu.Unlock()
	return turnCtx, func() {
		cancel()
		g.mu.Lock()
		g.active = false
		g.cancelTurn = nil
		g.interrupted = false
		g.mu.Unlock()
	}
}

func (g *turnGate) handle(cancelSession context.CancelFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active || g.interrupted {
		cancelSession()
		return
	}
	g.interrupted = true
	if g.cancelTurn != nil {
		g.cancelTurn()
	}
}

func watchInterrupts(
	ctx context.Context,
	interrupts <-chan os.Signal,
	cancelSession context.CancelFunc,
	turns *turnGate,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-interrupts:
			turns.handle(cancelSession)
		}
	}
}

func printREPLHelp(writer io.Writer, settings REPLSettings) {
	for _, command := range slashCommands {
		fmt.Fprintf(
			writer,
			"%s  %s\n",
			paint(settings.Color, cyan, command.name),
			paint(settings.Color, dim, command.help),
		)
	}
	fmt.Fprintf(
		writer,
		"%s  %s\n",
		paint(settings.Color, cyan, "Shift+Tab"),
		paint(settings.Color, dim, "Toggle plan mode (read-only design) and agent mode"),
	)
}

func togglePlan(settings *REPLSettings) {
	if settings == nil {
		return
	}
	next := !settings.Plan
	if settings.SetPlan != nil {
		if err := settings.SetPlan(next); err != nil {
			return
		}
	}
	settings.Plan = next
}

func showUsage(ctx context.Context, writer io.Writer, settings REPLSettings) error {
	if settings.FetchUsage == nil {
		return errors.New("usage is unavailable")
	}
	report, err := settings.FetchUsage(ctx)
	if err != nil {
		return err
	}
	printUsage(writer, settings.Color, report)
	return nil
}

func applyModelCommand(writer io.Writer, settings *REPLSettings, line string) (bool, error) {
	command, err := parseModelCommand(line)
	if err != nil {
		return false, err
	}
	if command.Show {
		fmt.Fprintln(
			writer,
			paint(settings.Color, dim, formatModelStatus(
				settings.Model,
				orDefault(settings.Context, "272k"),
				orDefault(settings.Effort, "medium"),
				settings.Fast,
			)),
		)
		for _, model := range catalogModels(settings.Model) {
			marker := "  "
			if model == settings.Model {
				marker = "* "
			}
			fmt.Fprintln(writer, paint(settings.Color, dim, marker+model))
		}
		fmt.Fprintln(writer, paint(settings.Color, dim, "Usage: /model [id] [context=272k] [effort=medium] [fast=on|off]"))
		fmt.Fprintln(writer, paint(settings.Color, dim, "In a terminal, Tab opens context size, effort, and fast options."))
		return false, nil
	}

	previousModel := settings.Model
	if command.Model != "" {
		settings.Model = command.Model
	}
	if command.Context != "" {
		settings.Context = command.Context
	}
	if command.Effort != "" {
		settings.Effort = command.Effort
	}
	if command.Fast != nil {
		settings.Fast = *command.Fast
	}
	if settings.SyncSession != nil {
		if err := settings.SyncSession(*settings); err != nil {
			return false, err
		}
	}
	fmt.Fprintln(
		writer,
		paint(settings.Color, dim, formatModelStatus(
			settings.Model,
			orDefault(settings.Context, "272k"),
			orDefault(settings.Effort, "medium"),
			settings.Fast,
		)),
	)
	return command.Model != "" && command.Model != previousModel, nil
}

func applyModeCommand(writer io.Writer, settings *REPLSettings, line string) error {
	command, err := parseModeCommand(line)
	if err != nil {
		return err
	}
	if command.Show {
		current := orDefault(settings.PermissionMode, config.PermissionAsk)
		fmt.Fprintln(writer, paintModeStatus(settings.Color, current))
		for _, mode := range config.PermissionModes {
			marker := "  "
			label := paintPermission(settings.Color, mode)
			if mode == current {
				marker = "* "
			}
			fmt.Fprintln(writer, marker+label+paint(settings.Color, dim, "  "+permissionHelp(mode)))
		}
		fmt.Fprintln(writer, paint(settings.Color, dim, "Usage: /mode [ask|auto-writes|auto]"))
		fmt.Fprintln(writer, paint(settings.Color, dim, "In a terminal, Tab opens the mode picker."))
		return nil
	}

	previous := settings.PermissionMode
	settings.PermissionMode = command.Mode
	if settings.SyncSession != nil {
		if err := settings.SyncSession(*settings); err != nil {
			settings.PermissionMode = previous
			return err
		}
	}
	fmt.Fprintln(writer, paintModeStatus(settings.Color, settings.PermissionMode))
	return nil
}

func paintModeStatus(color bool, mode string) string {
	status := formatModeStatus(mode)
	if mode == config.PermissionAuto {
		return paint(color, bold+red, status)
	}
	return paint(color, dim, status)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func configureAPIKey(
	ctx context.Context,
	loop *agent.Loop,
	writer io.Writer,
	settings REPLSettings,
) error {
	if settings.ReadAPIKey == nil || settings.SaveAPIKey == nil {
		return errors.New("interactive API key configuration is unavailable")
	}
	if _, err := fmt.Fprint(writer, "OpenAI API key (hidden; blank cancels): "); err != nil {
		return err
	}
	apiKey, err := settings.ReadAPIKey(ctx)
	_, _ = fmt.Fprintln(writer)
	if err != nil {
		return fmt.Errorf("read API key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		fmt.Fprintln(writer, "Configuration unchanged.")
		return nil
	}
	if len(apiKey) > 16*1024 {
		return errors.New("API key is too large")
	}
	path, err := settings.SaveAPIKey(apiKey)
	if err != nil {
		return fmt.Errorf("save API key: %w", err)
	}
	loop.Reset()
	fmt.Fprintf(writer, "API key saved to %s. Conversation cleared.\n", path)
	return nil
}

func readLine(ctx context.Context, reader *bufio.Reader, file *os.File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if file != nil {
		stop := context.AfterFunc(ctx, func() {
			_ = file.SetReadDeadline(time.Now())
		})
		defer stop()
		defer func() { _ = file.SetReadDeadline(time.Time{}) }()
		line, err := reader.ReadString('\n')
		if err != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}
		return line, err
	}

	type result struct {
		line string
		err  error
	}
	read := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		read <- result{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-read:
		return value.line, value.err
	}
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(value, "\n")
	if len(line) > 180 {
		line = line[:180] + "…"
	}
	return safeTerminalText(line)
}

func safeTerminalText(value string) string {
	var output strings.Builder
	for _, character := range value {
		switch character {
		case '\n', '\t':
			output.WriteRune(character)
		case '\r':
			output.WriteString(`\r`)
		default:
			if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
				if character <= 0xffff {
					fmt.Fprintf(&output, `\u%04x`, character)
				} else {
					fmt.Fprintf(&output, `\U%08x`, character)
				}
			} else {
				output.WriteRune(character)
			}
		}
	}
	return output.String()
}
