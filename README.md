<div align="center">

```
 ██████╗ ██╗  ██╗██╗  ██╗
██╔════╝ ╚██╗██╔╝╚██╗██╔╝
██║  ███╗ ╚███╔╝  ╚███╔╝
██║   ██║ ██╔██╗  ██╔██╗
╚██████╔╝██╔╝ ██╗██╔╝ ██╗
 ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝
```

**A small coding agent for the terminal.**

Open a repo, type what you want, and it lists, searches, reads,
inspects git, patches, generates images, and runs commands — in that folder only.

[Install](#install) · [Quick start](#quick-start) · [REPL](#repl) · [Permissions](#permissions)

[![Release](https://img.shields.io/github/v/release/DeMaarco/gxx?style=flat-square&color=a855f7)](https://github.com/DeMaarco/gxx/releases)
[![License](https://img.shields.io/badge/license-Apache%202.0-0ea5e9?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/macOS%20%7C%20Linux%20%7C%20Windows-111827?style=flat-square)](#install)

</div>

---

Inspired by [`fx`](https://github.com/vercel-labs/fx), but narrower on purpose:
**OpenAI or Claude**, **one workspace**, **no TUI**. Just a prompt.

```text
◆ gxx  v0.0.16
> ask
gpt-5.6-sol · ask · medium · 272k · 0%
```

The prompt badge is the session (`ask`, `plan`, or plain `>` for agent).
The status line is model · permission mode · effort · context size · window fill.
`auto` paints red. Context turns yellow at 70% and red at 90%.
After each turn the footer adds estimated USD. Rates are re-read from the
official OpenAI and Anthropic pricing pages so a price change is picked up
without a new gxx release.

## Features

| | What you get |
| --- | --- |
| **Workspace-bound** | The directory you start in is the whole world. No traversal, no outside symlinks. |
| **Ask** | Default session. Lists, searches, reads, and inspects git. No patches, images, or shell. `Shift+Tab` cycles ask → plan → agent. |
| **Plan** | Read-only design pass. After the plan, arrow keys choose execute, request changes, or cancel. Execute leaves plan and enters agent. |
| **Git inspect** | `git_status`, `git_diff`, and `git_log` are read-only and stay inside the workspace. |
| **Images** | `generate_image` calls GPT Image 2 (or another GPT image model) through the OpenAI Images API and writes the file in the workspace. Needs a platform API key. |
| **One-shot CLI** | `gxx ask` for scripts and pipes. `--json` when you want a machine-readable result. |
| **Secret-aware** | `.env`, keys, and credential paths are blocked on read, search, list, patch, git inspect, and shell commands that name them. Requests go out with `store:false`. |

## Install

macOS and Linux, amd64 and arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
```

That puts `gxx` in `~/.local/bin`. If the shell cannot find it, or another `gxx` is first on PATH:

```sh
export PATH="$HOME/.local/bin:$PATH"
gxx version
```

Pin a release or another directory:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --version v0.0.16
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --dir /usr/local/bin
```

Windows, amd64 and arm64 (PowerShell):

```powershell
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

That puts `gxx.exe` in `%LOCALAPPDATA%\gxx` and puts that folder first on your user PATH and this session. If `gxx version` still shows an older build, another `gxx.exe` is first on PATH (`Get-Command gxx`).

Pin a release or another directory:

```powershell
$env:GXX_VERSION = "v0.0.16"
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

`run_command` uses PowerShell. The git tools need [Git for Windows](https://git-scm.com/download/win) on PATH.

`/config` writes `%APPDATA%\gxx\config.json`. An older `%USERPROFILE%\.config\gxx\config.json` is still read until the next save.

## Quick start

**OpenAI:** an [API key](https://platform.openai.com/api-keys), or a ChatGPT account (Plus/Pro/Team). Export the key, run `/config`, or log in:

```sh
export OPENAI_API_KEY="sk-..."
cd your-project
gxx
```

```sh
gxx login openai
cd your-project
gxx
```

Account login talks to the ChatGPT Codex backend (undocumented; it can break). `/config` is still the platform API key. `OPENAI_API_KEY` wins over a saved key, and a key wins over OAuth. On SSH or a machine without a display, use `gxx login openai --device`.

**Claude:** a Pro/Max subscription. Run `gxx login claude`, or start `gxx` and run `/login claude`. You can also export a token from `claude setup-token`:

```sh
gxx login claude
cd your-project
gxx --model sonnet
```

`gxx login` / `/login` without a provider opens a picker in a terminal (`openai` or `claude`). Scripts must pass the provider. `chatgpt` and `codex` are aliases for `openai`.

```sh
export CLAUDE_CODE_OAUTH_TOKEN="..."
gxx ask --model opus "explain this repository"
```

PowerShell (OpenAI):

```powershell
$env:OPENAI_API_KEY = "sk-..."
cd your-project
gxx
```

One-shot, without the REPL:

```sh
gxx ask "explain this repository"
gxx ask --json "inspect the project"
echo "what does main.go do?" | gxx ask
gxx usage
```

Drop an `AGENTS.md` in the project root if you want extra instructions loaded into the session. It is re-read at the start of every turn and on `/clear`. Those notes cannot override gxx safety, permission, or plan-mode rules.

## REPL

```text
◆ gxx  v0.0.16     badge and version
> ask             type here  ·  becomes  > plan  in plan  ·  plain > in agent
gpt-5.6-sol · ask · medium · 272k · 0%
```

| Key | Action |
| --- | --- |
| `/` | Slash commands |
| `Tab` | Complete, or open pickers |
| `Shift+Tab` | Cycle ask → plan → agent → ask |
| `Ctrl+C` | Clear, cancel, or confirm exit |
| `Ctrl+D` | Exit |

Ask and plan are separate session modes. They never overlap. Both are read-only: only file reads and git inspect, with no approval prompt. They are session-only and not saved to config.

After a plan is generated, a terminal shows an arrow-key menu: execute the plan, request changes, or cancel. Request changes stays in plan so you can send a revision. Execute switches to agent and implements, using the current permission mode.

`/eco` is also session-only. It paints green on the prompt like plan. `/eco` toggles; `/eco lite` `full` `ultra` set the strength (aliases: 1/2/3). Eco never changes the model. It compresses request input the way Caveman does: drop filler, keep code, paths, URLs, and identifiers. Tool descriptions shrink too. Ultra also drops reasoning replay.

### Commands

| Command | What it does |
| --- | --- |
| `/help` | Commands |
| `/model` | Models for the connected account only · Tab for context, effort, fast |
| `/eco` | Caveman input saver · `lite` `full` `ultra` · green on the prompt · session-only |
| `/mode` | Permission for **agent**: `ask` (confirm writes and commands) · `auto-writes` · `auto` |
| `/config` | Save the OpenAI API key |
| `/login` | Connect one account · openai · claude · api · green marks the active one |
| `/logout` | Clear the connected account |
| `/context` | Window occupancy |
| `/usage` | Session tokens, estimated cost, and remaining subscription or API quota |
| `/clear` | Forget this conversation |
| `/exit` | Quit |

Inline forms work too:

```text
/model terra context=1m effort=high fast=on
/model opus
/mode auto-writes
```

`yolo` is an alias for `auto`.

## Permissions

Reads always run, with no approval. Ask and plan only expose read tools, so `/mode` does not apply while those sessions are on.

In agent, writes and shell commands follow the permission mode.

| Mode | Files | Shell |
| --- | --- | --- |
| `ask` | Preview + confirm | Preview + arrow menu, or allow that exact command for the session |
| `auto-writes` | Apply | Preview + arrow menu, or allow that exact command for the session |
| `auto` | Apply | Apply |

A terminal shows an arrow-key menu for commands that still need approval (deny is the default). Without one, type `y-xxxx` to approve or `a-xxxx` to allow that exact command for the session.
Piped `gxx ask` stays in ask session unless you pass `--permission`. Changing `/mode` clears the session command allowlist. On Windows they run under PowerShell (`pwsh` if present, otherwise `powershell.exe`).
Commands are not OS-sandboxed — review them in `auto-writes` or `auto`.

## Privacy

- OpenAI requests use `store:false`.
- Only files the tools actually open go to the active provider.
- Secret paths (`.env`, keys, credentials) are blocked on file tools, image writes, git inspect, and commands that name them.
- Image generation uses the platform Images API with the OpenAI API key. ChatGPT login is not enough for `generate_image`.
- The OpenAI API key, ChatGPT Codex OAuth tokens, and Claude OAuth tokens live in the same owner-only `config.json` (`0600` on Unix, a user-only ACL on Windows). They are stripped from child shell environments. gxx does not read `~/.codex/auth.json` or `~/.claude/.credentials.json`.
- Do not point it at code you cannot send to that account.

## Build from source

Go 1.27+.

```sh
git clone https://github.com/DeMaarco/gxx.git
cd gxx
go install ./cmd/gxx
go test ./test/...
```

## License

Copyright 2026 DeMarco

Licensed under the [Apache License, Version 2.0](LICENSE).
