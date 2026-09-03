---
title: Skills
description: Progressive Agent Skills (SKILL.md) for project and personal workflows.
---

gxx discovers [Agent Skills](https://agentskills.io/specification): a folder with a `SKILL.md` (YAML frontmatter plus markdown). Like `AGENTS.md`, skill content is untrusted data. It cannot override gxx safety, permission, or plan-mode rules.

## Where skills live

| Location | Origin |
| --- | --- |
| `~/.config/gxx/skills/<name>/SKILL.md` (Windows `%APPDATA%\gxx\skills`) | personal (`user`) |
| `.agents/skills/<name>/SKILL.md` in the workspace | project |
| `.gxx/skills/<name>/SKILL.md` in the workspace | project |

Discovery runs at the start of every turn (and on `/clear`), same rhythm as `AGENTS.md`. Invalid skills are skipped silently. The catalog caps at 64 skills.

If the same name appears in more than one place, precedence is `.gxx/skills` > `.agents/skills` > personal.

gxx does not scan `.cursor/skills` or `.claude/skills`, and there is no marketplace or lockfile.

## Progressive disclosure

1. A compact catalog (name, origin, description) is prepended to each **user** message — not the system prompt.
2. When a listed skill matches the task, the model calls `read_skill` before any other tool, then follows that skill's process.
3. Optional `path` loads another file under that skill’s root (references, assets). Default is `SKILL.md` (body only, frontmatter stripped).

`read_skill` is read-only and available in ask and plan as well as agent. If a skill's CLI is missing from PATH, gxx tells the model to retry with `npx --yes <name>`.

## Scripts

Project skill scripts inside the workspace can be run with `run_command` (same sandbox and permission rules as any other workspace command). Personal skills live outside the workspace (`~/.config/gxx/skills`); their scripts are **not** runnable.

## Frontmatter

Required fields match the open skill format:

```md
---
name: code-review
description: Review local changes against repo standards.
---

Instructions go here.
```

`name` must match the directory name (`[a-z0-9-]+`, max 64). `description` is required (max 1024). Other frontmatter fields are ignored.

## REPL

`/skills` lists discovered skills (name, origin, description). Invoke one with `/<name> <request>`, or several: `/frontend-design y /agent-browser <request>`. `gxx ask` uses the same discovery and `read_skill`.

## Privacy

The catalog on every turn, and any skill body or file loaded via `read_skill`, go to the active provider. See [Privacy](/privacy/).
