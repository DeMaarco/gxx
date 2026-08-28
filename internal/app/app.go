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

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/term"

	"gxx/internal/agent"
	"gxx/internal/approval"
	"gxx/internal/config"
	openaiProvider "gxx/internal/openai"
	"gxx/internal/tools"
	"gxx/internal/ui"
	"gxx/internal/workspace"
)

var Version = "0.0.7"

type runtime struct {
	config    config.Config
	loop      *agent.Loop
	reader    *bufio.Reader
	renderer  *ui.Renderer
	workspace *workspace.Workspace
	provider  *openaiProvider.Provider
	policy    *approval.Policy
	registry  *tools.Registry
}

type jsonResult struct {
	Answer      string             `json:"answer"`
	Model       string             `json:"model"`
	Steps       int                `json:"steps"`
	Usage       agent.Usage        `json:"usage"`
	ToolResults []agent.ToolResult `json:"tool_results,omitempty"`
	Error       string             `json:"error,omitempty"`
}

// Run executes the CLI and returns a process exit code.
func Run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	interactive bool,
) int {
	if len(args) > 0 {
		switch args[0] {
		case "ask":
			return runAsk(ctx, args[1:], stdin, stdout, stderr, interactive)
		case "usage":
			return runUsage(ctx, args[1:], stdin, stdout, stderr)
		case "help":
			if len(args) > 1 && args[1] == "ask" {
				printAskUsage(stdout)
			} else if len(args) > 1 && args[1] == "usage" {
				fmt.Fprintln(stdout, "Usage:")
				fmt.Fprintln(stdout, "  gxx usage")
			} else {
				printRootUsage(stdout)
			}
			return 0
		case "version", "--version":
			fmt.Fprintf(stdout, "gxx %s\n", ui.FormatVersion(Version))
			return 0
		case "-h", "--help":
			printRootUsage(stdout)
			return 0
		}
	}
	return runInteractive(ctx, args, stdin, stdout, stderr, interactive)
}

func runInteractive(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	interactive bool,
) int {
	settings, help, err := parseFlags("gxx", args, stderr, false)
	if help {
		printRootUsage(stdout)
		return 0
	}
	if err != nil {
		return 2
	}
	if !interactive {
		fmt.Fprintln(stderr, "gxx: interactive mode requires a terminal; use `gxx ask` for piped input")
		return 2
	}

	rt, err := newRuntimeFromConfig(settings.config, stdin, stdout, stderr, interactive, false)
	if err != nil {
		fmt.Fprintf(stderr, "gxx: %v\n", err)
		return 2
	}
	defer rt.workspace.Close()
	err = ui.RunREPL(
		ctx,
		rt.loop,
		rt.reader,
		rt.renderer,
		stdout,
		replSettings(rt, stdin, stdout),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return 130
		}
		fmt.Fprintf(stderr, "gxx: %v\n", err)
		return 1
	}
	return 0
}

func runAsk(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	interactive bool,
) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	jsonRequested := wantsJSON(args)
	settings, help, err := parseFlags("gxx ask", args, stderr, true)
	if help {
		printAskUsage(stdout)
		return 0
	}
	if err != nil {
		if jsonRequested {
			writeJSONError(stdout, settings.config.Model, err)
		}
		return 2
	}

	prompt, err := askPrompt(settings.remaining, stdin, interactive)
	if err != nil {
		if settings.json {
			writeJSONError(stdout, settings.config.Model, err)
		} else {
			fmt.Fprintf(stderr, "gxx ask: %v\n", err)
		}
		return 2
	}

	rt, err := newRuntimeFromConfig(settings.config, stdin, stdout, stderr, interactive, true)
	if err != nil {
		if settings.json {
			writeJSONError(stdout, settings.config.Model, err)
		} else {
			fmt.Fprintf(stderr, "gxx ask: %v\n", err)
		}
		return 2
	}
	defer rt.workspace.Close()

	var emit agent.EmitFunc
	if !settings.json {
		_ = ui.WriteAskHeader(stdout, replSettings(rt, stdin, stdout))
		rt.renderer.StartTurn()
		emit = rt.renderer.Event
	}
	result, runErr := rt.loop.Run(ctx, prompt, emit)

	if settings.json {
		output := jsonResult{
			Answer:      result.Answer,
			Model:       rt.config.Model,
			Steps:       result.Steps,
			Usage:       result.Usage,
			ToolResults: result.ToolResults,
		}
		if runErr != nil {
			output.Error = runErr.Error()
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(output); err != nil {
			fmt.Fprintf(stderr, "gxx ask: encode JSON: %v\n", err)
			return 1
		}
	} else {
		rt.renderer.Finish(result.Answer)
	}

	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
			return 130
		}
		if !settings.json {
			fmt.Fprintf(stderr, "gxx ask: %v\n", runErr)
		}
		return 1
	}
	return 0
}

