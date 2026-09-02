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
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"

	"gxx/internal/agent"
	anthropicProvider "gxx/internal/anthropic"
	"gxx/internal/approval"
	"gxx/internal/auth"
	"gxx/internal/auth/claude"
	openaiAuth "gxx/internal/auth/openai"
	"gxx/internal/config"
	"gxx/internal/conversations"
	"gxx/internal/models"
	openaiProvider "gxx/internal/openai"
	"gxx/internal/pricing"
	"gxx/internal/tools"
	"gxx/internal/ui"
	"gxx/internal/workspace"
)

var Version = "0.0.19"

type runtime struct {
	config    config.Config
	loop      *agent.Loop
	reader    *bufio.Reader
	renderer  *ui.Renderer
	workspace *workspace.Workspace
	provider  agent.Backend
	policy    *approval.Policy
	registry  *tools.Registry
	eco       int

	conversationStore    *conversations.Store
	activeConversationID string

	modelsMu      sync.Mutex
	modelsGen     atomic.Uint64
	modelsAccount string
	modelsList    []string
	modelsLive    bool
}

type jsonResult struct {
	Answer      string             `json:"answer"`
	Model       string             `json:"model"`
	Steps       int                `json:"steps"`
	Usage       agent.Usage        `json:"usage"`
	ToolResults []agent.ToolResult `json:"tool_results,omitempty"`
	CostUSD     *float64           `json:"cost_usd,omitempty"`
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
		case "login":
			return runLogin(ctx, args[1:], stdin, stdout, stderr)
		case "logout":
			return runLogout(args[1:], stdin, stdout, stderr)
		case "help":
			if len(args) > 1 && args[1] == "ask" {
				printAskUsage(stdout)
			} else if len(args) > 1 && args[1] == "usage" {
				fmt.Fprintln(stdout, "Usage:")
				fmt.Fprintln(stdout, "  gxx usage")
			} else if len(args) > 1 && args[1] == "login" {
				printLoginUsage(stdout)
			} else if len(args) > 1 && args[1] == "logout" {
				printLogoutUsage(stdout)
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
	startSession(rt, false)
	session := replSettings(rt, stdin, stdout)
	rt.startModelRefresh()
	err = ui.RunREPL(
		ctx,
		rt.loop,
		rt.reader,
		rt.renderer,
		stdout,
		session,
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

	settings.config.PermissionMode = constrainPipedPermission(
		interactive,
		settings.permissionFlag,
		settings.config.PermissionMode,
	)

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
	startSession(rt, !settings.permissionFlag)

	var emit agent.EmitFunc
	if !settings.json {
		_ = ui.WriteAskHeader(stdout, replSettings(rt, stdin, stdout))
		rt.renderer.SetQuote(func(usage agent.Usage) (float64, bool) {
			return quoteCost(rt, usage)
		})
		rt.renderer.StartTurn()
		emit = rt.renderer.Event
	}
	result, runErr := rt.loop.Run(ctx, prompt, emit)
	refreshPricing(ctx)

	if settings.json {
		output := jsonResult{
			Answer:      result.Answer,
			Model:       rt.config.Model,
			Steps:       result.Steps,
			Usage:       result.Usage,
			ToolResults: result.ToolResults,
		}
		if cost, ok := quoteCost(rt, result.Usage); ok {
			output.CostUSD = &cost
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
			fmt.Fprintln(stdout, "Show session token usage, estimated USD cost, organization spend")
			fmt.Fprintln(stdout, "for the current month, remaining spend quota, and remaining rate-limit quota.")
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
	if err := settings.Validate(); err != nil {
		fmt.Fprintf(stderr, "gxx usage: %v\n", err)
		return 2
	}

	provider, err := newBackend(settings, nil)
	if err != nil {
		fmt.Fprintf(stderr, "gxx usage: %v\n", err)
		return 2
	}
	refreshPricing(ctx)
	report := provider.Report(ctx)
	if cost, ok := pricing.Default().Estimate(pricing.Query{
		Model: settings.Model,
		Fast:  settings.Fast,
		Usage: report.Session,
	}); ok {
		report.SessionCostUSD = cost
		report.HasSessionCost = true
	}
	fmt.Fprint(stdout, ui.FormatUsage(report, ui.ColorEnabled(stdout)))
	return 0
}

func runLogin(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) int {
	command, err := auth.ParseCommand(args)
	if err != nil {
		fmt.Fprintf(stderr, "gxx login: %v\n", err)
		return 2
	}
	if command.Help {
		printLoginUsage(stdout)
		return 0
	}
	cwd, _ := os.Getwd()
	settings := config.Load(cwd)
	provider, err := resolveProviderChoice(command.Provider, stdin, stdout, settings.ActiveAccount(), "gxx login")
	if err != nil {
		if auth.IsCanceled(err) {
			fmt.Fprintln(stdout, "Login canceled.")
			return 0
		}
		fmt.Fprintf(stderr, "gxx login: %v\n", err)
		return 2
	}

	var path string
	switch provider {
	case auth.ProviderAPI:
		path, err = loginAPIKey(ctx, stdin, stdout)
	case auth.ProviderOpenAI:
		device := command.Device || openaiAuth.PreferDevice()
		_, path, err = openaiAuth.Login(ctx, openaiAuth.NewClient(), stdout, device)
	default:
		_, path, err = claude.Login(ctx, claude.NewClient(), stdout, claude.LineReader(stdin))
	}
	if err != nil {
		if auth.IsCanceled(err) || claude.IsCanceled(err) || openaiAuth.IsCanceled(err) {
			fmt.Fprintln(stdout, "Login canceled.")
			return 0
		}
		fmt.Fprintf(stderr, "gxx login: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s login saved to %s.\n", loginLabel(provider), path)
	return 0
}

func runLogout(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	command, err := auth.ParseCommand(args)
	if err != nil {
		fmt.Fprintf(stderr, "gxx logout: %v\n", err)
		return 2
	}
	if command.Help {
		printLogoutUsage(stdout)
		return 0
	}
	if command.Device {
		fmt.Fprintln(stderr, "gxx logout: unexpected flag --device")
		return 2
	}
	cwd, _ := os.Getwd()
	settings := config.Load(cwd)
	provider := command.Provider
	if provider == "" {
		provider = settings.ActiveAccount()
	}
	if provider == "" {
		fmt.Fprintln(stderr, "gxx logout: no account connected")
		return 2
	}

	var path string
	switch provider {
	case auth.ProviderAPI:
		path, err = config.ClearAPIKey()
	case auth.ProviderOpenAI:
		path, err = openaiAuth.Logout()
	default:
		path, err = claude.Logout()
	}
	if err != nil {
		fmt.Fprintf(stderr, "gxx logout: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s login cleared from %s.\n", loginLabel(provider), path)
	return 0
}

func resolveProviderChoice(
	provider string,
	stdin io.Reader,
	stdout io.Writer,
	active string,
	command string,
) (string, error) {
	if strings.TrimSpace(provider) != "" {
		return provider, nil
	}
	file := terminalFile(stdin)
	if file == nil {
		return "", fmt.Errorf("choose openai, claude, or api (for example `%s openai`)", command)
	}
	return ui.ReadLoginChoice(file, stdout, ui.ColorEnabled(stdout), active)
}

func loginLabel(provider string) string {
	switch provider {
	case auth.ProviderOpenAI:
		return "OpenAI"
	case auth.ProviderAPI:
		return "API"
	default:
		return "Claude"
	}
}

func loginAPIKey(ctx context.Context, stdin io.Reader, stdout io.Writer) (string, error) {
	if _, err := fmt.Fprint(stdout, "OpenAI API key (hidden; blank cancels): "); err != nil {
		return "", err
	}
	file, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return "", errors.New("API key input requires a terminal")
	}
	value, err := term.ReadPassword(int(file.Fd()))
	_, _ = fmt.Fprintln(stdout)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(value))
	if key == "" {
		return "", auth.ErrCanceled
	}
	return config.SaveAPIKey(key)
}

type parsedFlags struct {
	config         config.Config
	json           bool
	remaining      []string
	permissionFlag bool
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
	flags.StringVar(&parsed.config.Model, "model", parsed.config.Model, "model (OpenAI or Claude)")
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
		"timeout for each model response",
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
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "permission" {
			parsed.permissionFlag = true
		}
	})
	parsed.remaining = flags.Args()
	if !allowJSON && len(parsed.remaining) > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name, parsed.remaining[0])
		return parsed, false, errors.New("unexpected positional argument")
	}
	return parsed, false, nil
}

