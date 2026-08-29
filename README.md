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
inspects git, patches, and runs commands — in that folder only.

[Install](#install) · [Quick start](#quick-start) · [REPL](#repl) · [Permissions](#permissions)

[![Release](https://img.shields.io/github/v/release/DeMaarco/gxx?style=flat-square&color=a855f7)](https://github.com/DeMaarco/gxx/releases)
[![License](https://img.shields.io/badge/license-Apache%202.0-0ea5e9?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/macOS%20%7C%20Linux%20%7C%20Windows-111827?style=flat-square)](#install)

</div>

---

Inspired by [`fx`](https://github.com/vercel-labs/fx), but narrower on purpose:
**one provider** (OpenAI), **one workspace**, **no TUI**. Just a prompt.

```text
◆ gxx  v0.0.11
>
gpt-5.6-sol · ask · medium · 272k · 0%
```

The status line is model · permission mode · effort · context size · window fill.
`auto` paints red. Context turns yellow at 70% and red at 90%.

## Features

| | What you get |
| --- | --- |
| **Workspace-bound** | The directory you start in is the whole world. No traversal, no outside symlinks. |
| **Ask before it writes** | Default `ask` mode previews file edits and shell commands until you approve. Type `a-xxxx` to allow that exact command for the rest of the session. |
| **Plan mode** | `Shift+Tab` for a read-only pass: inspect and design, no writes and no shell. |
| **Git inspect** | `git_status`, `git_diff`, and `git_log` are read-only and stay inside the workspace. |
| **One-shot CLI** | `gxx ask` for scripts and pipes. `--json` when you want a machine-readable result. |
| **Secret-aware** | `.env`, keys, and credential paths stay blocked. Requests go out with `store:false`. |

## Install

macOS and Linux, amd64 and arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
```

That puts `gxx` in `~/.local/bin`. If the shell cannot find it:

```sh
export PATH="$HOME/.local/bin:$PATH"
gxx version
```

Pin a release or another directory:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --version v0.0.11
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --dir /usr/local/bin
```

Windows, amd64 and arm64 (PowerShell):

```powershell
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

That puts `gxx.exe` in `%LOCALAPPDATA%\gxx`. If the shell cannot find it:

```powershell
$env:Path = "$env:LOCALAPPDATA\gxx;$env:Path"
gxx version
```

Pin a release or another directory:

```powershell
$env:GXX_VERSION = "v0.0.11"
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

`run_command` uses PowerShell. The git tools need [Git for Windows](https://git-scm.com/download/win) on PATH.

## Quick start

You need an [OpenAI API key](https://platform.openai.com/api-keys). Export it, or start `gxx` and run `/config`.

```sh
export OPENAI_API_KEY="sk-..."
cd your-project
gxx
```

PowerShell:

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

Drop an `AGENTS.md` in the project root if you want extra instructions loaded into the session. It is re-read at the start of every turn and on `/clear`.

## REPL

```text
◆ gxx  v0.0.11     badge and version
>                 type here  ·  becomes  > plan  in plan mode
gpt-5.6-sol · ask · medium · 272k · 0%
```

| Key | Action |
| --- | --- |
| `/` | Slash commands |
| `Tab` | Complete, or open pickers |
| `Shift+Tab` | Plan mode on / off |
| `Ctrl+C` | Clear, cancel, or confirm exit |
| `Ctrl+D` | Exit |

Plan mode is session-only. It is not saved to config.

`/eco` is also session-only. It paints green on the prompt like plan. `/eco` toggles; `/eco lite` `full` `ultra` set the strength (aliases: 1/2/3). Eco never changes the model. It compresses request input the way Caveman does: drop filler, keep code, paths, URLs, and identifiers. Tool descriptions shrink too. Ultra also drops reasoning replay.

### Commands

| Command | What it does |
| --- | --- |
| `/help` | Commands |
| `/model` | GPT-5.6 Sol, Terra, or Luna · Tab for context, effort, fast |
| `/eco` | Caveman input saver · `lite` `full` `ultra` · green on the prompt · session-only |
| `/mode` | `ask` · `auto-writes` · `auto` |
| `/config` | Save the API key |
| `/context` | Window occupancy |
| `/usage` | Tokens and remaining quota |
| `/clear` | Forget this conversation |
| `/exit` | Quit |

Inline forms work too:

```text
/model terra context=1m effort=high fast=on
/mode auto-writes
```

`yolo` is an alias for `auto`.

## Permissions

Reads always run. Writes and shell commands follow the mode, unless plan is on.

| Mode | Files | Shell |
| --- | --- | --- |
| `ask` | Preview + `y-xxxx` | Preview + `y-xxxx`, or `a-xxxx` to allow that exact command for the session |
| `auto-writes` | Apply | Preview + `y-xxxx` / `a-xxxx` |
| `auto` | Apply | Apply |

Piped `gxx ask` stays on `ask` and denies mutations unless you pass `--permission`.
Commands are not OS-sandboxed — review them in `ask`. Changing `/mode` clears the session command allowlist. On Windows they run under PowerShell (`pwsh` if present, otherwise `powershell.exe`).

## Privacy

- Requests use `store:false`.
- Only files the tools actually open go to OpenAI.
- Secret paths (`.env`, keys, credentials) are blocked.
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
