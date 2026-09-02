---
title: REPL
description: Keyboard shortcuts, session modes, and slash commands.
---

```text
◆ gxx  v0.0.20     badge and version
>                 type here  ·  Shift+Tab →  > ask  ·  Shift+Tab →  agent again
gpt-5.6-sol · auto · medium · 272k · 0%
```

| Key | Action |
| --- | --- |
| `/` | Slash commands |
| `Tab` | Complete, or open pickers |
| `Shift+Tab` | From ask/plan, back to agent. From agent, cycle ask and plan |
| `Ctrl+O` | Open saved conversations for this workspace |
| `Ctrl+C` | Clear, cancel, or confirm exit |
| `Ctrl+D` | Exit |

`gxx` starts in agent, so `/mode` (`ask`, `auto-writes`, `auto`) applies. Ask and plan are separate session modes on `Shift+Tab`. They never overlap. One press from ask or plan returns to agent. Both are read-only: only file reads and git inspect, with no approval prompt. They are session-only and not saved to config.

After a plan is generated, a terminal shows an arrow-key menu: execute the plan, request changes, or cancel. Request changes stays in plan so you can send a revision. Execute switches to agent and implements, using the current permission mode.

Conversations are saved automatically after each turn to `~/.config/gxx/conversations/` (per workspace). `Ctrl+O` or `/history` opens an arrow-key menu to load a previous thread. The screen does not replay old turns; the model context is restored in memory. `/clear` archives the current thread and starts a new one.

`/eco` is also session-only. It paints green on the prompt like plan. `/eco` toggles; `/eco lite` `full` `ultra` set the strength (aliases: 1/2/3). Eco never changes the model. It compresses request input the way Caveman does: drop filler, keep code, paths, URLs, and identifiers. Tool descriptions shrink too. Ultra also drops reasoning replay.

## Commands

| Command | What it does |
| --- | --- |
| `/help` | Commands |
| `/model` | Models for the connected account only · Tab for context, effort, fast |
| `/eco` | Caveman input saver · `lite` `full` `ultra` · green on the prompt · session-only |
| `/compact` | Summarize older turns to free context · optional focus text |
| `/mode` | Permission for **agent**: `ask` (confirm writes and commands) · `auto-writes` · `auto` |
| `/config` | Save the OpenAI API key |
| `/login` | Connect one account · openai · claude · api · green marks the active one |
| `/logout` | Clear the connected account |
| `/context` | Window occupancy |
| `/usage` | Session tokens, estimated cost, and remaining subscription or API quota |
| `/history` | Open saved conversations for this workspace |
| `/clear` | Archive this conversation and start a new one |
| `/exit` | Quit |

Inline forms work too:

```text
/model terra context=1m effort=high fast=on
/model opus
/mode auto-writes
```

Context sizes follow each model’s API window: OpenAI `272k` or `1m`, Claude `300k` (cheaper compact budget) or `1m` (Haiku `200k`). Switching models clamps an unsupported size.

`yolo` is an alias for `auto`.

The prompt badge is the session: plain `>` is agent, `> ask` and `> plan` after `Shift+Tab`.
The status line is model · permission mode · effort · context size · window fill.
`auto` paints red. Context turns yellow at 70% and red at 90%.
After each turn the footer adds estimated USD. Rates are re-read from the
official OpenAI and Anthropic pricing pages so a price change is picked up
without a new gxx release.
