# gxx

```text
◆ gxx  v0.0.3
>
gpt-5.6-sol · ask · medium · 272k · 0%
```

A small coding agent for the terminal. Open a repo, type what you want,
and it lists, searches, reads, patches, and runs commands in that folder.

Inspired by [`fx`](https://github.com/vercel-labs/fx). Narrower on purpose:
one provider (OpenAI), one workspace, no TUI.

macOS and Linux.

## Install

The repository is private, so GitHub CLI has to fetch the script:

```sh
gh api -H "Accept: application/vnd.github.raw" repos/DeMaarco/gxx/contents/install.sh | sh
```

That drops `gxx` in `~/.local/bin`. If the shell cannot find it:

```sh
export PATH="$HOME/.local/bin:$PATH"
gxx version
```

When the repo is public, this is enough:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
```

Pin a release or another directory with `--version v0.0.3` and `--dir /usr/local/bin`
after `sh -s --`.

## Run

You need an OpenAI API key. Either export it, or start `gxx` and run `/config`.

```sh
export OPENAI_API_KEY="..."
cd your-project
gxx
```

One-shot, without the REPL:

```sh
gxx ask "explain this repository"
gxx ask --json "inspect the project"
gxx usage
```

## Prompt

```text
◆ gxx  v0.0.3     badge and version
>                 type here  ·  becomes  > plan  in plan mode
gpt-5.6-sol · ask · medium · 272k · 0%
```

The status line is model, permission mode, effort, context size, and how full
the window is. `auto` paints red. Context turns yellow at 70% and red at 90%.

| Key | Action |
| --- | --- |
| `/` | Slash commands |
| Tab | Complete, or open pickers |
| Shift+Tab | Plan mode on/off |
| Ctrl+C | Clear, cancel, or confirm exit |
| Ctrl+D | Exit |

Plan mode is read-only: look and design, no writes and no shell. It is not saved
to config.

## Commands

| Command | What it does |
| --- | --- |
| `/help` | Commands |
| `/model` | GPT-5.6 Sol, Terra, or Luna · Tab for context, effort, fast |
| `/mode` | `ask` · `auto-writes` · `auto` |
| `/config` | Save the API key |
| `/context` | Window occupancy |
| `/usage` | Tokens and remaining quota |
| `/clear` | Forget this conversation |
| `/exit` | Quit |

`/model terra context=1m effort=high fast=on` and `/mode auto-writes` also work
as one line. `yolo` is an alias for `auto`.

## Permissions

Reads always run. Writes and shell commands follow the mode, unless plan is on.

| Mode | Files | Shell |
| --- | --- | --- |
| `ask` | Preview + `y-xxxx` | Preview + `y-xxxx` |
| `auto-writes` | Apply | Preview + `y-xxxx` |
| `auto` | Apply | Apply |

Piped `gxx ask` stays on `ask` and denies mutations unless you pass
`--permission`. Commands are not OS-sandboxed; review them in `ask`.

The workspace is the directory you started in. Traversal, outside symlinks,
and secret paths (`.env`, keys, credentials) are blocked.

## Privacy

Requests use `store:false`. Only files the tools actually open go to OpenAI.
Do not point it at code you cannot send to that account.

## Source

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