func constrainPipedPermission(interactive, permissionFlag bool, mode string) string {
	if !interactive && !permissionFlag {
		return config.PermissionAsk
	}
	return mode
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
	promptOut := stderr
	if !requireAPIKey {
		promptOut = stdout
	}
	prompt := approval.NewPrompt(reader, promptOut, interactive)
	if file := terminalFile(stdin); file != nil {
		prompt.SetFile(file)
		color := ui.ColorEnabled(promptOut)
		prompt.SetChooser(func(ctx context.Context, action approval.Action) (approval.Decision, error) {
			return ui.ReadApprovalChoice(ctx, file, promptOut, color, action)
		})
	}
	policy := approval.NewPolicy(settings.PermissionMode, prompt)
	registry := tools.NewRegistry(ws, policy, tools.Options{
		MaxResultBytes:  settings.MaxToolResultBytes,
		MaxSearchResult: settings.MaxSearchResults,
		ParallelReads:   settings.ParallelReads,
		CommandTimeout:  settings.CommandTimeout,
	})
	model, err := newBackend(settings, ws)
	if err != nil {
		return nil, err
	}
	loop := &agent.Loop{
		Model:    model,
		Executor: registry,
		MaxSteps: settings.MaxSteps,
		Overview: registry.WorkspaceOverview,
	}
	renderer := ui.NewRendererWithColor(stdout, ui.ColorEnabled(stdout))
	prompt.SetHold(func() func() {
		renderer.HoldForPrompt()
		return renderer.ResumeAfterPrompt
	})
	rt := &runtime{
		config:    settings,
		loop:      loop,
		reader:    reader,
		renderer:  renderer,
		workspace: ws,
		provider:  model,
		policy:    policy,
		registry:  registry,
	}
	loop.ProjectContext = func() string {
		return agent.ProjectContext(ws, rt.eco)
	}
	if store, err := conversations.NewStore(); err == nil {
		rt.conversationStore = store
	}
	registry.SetGenerateImage(rt.generateImage)
	return rt, nil
}