func runUsage(
	ctx context.Context,
	args []string,
	_ io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			fmt.Fprintln(stdout, "Usage:")
			fmt.Fprintln(stdout, "  gxx usage")
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Show session token usage, organization spend for the current month,")
			fmt.Fprintln(stdout, "remaining spend quota, and remaining rate-limit quota.")
			return 0
		}
		fmt.Fprintf(stderr, "gxx usage: unexpected argument %q\n", args[0])
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "gxx usage: %v\n", err)
		return 2
	}
	settings := config.Load(cwd)
	if strings.TrimSpace(settings.APIKey) == "" {
		fmt.Fprintln(stderr, "gxx usage: OPENAI_API_KEY is not set")
		return 2
	}

	provider := openaiProvider.New(settings.APIKey, settings.Model, "", settings.APITimeout)
	fmt.Fprint(stdout, ui.FormatUsage(provider.Report(ctx), ui.ColorEnabled(stdout)))
	return 0
}

type parsedFlags struct {
	config    config.Config
	json      bool
	remaining []string
}

func parseFlags(
	name string,
	args []string,
	stderr io.Writer,
	allowJSON bool,
) (parsedFlags, bool, error) {
	var parsed parsedFlags
	cwd, err := os.Getwd()
	if err != nil {
		return parsed, false, fmt.Errorf("get current directory: %w", err)
	}
	parsed.config = config.Load(cwd)

	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {}
	flags.StringVar(&parsed.config.Model, "model", parsed.config.Model, "OpenAI model")
	flags.IntVar(&parsed.config.MaxSteps, "max-steps", parsed.config.MaxSteps, "maximum model steps")
	flags.DurationVar(
		&parsed.config.CommandTimeout,
		"command-timeout",
		parsed.config.CommandTimeout,
		"maximum command duration",
	)
	flags.DurationVar(
		&parsed.config.APITimeout,
		"api-timeout",
		parsed.config.APITimeout,
		"timeout for each OpenAI response",
	)
	flags.StringVar(&parsed.config.Effort, "effort", parsed.config.Effort, "reasoning effort")
	flags.StringVar(&parsed.config.Context, "context", parsed.config.Context, "context window size")
	flags.StringVar(&parsed.config.PermissionMode, "permission", parsed.config.PermissionMode, "permission mode")
	flags.BoolVar(&parsed.config.Fast, "fast", parsed.config.Fast, "use OpenAI fast service tier")
	if allowJSON {
		flags.BoolVar(&parsed.json, "json", false, "emit one JSON result")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return parsed, true, nil
		}
		fmt.Fprintf(stderr, "Try `%s --help` for usage.\n", name)
		return parsed, false, err
	}
	parsed.remaining = flags.Args()
	if !allowJSON && len(parsed.remaining) > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name, parsed.remaining[0])
		return parsed, false, errors.New("unexpected positional argument")
	}
	return parsed, false, nil
}

