---
title: CLI
description: One-shot commands, flags, and piping with gxx ask.
---

## One-shot

Run a single prompt without opening the REPL:

```sh
gxx ask "explain this repository"
gxx ask --json "inspect the project"
echo "what does main.go do?" | gxx ask
gxx usage
```

Piped input is supported. Piped `gxx ask` stays in ask session unless you pass `--permission`.

## Login

```sh
gxx login openai
gxx login claude
gxx login openai --device
```

Scripts must pass the provider. `chatgpt` and `codex` are aliases for `openai`.

## Common flags

| Flag | Description |
| --- | --- |
| `--model` | Model name (e.g. `sonnet`, `opus`) |
| `--json` | Machine-readable output |
| `--permission` | Override permission mode for one-shot runs |

## Version

```sh
gxx version
```

Current release: **v0.0.24**

See [Updates](../updates/) for what changed in each version.