func (rt *runtime) generateImage(ctx context.Context, req tools.ImageRequest) (tools.ImageResult, error) {
	key := strings.TrimSpace(rt.config.APIKey)
	if key == "" {
		return tools.ImageResult{}, errors.New("image generation needs an OpenAI platform API key (not ChatGPT login); run /config or export OPENAI_API_KEY")
	}
	result, err := openaiProvider.GenerateImage(ctx, key, openaiProvider.ImageRequest{
		Prompt:     req.Prompt,
		Model:      req.Model,
		Size:       req.Size,
		Quality:    req.Quality,
		Format:     req.Format,
		Background: req.Background,
	})
	if err != nil {
		return tools.ImageResult{}, err
	}
	return tools.ImageResult{
		Data:    result.Data,
		Model:   result.Model,
		Size:    result.Size,
		Quality: result.Quality,
		Format:  result.Format,
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
		Ask:              rt.registry != nil && rt.registry.Ask(),
		Plan:             rt.registry != nil && rt.registry.Plan(),
		Workspace:        rt.config.Workspace,
		Color:            ui.ColorEnabled(stdout),
		Stdin:            terminalFile(stdin),
		APIKeyConfigured: rt.config.HasOpenAIAPIKey(),
		OpenAIConfigured: rt.config.HasOpenAICredentials(),
		ClaudeConfigured: rt.config.HasClaudeCredentials(),
		ActiveAccount:    rt.config.ActiveAccount(),
		Models:           bundledAccountModels(rt.config),
		ReadAPIKey:       terminalAPIKeyReader(stdin, rt.reader),
		SaveAPIKey:       rt.saveAPIKey,
		Login:            rt.login,
		Logout:           rt.logout,
		RefreshAuth:      rt.refreshAuth,
		RefreshModels:    rt.applyModels,
		FetchUsage: func(ctx context.Context) (agent.UsageReport, error) {
			return rt.provider.Report(ctx), nil
		},
		RefreshPricing: refreshPricing,
		QuoteCost: func(usage agent.Usage) (float64, bool) {
			return quoteCost(rt, usage)
		},
		FetchContext: func() agent.ContextUsage {
			return rt.provider.ContextSnapshot()
		},
		RefreshInstructions: func() {
			rt.provider.SetInstructions(rt.systemPrompt())
		},
		SetAsk: func(ask bool) error {
			if rt.registry != nil {
				rt.registry.SetAsk(ask)
			}
			rt.provider.SetInstructions(rt.systemPrompt())
			return nil
		},
		SetPlan: func(plan bool) error {
			if rt.registry != nil {
				rt.registry.SetPlan(plan)
			}
			rt.provider.SetInstructions(rt.systemPrompt())
			return nil
		},
		SetEco: func(level int) error {
			rt.eco = level
			return applyEcoRuntime(rt)
		},
		Compact: func(ctx context.Context, focus string) error {
			return rt.provider.Compact(ctx, nil, focus)
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
			if rt.policy != nil && strings.TrimSpace(session.PermissionMode) != "" {
				if err := rt.policy.SetMode(session.PermissionMode); err != nil {
					return err
				}
				rt.config.PermissionMode = rt.policy.Mode()
			}
			previousProvider := rt.config.Provider
			rt.config.Model = session.Model
			rt.config.Effort = session.Effort
			rt.config.Context = session.Context
			rt.config.Fast = session.Fast
			rt.config.Provider = config.ProviderForModel(session.Model)
			if rt.config.Provider != previousProvider {
				if err := rt.swapBackend(); err != nil {
					return err
				}
			}
			return applyEcoRuntime(rt)
		},
		ListConversations: func() ([]ui.ConversationEntry, error) {
			return rt.listConversationEntries()
		},
		LoadConversation: func(id string) error {
			return rt.loadConversation(id)
		},
		SaveConversation: func() error {
			return rt.saveConversation()
		},
		ArchiveAndClear: func() error {
			return rt.archiveAndClear()
		},
		RefreshSession: func(session *ui.REPLSettings) {
			rt.refreshREPLSession(session)
		},
		ChooseConversation: func(ctx context.Context, writer io.Writer) (string, error) {
			entries, err := rt.listConversationEntries()
			if err != nil {
				return "", err
			}
			file := terminalFile(stdin)
			if file == nil {
				return "", fmt.Errorf("conversation menu requires a terminal")
			}
			return ui.ReadConversationChoice(ctx, file, writer, entries, ui.ColorEnabled(stdout))
		},
	}
}

