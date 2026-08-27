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
- Workspace-local tools to list, search, and read files.
- Transactional `apply_patch` changes across added, updated, and deleted files.
- Exact text edits, complete file writes, and shell commands, gated by permission
  mode (`ask`, `auto-writes`, or `auto`).
- Bounded parallel execution for independent read-only tools.
- No runtime dependency on `rg`, `git`, or a TUI framework.

The MVP supports macOS and Linux. Sessions, Windows, MCP, skills, subagents,
web tools, background terminals, and ChatGPT OAuth are not included.

## Requirements

- Go 1.27 or newer.
- An OpenAI API key with access to the selected model.

```sh
export OPENAI_API_KEY="..."
```

Alternatively, start the interactive REPL and run `/config`. The key is entered
without terminal echo and stored as plaintext JSON at
`~/.config/gxx/config.json` (or `$XDG_CONFIG_HOME/gxx/config.json`) with file
permissions `0600` and directory permissions `0700`. `OPENAI_API_KEY` takes
precedence over the saved value on future starts.

`gxx` removes the OpenAI key and common secret-bearing environment variables
from commands launched by the agent.

## Build

```sh
go build -o gxx ./cmd/gxx
```

You can also install it into your Go bin directory:

```sh
go install ./cmd/gxx
```

## Usage

Start an interactive session in the current directory:

```sh
./gxx
```

The REPL chrome is:

```text
◆ gxx  0.0.1
>
gpt-5.6-sol · ask · medium · 272k
```

The first line is the product badge and version. The second line is the prompt.
The third line shows the selected model, permission mode, reasoning effort, and
context window size. `fast` also appears there when the fast service tier is on.
Permission mode `auto` is shown in red.

Type `/` to open slash-command autocomplete; Tab accepts the highlighted
command, and the up/down arrows move through suggestions. With a normal prompt,
the same arrows walk previous lines from the current session. `/model` opens a
picker for GPT-5.6 Sol, Terra, and Luna; Tab switches to a submenu for context
window size, effort, and the fast service tier. Left/right cycle those options,
and Enter applies them. `/mode` opens a picker for `ask`, `auto-writes`, and
`auto`.

Run one request:

```sh
./gxx ask "explain this repository"
./gxx ask "run the tests and fix the failure"
printf 'summarize README.md' | ./gxx ask
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
- `/clear` discards the in-memory conversation.
- `/exit` exits.

Common flags:

```text
--model string          OpenAI model (default: GXX_MODEL or gpt-5.6-sol)
--effort string         Reasoning effort (default: GXX_EFFORT or medium)
--context string        Context window size (default: GXX_CONTEXT or 272k)
--permission string     Permission mode (default: GXX_PERMISSION or ask)
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

## Permissions and workspace safety

Read-only tools run automatically. File mutations and shell commands follow the
current permission mode:

- `ask` (default): every file change and command shows a preview and requires
  typing the displayed one-time `y-xxxx` challenge. This prevents input entered
  before the preview from approving an unseen action.
- `auto-writes`: file changes (`apply_patch`, `edit_file`, `write_file`) run
  without confirmation; shell commands still use the `y-xxxx` challenge.
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
Actions whose preview exceeds 16 KiB are rejected instead of executing hidden
content. `apply_patch` stages every output first, atomically captures and
revalidates all approved source snapshots, then commits the complete file set;
any failure before completion restores the originals. Single-file edits are
also cancelled if their source changes between preview and write.

Shell commands run in the workspace with a finite timeout and sanitized
environment through a fixed non-login `/bin/sh`, but they are **not** placed in
an operating-system sandbox. In `ask` and `auto-writes`, review each proposed
command before approving it.

## Privacy

Each OpenAI Responses request sets `store:false`. `gxx` manually resends the
in-memory conversation and requests encrypted reasoning content so reasoning
models can continue correctly without `previous_response_id`.

Only files selected through tool calls are sent back to the model; the
repository is not indexed or uploaded at startup. OpenAI still receives the
prompts and tool results needed for each request, so do not use the CLI on code
you are not permitted to send to the configured API account.

## Development

```sh
gofmt -w cmd internal
go vet ./...
go test -race ./...
go build ./cmd/gxx
```

Automated tests use fake model responses and do not call OpenAI. A live smoke
test is intentionally manual to avoid unexpected API charges.