func newRuntimeFromConfig(
	settings config.Config,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	interactive bool,
	requireAPIKey bool,
) (*runtime, error) {
	var validationError error
	if requireAPIKey {
		validationError = settings.Validate()
	} else {
		validationError = settings.ValidateInteractive()
	}
	if validationError != nil {
		return nil, validationError
	}
	ws, err := workspace.New(settings.Workspace)
	if err != nil {
		return nil, err
	}
	settings.Workspace = ws.Root()

	reader := bufio.NewReader(stdin)
	prompt := approval.NewPrompt(reader, stderr, interactive)
	if file := terminalFile(stdin); file != nil {
		prompt.SetFile(file)
	}
	policy := approval.NewPolicy(settings.PermissionMode, prompt)
	registry := tools.NewRegistry(ws, policy, tools.Options{
		MaxResultBytes:  settings.MaxToolResultBytes,
		MaxSearchResult: settings.MaxSearchResults,
		ParallelReads:   settings.ParallelReads,
		CommandTimeout:  settings.CommandTimeout,
	})
	model := openaiProvider.New(
		settings.APIKey,
		settings.Model,
		agent.SystemPrompt(ws, false),
		settings.APITimeout,
	)
	model.SetEffort(settings.Effort)
	model.SetContext(settings.Context)
	model.SetFast(settings.Fast)
	loop := &agent.Loop{
		Model:    model,
		Executor: registry,
		MaxSteps: settings.MaxSteps,
	}
	return &runtime{
		config:    settings,
		loop:      loop,
		reader:    reader,
		renderer:  ui.NewRendererWithColor(stdout, ui.ColorEnabled(stdout)),
		workspace: ws,
		provider:  model,
		policy:    policy,
		registry:  registry,
	}, nil
}

func replSettings(rt *runtime, stdin io.Reader, stdout io.Writer) ui.REPLSettings {
	return ui.REPLSettings{
		Version:          Version,
		Model:            rt.config.Model,
		PermissionMode:   rt.config.PermissionMode,
		Effort:           rt.config.Effort,
		Context:          rt.config.Context,
		Fast:             rt.config.Fast,
		Workspace:        rt.config.Workspace,
		Color:            ui.ColorEnabled(stdout),
		Stdin:            terminalFile(stdin),
		APIKeyConfigured: rt.config.APIKey != "",
		ReadAPIKey:       terminalAPIKeyReader(stdin, rt.reader),
		SaveAPIKey:       rt.saveAPIKey,
		FetchUsage: func(ctx context.Context) (agent.UsageReport, error) {
			return rt.provider.Report(ctx), nil
		},
		FetchContext: func() agent.ContextUsage {
			return rt.provider.ContextSnapshot()
		},
		SetPlan: func(plan bool) error {
			rt.provider.SetInstructions(agent.SystemPrompt(rt.workspace, plan))
			if rt.registry != nil {
				rt.registry.SetPlan(plan)
			}
			return nil
		},
		SyncSession: func(session ui.REPLSettings) error {
			if _, err := config.SaveSession(
				session.Model,
				session.Effort,
				session.Context,
				session.Fast,
				session.PermissionMode,
			); err != nil {
				return err
			}
			rt.provider.SetModel(session.Model)
			rt.provider.SetEffort(session.Effort)
			rt.provider.SetContext(session.Context)
			rt.provider.SetFast(session.Fast)
			if rt.policy != nil && strings.TrimSpace(session.PermissionMode) != "" {
				if err := rt.policy.SetMode(session.PermissionMode); err != nil {
					return err
				}
				rt.config.PermissionMode = rt.policy.Mode()
			}
			rt.config.Model = session.Model
			rt.config.Effort = session.Effort
			rt.config.Context = session.Context
			rt.config.Fast = session.Fast
			return nil
		},
	}
}

func terminalFile(reader io.Reader) *os.File {
	file, ok := reader.(*os.File)
	if !ok {
		return nil
	}
	if !term.IsTerminal(int(file.Fd())) {
		return nil
	}
	return file
}