func applyEcoRuntime(rt *runtime) error {
	if rt == nil || rt.provider == nil {
		return nil
	}
	rt.provider.SetModel(rt.config.Model)
	rt.provider.SetEffort(rt.config.Effort)
	rt.provider.SetContext(rt.config.Context)
	rt.provider.SetFast(rt.config.Fast)
	state := config.ApplyEco(rt.config, rt.eco)
	rt.provider.SetTokenBudget(
		state.Level,
		state.CompactNumer,
		state.CompactDenom,
		state.ToolOutputKeep,
		state.ToolOutputClip,
		state.IncludeReasoning,
	)
	if rt.registry != nil {
		rt.registry.SetMaxResultBytes(state.MaxToolResultBytes)
	}
	if rt.workspace != nil {
		rt.provider.SetInstructions(rt.systemPrompt())
	}
	return nil
}

// startSession selects the exclusive session mode. The REPL starts in agent so
// /mode auto and auto-writes actually apply. gxx ask starts read-only unless
// --permission was passed.
func startSession(rt *runtime, ask bool) {
	if rt == nil {
		return
	}
	if rt.registry != nil {
		rt.registry.SetPlan(false)
		rt.registry.SetAsk(ask)
	}
	if rt.provider != nil {
		rt.provider.SetInstructions(rt.systemPrompt())
	}
}

