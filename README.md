# gxx

`gxx` is a small, inline coding-agent CLI written in Go. It is inspired by
[`fx`](https://github.com/vercel-labs/fx), but the first version deliberately
keeps a narrow scope: one OpenAI provider, one workspace, and a handful of
local tools.

## MVP features

- Interactive inline REPL with streamed model output.
- One-shot requests through `gxx ask`.
- OpenAI Responses API using the official Go SDK.
- Stateless API requests with `store:false`; conversation state only lives in
  the current process.
- Workspace-local tools to list, search, and read files, honoring `.gitignore`
  and `.gxxignore`.
- Transactional `apply_patch` changes to add, update, or delete files.
- Shell commands, gated by permission mode (`ask`, `auto-writes`, or `auto`).
- Bounded parallel execution for independent read-only tools.
- No runtime dependency on `rg`, `git`, or a TUI framework.

The MVP supports macOS and Linux. Sessions, Windows, MCP, skills, subagents,
web tools, background terminals, and ChatGPT OAuth are not included.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
```

The script downloads a macOS or Linux binary from GitHub Releases into
`~/.local/bin`. Pin a release or pick another directory:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --version v0.0.3
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --dir /usr/local/bin
```

If `gxx` is not found after install, add `~/.local/bin` to `PATH`.

## Requirements

- An OpenAI API key with access to the selected model.
- Go 1.27 or newer only if you build from source.

```sh
export OPENAI_API_KEY="..."
```

Alternatively, start the interactive REPL and run `/config`. The key is entered
without terminal echo and stored as plaintext JSON at
`~/.config/gxx/config.json` (or `$XDG_CONFIG_HOME/gxx/config.json`) with file
permissions `0600` and directory permissions `0700`. `/model` and `/mode` persist
the selected model, context size, effort, fast tier, and permission mode to the
same file. Flags and `GXX_*` / `OPENAI_API_KEY` environment variables take
precedence over saved values on future starts.

`gxx` removes the OpenAI API key, OpenAI admin key, and common secret-bearing
environment variables (`*_KEY`, `*_TOKEN`, `*_SECRET`, and similar) from
commands launched by the agent. `HOME` is kept so tools such as git and npm can
use the user configuration and caches. `GIT_TERMINAL_PROMPT=0` is set so git
does not hang waiting for credentials.

## Build from source

```sh
go build -o gxx ./cmd/gxx
go install ./cmd/gxx
```

## Test

Tests live under `test/<package>` and cover the matching `internal/<package>` code.

```sh
go test ./test/...
```

## Usage

Start an interactive session in the current directory:

```sh
./gxx
```

The REPL chrome is:

```text
◆ gxx  v0.0.3
>
gpt-5.6-sol · ask · medium · 272k · 0%
```

The first line is the product badge and version. The second line is the prompt.
The third line shows the selected model, permission mode, reasoning effort,
context window size, and how full the conversation context is. `fast` also
appears there when the fast service tier is on. Permission mode `auto` is shown
in red. The context percent turns yellow at 70% and red at 90%. In plan mode
the prompt becomes `> plan` instead of `>`.

Type `/` to open slash-command autocomplete; Tab accepts the highlighted
command, and the up/down arrows move through suggestions. With a normal prompt,
the same arrows walk previous lines from the current session. Shift+Tab toggles
plan mode: inspect and design with read-only tools (`list_files`,
`search_files`, `read_file`) instead of editing. Shift+Tab again returns to
agent mode. Plan mode is session-only and is not written to `config.json`.
Unknown slash commands and extra arguments on commands that do not take them
are rejected instead of being sent to the model. `/model` opens a
picker for GPT-5.6 Sol, Terra, and Luna; Tab switches to a submenu for context
window size, effort, and the fast service tier. Left/right cycle those options,
and Enter applies them. `/mode` opens a picker for `ask`, `auto-writes`, and
`auto`.

Run one request:

```sh
./gxx ask "explain this repository"
./gxx ask "run the tests and fix the failure"
printf 'summarize README.md' | ./gxx ask
./gxx usage
```

Return one machine-readable result and suppress token streaming:

```sh
./gxx ask --json "inspect the project"
```

The JSON object contains `answer`, `model`, `steps`, aggregate token `usage`,
tool results, and an optional `error`. Early configuration and usage failures
also return this envelope when `--json` is present.

REPL commands:

- `/help` shows available commands.
- `/model` selects GPT-5.6 Sol, Terra, or Luna. In a terminal, Tab opens options
  for context window size (`32k`, `128k`, `272k`, `1m`), effort, and fast
  on/off. You can also set them directly:
  `/model terra context=1m effort=high fast=on`.
- `/mode` selects the permission mode: `ask`, `auto-writes`, or `auto`. In a
  terminal, Tab opens a picker. You can also set it directly: `/mode auto-writes`.
  `yolo` is accepted as an alias for `auto`.
- `/config` securely prompts for and persists the OpenAI API key.
- `/context` shows a colored breakdown of context occupancy (instructions, user,
  assistant, reasoning, tools, and free space). In a terminal, Tab or Enter
  opens it as a submenu.
- `/usage` shows this process's token usage, including cached and cache-write
  tokens, organization spend for the current month, remaining spend quota, and
  remaining rate-limit quota. Account spend
  needs an OpenAI admin key (`OPENAI_ADMIN_KEY`) or a key allowed to read
  organization usage.
- `/clear` discards the in-memory conversation.
- `/exit` exits.

Shift+Tab at the prompt toggles plan mode. The prompt shows `> plan` while it
is on. In plan mode the agent only inspects the workspace and writes an
implementation plan; file mutations and shell commands are disabled even if
permission mode is `auto`.

Ctrl+C at an empty prompt asks for confirmation; a second Ctrl+C exits.
If there is typed text or an open picker, the first Ctrl+C clears the line.
During a generation, Ctrl+C cancels that turn; a second Ctrl+C while the turn
is cancelling exits. Ctrl+D at an empty prompt and `/exit` still leave immediately.

Common flags:

```text
--model string          OpenAI model (default: GXX_MODEL, config.json, or gpt-5.6-sol)
--effort string         Reasoning effort (default: GXX_EFFORT, config.json, or medium)
--context string        Context window size (default: GXX_CONTEXT, config.json, or 272k)
--permission string     Permission mode (default: GXX_PERMISSION, config.json, or ask)
--fast                  Use OpenAI fast service tier
--max-steps int         Maximum model steps (default: 12)
--command-timeout dur   Maximum command duration (default: 2m)
--api-timeout dur       Timeout per OpenAI response (default: 10m)
```

Additional environment settings are available for automation:

- `GXX_MODEL`
- `GXX_EFFORT` (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`)
- `GXX_CONTEXT` (`32k`, `128k`, `272k`, `1m`)
- `GXX_PERMISSION` (`ask`, `auto-writes`, `auto`)
- `GXX_FAST` (`on` / `off`)
- `GXX_MAX_STEPS`
- `GXX_COMMAND_TIMEOUT`
- `GXX_API_TIMEOUT`
- `GXX_MAX_TOOL_RESULT_BYTES`
- `GXX_MAX_SEARCH_RESULTS`
- `GXX_PARALLEL_READS`
- `OPENAI_ADMIN_KEY` (optional; organization spend and remaining quota on `/usage`)

## Permissions and workspace safety

Read-only tools run automatically. File mutations and shell commands follow the
current permission mode, unless plan mode is on: then only `list_files`,
`search_files`, and `read_file` are available.

- `ask` (default): every file change and command shows a preview and requires
  typing the displayed one-time `y-xxxx` challenge. This prevents input entered
  before the preview from approving an unseen action.
- `auto-writes`: file changes (`apply_patch`) run without confirmation; shell
  commands still use the `y-xxxx` challenge.
- `auto`: every file change and command runs without confirmation. The status
  line shows this mode in red.

If stdin is not interactive, `ask` still denies mutable tools, so piped
`gxx ask` stays safe by default. `--permission auto-writes` and `--permission auto`
apply those modes even without a TTY, so unattended `gxx ask` can mutate the
workspace.

Tool paths must be relative to the startup directory. Path traversal and
symlinks are rejected for file contents, and filesystem operations use Go's
root-confined API. Writes use a temporary file followed by an atomic rename.
Common secret-bearing paths such as `.env`, private keys, and credential files
are unavailable to automatic file tools.

Approval previews and streamed model output escape terminal control characters.
Previews longer than 16 KiB are truncated in the terminal with a size marker;
the underlying write or command can still be approved. `apply_patch` is the
only file-mutation tool: it takes a `changes` array of add, update, and delete
operations, stages every output first, atomically captures and revalidates all
approved source snapshots, then commits the complete file set; any failure
before completion restores the originals. `add` creates a new file. `update`
replaces `old_text` that must occur exactly once. `list_files` and
`search_files` skip default dependency directories (`node_modules`, virtualenvs,
and similar) plus patterns from `.gitignore` and `.gxxignore`. Those defaults
can be un-ignored with a negation in `.gxxignore`. `search_files` treats `query`
as a RE2 regular expression and falls back to a literal search if the pattern
does not compile; an optional `glob` limits which files are scanned.

Shell commands run in the workspace with a finite timeout and sanitized
environment through a fixed non-login `/bin/sh`, but they are **not** placed in
an operating-system sandbox. In `ask` and `auto-writes`, review each proposed
command before approving it.

## Privacy

Each OpenAI Responses request sets `store:false`. `gxx` resends the in-memory
conversation, including encrypted reasoning items, so GPT-5.6 can keep
persisted reasoning (`all_turns` by default) without `previous_response_id`.
Requests also send a stable `prompt_cache_key` and a 30-minute prompt-cache TTL
so the instruction prefix can be reused across turns.

Only files selected through tool calls are sent back to the model; the
repository is not indexed or uploaded at startup. OpenAI still receives the
prompts and tool results needed for each request, so do not use the CLI on code
you are not permitted to send to the configured API account.

## Development

```sh
gofmt -w cmd internal
go vet ./...
go test -race ./test/...
go build ./cmd/gxx
```

Automated tests use fake model responses and do not call OpenAI. A live smoke
test is intentionally manual to avoid unexpected API charges.
