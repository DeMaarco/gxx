---
title: Quick start
description: Connect OpenAI or Claude and run your first session.
---

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

For reusable workflows, add Agent Skills (`SKILL.md`) under `.agents/skills`, `.gxx/skills`, or `~/.config/gxx/skills`. gxx sends a compact catalog each turn and loads the body with `read_skill` when it matches. See [Skills](/skills/).