func (rt *runtime) systemPrompt() string {
	plan := false
	ask := false
	ws := (*workspace.Workspace)(nil)
	eco := 0
	if rt != nil {
		ws = rt.workspace
		eco = rt.eco
		if rt.registry != nil {
			plan = rt.registry.Plan()
			ask = rt.registry.Ask()
		}
	}
	return agent.SystemPromptWithOptions(ws, plan, ask, eco)
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

func newBackend(settings config.Config, ws *workspace.Workspace) (agent.Backend, error) {
	instructions := ""
	if ws != nil {
		instructions = agent.SystemPromptWithOptions(ws, false, false, 0)
	}
	switch config.ProviderForModel(settings.Model) {
	case config.ProviderAnthropic:
		provider := anthropicProvider.New(
			claude.NewSource(nil),
			settings.Model,
			instructions,
			settings.APITimeout,
		)
		provider.SetEffort(settings.Effort)
		provider.SetContext(settings.Context)
		provider.SetFast(settings.Fast)
		return provider, nil
	default:
		var provider *openaiProvider.Provider
		if settings.HasOpenAIAPIKey() {
			provider = openaiProvider.New(
				settings.APIKey,
				settings.Model,
				instructions,
				settings.APITimeout,
			)
		} else if strings.TrimSpace(settings.OpenAITokens.AccessToken) != "" {
			provider = openaiProvider.NewWithSource(
				openaiAuth.NewSource(nil),
				settings.Model,
				instructions,
				settings.APITimeout,
			)
		} else {
			provider = openaiProvider.New(
				"",
				settings.Model,
				instructions,
				settings.APITimeout,
			)
		}
		provider.SetEffort(settings.Effort)
		provider.SetContext(settings.Context)
		provider.SetFast(settings.Fast)
		return provider, nil
	}
}

func (rt *runtime) swapBackend() error {
	backend, err := newBackend(rt.config, rt.workspace)
	if err != nil {
		return err
	}
	rt.provider = backend
	if rt.loop != nil {
		rt.loop.Model = backend
		rt.loop.Reset()
	}
	return nil
}

func (rt *runtime) saveAPIKey(apiKey string) (string, error) {
	path, err := config.SaveAPIKey(apiKey)
	if err != nil {
		return "", err
	}
	rt.config.APIKey = apiKey
	rt.config.OpenAITokens = config.OpenAITokens{}
	rt.config.ClaudeTokens = config.ClaudeTokens{}
	rt.applyAccountModel(config.AccountAPI)
	if err := rt.swapBackend(); err != nil {
		return "", err
	}
	return path, nil
}

func (rt *runtime) login(ctx context.Context, writer io.Writer, args []string) (string, error) {
	command, err := auth.ParseCommand(args)
	if err != nil {
		return "", err
	}
	provider := command.Provider
	if provider == "" {
		return "", fmt.Errorf("choose openai, claude, or api (for example `/login openai`)")
	}
	switch provider {
	case auth.ProviderAPI:
		return "", errors.New("API login is handled by /login api")
	case auth.ProviderOpenAI:
		device := command.Device || openaiAuth.PreferDevice()
		tokens, path, err := openaiAuth.Login(ctx, openaiAuth.NewClient(), writer, device)
		if err != nil {
			return "", err
		}
		rt.config.OpenAITokens = tokens
		rt.config.APIKey = ""
		rt.config.ClaudeTokens = config.ClaudeTokens{}
		rt.applyAccountModel(config.AccountOpenAI)
		if err := rt.swapBackend(); err != nil {
			return "", err
		}
		return path, nil
	default:
		tokens, path, err := claude.Login(ctx, claude.NewClient(), writer, func() (string, error) {
			line, err := rt.reader.ReadString('\n')
			return line, err
		})
		if err != nil {
			return "", err
		}
		rt.config.ClaudeTokens = tokens
		rt.config.APIKey = ""
		rt.config.OpenAITokens = config.OpenAITokens{}
		rt.applyAccountModel(config.AccountClaude)
		if err := rt.swapBackend(); err != nil {
			return "", err
		}
		return path, nil
	}
}

func (rt *runtime) logout(args []string) (string, error) {
	command, err := auth.ParseCommand(args)
	if err != nil {
		return "", err
	}
	provider := command.Provider
	if provider == "" {
		provider = rt.config.ActiveAccount()
	}
	if provider == "" {
		return "", errors.New("no account connected")
	}
	switch provider {
	case auth.ProviderAPI:
		path, err := config.ClearAPIKey()
		if err != nil {
			return "", err
		}
		rt.config.APIKey = ""
		_ = rt.swapBackend()
		return path, nil
	case auth.ProviderOpenAI:
		path, err := openaiAuth.Logout()
		if err != nil {
			return "", err
		}
		rt.config.OpenAITokens = config.OpenAITokens{}
		_ = rt.swapBackend()
		return path, nil
	default:
		path, err := claude.Logout()
		if err != nil {
			return "", err
		}
		rt.config.ClaudeTokens = config.ClaudeTokens{}
		_ = rt.swapBackend()
		return path, nil
	}
}

func (rt *runtime) refreshAuth(settings *ui.REPLSettings) {
	if rt == nil || settings == nil {
		return
	}
	loaded := config.Load(rt.config.Workspace)
	rt.config.APIKey = loaded.APIKey
	rt.config.OpenAITokens = loaded.OpenAITokens
	rt.config.ClaudeTokens = loaded.ClaudeTokens
	if strings.TrimSpace(loaded.Model) != "" {
		rt.config.Model = loaded.Model
	}
	settings.APIKeyConfigured = rt.config.HasOpenAIAPIKey()
	settings.OpenAIConfigured = rt.config.HasOpenAICredentials()
	settings.ClaudeConfigured = rt.config.HasClaudeCredentials()
	settings.ActiveAccount = rt.config.ActiveAccount()
	settings.Model = rt.config.Model
	rt.resetModelCache()
	settings.Models = bundledAccountModels(rt.config)
	rt.startModelRefresh()
}

func bundledAccountModels(settings config.Config) []string {
	return models.Catalog(settings.Model, settings.ActiveAccount(), nil)
}

func (rt *runtime) applyModels(settings *ui.REPLSettings) {
	if rt == nil || settings == nil {
		return
	}
	settings.Models = rt.accountModels()
}

func (rt *runtime) accountModels() []string {
	if rt == nil {
		return nil
	}
	account := rt.config.ActiveAccount()
	rt.modelsMu.Lock()
	defer rt.modelsMu.Unlock()
	if rt.modelsLive && rt.modelsAccount == account {
		return append([]string(nil), rt.modelsList...)
	}
	return bundledAccountModels(rt.config)
}

func (rt *runtime) resetModelCache() {
	if rt == nil {
		return
	}
	rt.modelsGen.Add(1)
	rt.modelsMu.Lock()
	rt.modelsLive = false
	rt.modelsList = nil
	rt.modelsAccount = ""
	rt.modelsMu.Unlock()
}

func (rt *runtime) startModelRefresh() {
	if rt == nil {
		return
	}
	snapshot := rt.config
	gen := rt.modelsGen.Add(1)
	go func() {
		listed := listAccountModels(context.Background(), snapshot)
		rt.modelsMu.Lock()
		defer rt.modelsMu.Unlock()
		if rt.modelsGen.Load() != gen {
			return
		}
		rt.modelsAccount = snapshot.ActiveAccount()
		rt.modelsList = listed
		rt.modelsLive = true
	}()
}

func refreshPricing(ctx context.Context) {
	timeout := 5 * time.Second
	var priceCtx context.Context
	var cancel context.CancelFunc
	if ctx != nil && ctx.Err() == nil {
		priceCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		priceCtx, cancel = context.WithTimeout(context.Background(), timeout)
	}
	defer cancel()
	_ = pricing.Default().RefreshIfStale(priceCtx, time.Minute)
}

func quoteCost(rt *runtime, usage agent.Usage) (float64, bool) {
	if rt == nil {
		return 0, false
	}
	return pricing.Default().Estimate(pricing.Query{
		Model: rt.config.Model,
		Fast:  rt.config.Fast,
		Usage: usage,
	})
}

func (rt *runtime) applyAccountModel(account string) {
	switch account {
	case config.AccountClaude:
		if !config.IsClaudeModel(rt.config.Model) {
			rt.config.Model = config.DefaultClaudeModel
		}
	default:
		if !config.IsOpenAIModel(rt.config.Model) {
			rt.config.Model = config.DefaultModel
		}
	}
	_, _ = config.SaveSession(rt.config.Model, rt.config.Effort, rt.config.Context, rt.config.Fast, rt.config.PermissionMode)
}

func listAccountModels(ctx context.Context, settings config.Config) []string {
	account := settings.ActiveAccount()
	live, err := fetchLiveModels(ctx, settings)
	if err != nil || len(live) == 0 {
		return models.Catalog(settings.Model, account, nil)
	}
	return models.Catalog(settings.Model, account, live)
}

func fetchLiveModels(ctx context.Context, settings config.Config) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	switch settings.ActiveAccount() {
	case config.AccountClaude:
		token, err := claude.NewSource(nil).AccessToken(ctx)
		if err != nil {
			return nil, err
		}
		return anthropicProvider.ListModels(ctx, nil, "", token)
	case config.AccountAPI:
		return openaiProvider.ListModels(ctx, nil, "", settings.APIKey)
	case config.AccountOpenAI:
		token, err := openaiAuth.NewSource(nil).AccessToken(ctx)
		if err != nil {
			return nil, err
		}
		ids, err := openaiProvider.ListModels(ctx, nil, openaiProvider.CodexAPIBaseURL(), token)
		if err != nil || len(ids) == 0 {
			return nil, err
		}
		return ids, nil
	default:
		return nil, nil
	}
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
	fmt.Fprintln(writer, "gxx — small coding agent for OpenAI and Claude")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  gxx [flags]                  Start the interactive REPL")
	fmt.Fprintln(writer, "  gxx ask [flags] <prompt>     Run one request")
	fmt.Fprintln(writer, "  gxx login [openai|claude|api]  Connect one account")
	fmt.Fprintln(writer, "  gxx logout                    Clear the connected account")
	fmt.Fprintln(writer, "  gxx usage                    Show API usage and remaining quota")
	fmt.Fprintln(writer, "  gxx help                     Show help")
	fmt.Fprintln(writer, "  gxx version                  Show version")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Flags:")
	printCommonFlags(writer)
}

func printLoginUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  gxx login")
	fmt.Fprintln(writer, "  gxx login openai [--device]")
	fmt.Fprintln(writer, "  gxx login claude")
	fmt.Fprintln(writer, "  gxx login api")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Only one account can be connected. A terminal shows a selectable menu;")
	fmt.Fprintln(writer, "the active row is green. api is hidden after an OpenAI or Claude login.")
	fmt.Fprintln(writer, "openai uses a ChatGPT account (Codex backend). --device is for SSH / no display.")
}

func printLogoutUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  gxx logout")
	fmt.Fprintln(writer, "  gxx logout openai")
	fmt.Fprintln(writer, "  gxx logout claude")
	fmt.Fprintln(writer, "  gxx logout api")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Without a provider, logout clears the active account.")
	fmt.Fprintln(writer, "Pass openai, claude, or api. api removes the saved OpenAI API key;")
	fmt.Fprintln(writer, "openai and claude remove OAuth tokens from config.json.")
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
	fmt.Fprintln(writer, "  --model string          Model (default: GXX_MODEL, config.json, or gpt-5.6-sol)")
	fmt.Fprintln(writer, "  --effort string         Reasoning effort (default: GXX_EFFORT, config.json, or medium)")
	fmt.Fprintln(writer, "  --context string        Context window size (default: GXX_CONTEXT, config.json, or 272k)")
	fmt.Fprintln(writer, "  --permission string     Permission mode for agent (default: GXX_PERMISSION, config.json, or ask)")
	fmt.Fprintln(writer, "                          ask confirms writes and commands; auto-writes applies files; auto applies files and commands")
	fmt.Fprintln(writer, "                          Ask/plan session modes only read files and ignore this flag")
	fmt.Fprintln(writer, "  --fast                  Use the provider fast service tier when available")
	fmt.Fprintln(writer, "  --max-steps int         Maximum model steps (default: 12)")
	fmt.Fprintln(writer, "  --command-timeout dur   Maximum command duration (default: 2m)")
	fmt.Fprintln(writer, "  --api-timeout dur       Timeout per model response (default: 10m)")
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
