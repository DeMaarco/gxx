package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	Workspace        string
	Color            bool
	Stdin            *os.File
	APIKeyConfigured bool
	ReadAPIKey       func(context.Context) (string, error)
	SaveAPIKey       func(string) (string, error)
	SyncSession      func(REPLSettings) error
}

type Renderer struct {
	writer    io.Writer
	color     bool
	live      bool
	spinEvery time.Duration
	now       func() time.Time

	mu         sync.Mutex
	sawText    bool
	textOpen   bool
	liveOpen   bool
	cursorHide bool
	thinking   bool
	thinkStart time.Time
	frame      int
	running    []runningTool

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
	r.frame = 0
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
		r.thinking = false
		text := safeTerminalText(event.Text)
		_, _ = io.WriteString(r.writer, text)
		r.sawText = true
		r.textOpen = !strings.HasSuffix(text, "\n")
	case agent.EventToolCall:
		r.clearLiveLocked()
		r.thinking = false
	case agent.EventToolStarted:
		r.clearLiveLocked()
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
		r.removeRunning(event.Result)
		r.writeToolDoneLocked(event.Result)
		if r.live && len(r.running) == 0 {
			r.thinking = true
			r.thinkStart = r.timestamp()
		}
	case agent.EventNotice:
		r.clearLiveLocked()
		r.endTextLine()
		if event.Text != "" {
			_, _ = fmt.Fprintf(r.writer, "%s\n", paint(r.color, dim, safeTerminalText(event.Text)))
		}
	}
	if r.shouldSpinLocked() {
		r.drawLiveLocked()
		return true
	}
	return false
}

func (r *Renderer) writeToolDoneLocked(result *agent.ToolResult) {
	if result == nil {
		return
	}
	name := safeTerminalText(result.Name)
	if result.IsError {
		_, _ = fmt.Fprintf(
			r.writer,
			"%s %s (%dms): %s\n",
			paint(r.color, red, "✗"),
			paint(r.color, red, name),
			result.DurationMS,
			paint(r.color, dim, firstLine(result.Output)),
		)
		return
	}
	suffix := ""
	if result.Truncated {
		suffix = ", truncated"
	}
	_, _ = fmt.Fprintf(
		r.writer,
		"%s %s %s\n",
		paint(r.color, green, "✓"),
		paint(r.color, green, name),
		paint(r.color, dim, fmt.Sprintf("(%dms%s)", result.DurationMS, suffix)),
	)
}

func (r *Renderer) Finish(answer string) {
	r.stopAnim()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearLiveLocked()
	r.showCursorLocked()
	r.thinking = false
	r.running = nil
	if !r.sawText && strings.TrimSpace(answer) != "" {
		answer = safeTerminalText(answer)
		_, _ = io.WriteString(r.writer, answer)
		r.textOpen = !strings.HasSuffix(answer, "\n")
	}
	r.endTextLine()
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

	var editor *lineEditor
	if lineEditorEnabled(settings.Stdin, writer) {
		editor = &lineEditor{in: settings.Stdin, out: writer, color: settings.Color}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var line string
		var err error
		if editor != nil {
			if err := writeHeader(writer, settings); err != nil {
				return err
			}
			line, err = editor.Read(ctx, settings)
		} else {
			if err := writeChrome(writer, settings); err != nil {
				return err
			}
			line, err = readLine(ctx, reader)
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
		switch {
		case prompt == "/exit" || prompt == "/quit":
			return nil
		case prompt == "/clear":
			loop.Reset()
			fmt.Fprintln(writer, paint(settings.Color, dim, "Conversation cleared."))
			fmt.Fprintln(writer)
			continue
		case prompt == "/config":
			if err := configureAPIKey(ctx, loop, writer, settings); err != nil {
				fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
			}
			fmt.Fprintln(writer)
			continue
		case prompt == "/help":
			printREPLHelp(writer, settings)
			fmt.Fprintln(writer)
			continue
		case strings.HasPrefix(prompt, "/model"):
			changedModel, err := applyModelCommand(writer, &settings, prompt)
			if err != nil {
				fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
			} else if changedModel {
				loop.Reset()
				fmt.Fprintln(writer, paint(settings.Color, dim, "Conversation cleared."))
			}
			fmt.Fprintln(writer)
			continue
		case strings.HasPrefix(prompt, "/mode"):
			if err := applyModeCommand(writer, &settings, prompt); err != nil {
				fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
			}
			fmt.Fprintln(writer)
			continue
		}

		renderer.StartTurn()
		result, err := loop.Run(ctx, prompt, renderer.Event)
		renderer.Finish(result.Answer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(writer, "%s\n", paint(settings.Color, red, "error: "+err.Error()))
		}
		fmt.Fprintln(writer)
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

func readLine(ctx context.Context, reader *bufio.Reader) (string, error) {
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