func (rt *runtime) saveAPIKey(apiKey string) (string, error) {
	path, err := config.SaveAPIKey(apiKey)
	if err != nil {
		return "", err
	}
	if err := rt.provider.SetAPIKey(apiKey); err != nil {
		return "", err
	}
	rt.config.APIKey = apiKey
	return path, nil
}

func terminalAPIKeyReader(stdin io.Reader, reader *bufio.Reader) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		file, ok := stdin.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			return "", errors.New("API key input requires a terminal")
		}
		if buffered := reader.Buffered(); buffered > 0 {
			if _, err := reader.Discard(buffered); err != nil {
				return "", fmt.Errorf("discard stale terminal input: %w", err)
			}
		}
		value, err := term.ReadPassword(int(file.Fd()))
		if err != nil {
			return "", err
		}
		return string(value), nil
	}
}

func askPrompt(arguments []string, stdin io.Reader, interactive bool) (string, error) {
	if len(arguments) > 0 {
		prompt := strings.TrimSpace(strings.Join(arguments, " "))
		if prompt == "" {
			return "", errors.New("prompt cannot be empty")
		}
		return prompt, nil
	}
	if interactive {
		return "", errors.New("provide a prompt argument or pipe one on stdin")
	}

	data, err := io.ReadAll(io.LimitReader(stdin, maxPromptInputBytes+1))
	if err != nil {
		return "", fmt.Errorf("read prompt: %w", err)
	}
	if len(data) > maxPromptInputBytes {
		return "", fmt.Errorf("prompt exceeds %d bytes", maxPromptInputBytes)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", errors.New("prompt cannot be empty")
	}
	return prompt, nil
}

const maxPromptInputBytes = 1024 * 1024

func printRootUsage(writer io.Writer) {
	fmt.Fprintln(writer, "gxx — small OpenAI coding agent")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  gxx [flags]                  Start the interactive REPL")
	fmt.Fprintln(writer, "  gxx ask [flags] <prompt>     Run one request")
	fmt.Fprintln(writer, "  gxx usage                    Show API usage and remaining quota")
	fmt.Fprintln(writer, "  gxx help                     Show help")
	fmt.Fprintln(writer, "  gxx version                  Show version")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Flags:")
	printCommonFlags(writer)
}

func printAskUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  gxx ask [flags] <prompt>")
	fmt.Fprintln(writer, "  printf 'prompt' | gxx ask [flags]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Flags:")
	printCommonFlags(writer)
	fmt.Fprintln(writer, "  --json                  Emit one JSON result and suppress streaming")
}

func printCommonFlags(writer io.Writer) {
	fmt.Fprintln(writer, "  --model string          OpenAI model (default: GXX_MODEL, config.json, or gpt-5.6-sol)")
	fmt.Fprintln(writer, "  --effort string         Reasoning effort (default: GXX_EFFORT, config.json, or medium)")
	fmt.Fprintln(writer, "  --context string        Context window size (default: GXX_CONTEXT, config.json, or 272k)")
	fmt.Fprintln(writer, "  --permission string     Permission mode (default: GXX_PERMISSION, config.json, or ask)")
	fmt.Fprintln(writer, "  --fast                  Use OpenAI fast service tier")
	fmt.Fprintln(writer, "  --max-steps int         Maximum model steps (default: 12)")
	fmt.Fprintln(writer, "  --command-timeout dur   Maximum command duration (default: 2m)")
	fmt.Fprintln(writer, "  --api-timeout dur       Timeout per OpenAI response (default: 10m)")
}

func wantsJSON(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--json" || argument == "-json" {
			return true
		}
		for _, prefix := range []string{"--json=", "-json="} {
			if strings.HasPrefix(argument, prefix) {
				return strings.TrimPrefix(argument, prefix) == "true"
			}
		}
	}
	return false
}

func writeJSONError(writer io.Writer, model string, err error) {
	_ = json.NewEncoder(writer).Encode(jsonResult{
		Model: model,
		Error: err.Error(),
	})
}
