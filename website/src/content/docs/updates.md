---
title: Updates
description: What changed in each gxx release.
---

Newest first. Install a pin with `--version` on the [install](../install/) script, or `gxx version` to see what you have.

## v0.0.24

Cleaner Windows command hints and complete file deletes.

- Failed PowerShell scripts no longer suggest `npx --yes` for the first word of a mixed command; the hint uses the missing term and skips Unix-only names like `which`.
- Deleting the last files in a folder also removes the empty parent directories.

## v0.0.23

Sharper tools and Codex quota handling.

- Search matches dotted selectors such as `Loop.Run` and whole-word identifiers; ALL-CAPS tokens like `TODO` stay case-sensitive.
- Sensitive paths (`.env`, keys, credentials) are omitted from search, overview, and git with a notice — the model must not claim a secret is missing.
- `git_diff` includes untracked files in the same workspace.
- `apply_patch` skips no-op updates instead of rewriting the same bytes.
- Codex usage-limit errors are not retried as if they were rate limits.

## v0.0.22

Skill slash commands, local HTML open, and screenshot pinning.

- Invoke a skill as `/name` from the REPL.
- Pasted multi-line prompts stay one request.
- Open workspace HTML via `file://`.
- Save requested screenshots inside the project folder.

## v0.0.21

Leaner inventory prompts and Windows parity.

- Tighter `list_files` guidance so agents do not fan out folder by folder.
- Windows timeout-kill tests use `PATH` instead of absolute System32 paths.

## v0.0.20

Model-native context budgets and sharper search.

- Context pickers follow each model's API window.
- Refreshed model/options UI.
- Case-insensitive alternation and CamelCase symbol search.

## v0.0.19

Refined terminal chrome and a bilingual docs site.

- Sectioned bars for context and usage.
- Tighter status-line layout.
- GitHub Pages site in English and Spanish.

## v0.0.18

Saved conversation history, `AGENTS.md` in user context, and readable conversation titles.

## v0.0.17

Agent-first sessions, Luna Responses Lite, and cheaper workspace tools.

## v0.0.16

Exclusive ask/plan sessions, safer defaults, and attested installs.

## v0.0.15

Estimated turn cost, in-place file edits, and GPT Image 2 generation.

## v0.0.14

Arrow-key approval and plan follow-up menus.

## v0.0.13

Visible ask prompts, a PATH-aware install, and a model picker that stays on screen.

## v0.0.12

ChatGPT or Claude login and remaining subscription quota.

## v0.0.11

Windows support (amd64 and arm64).

## v0.0.10

Session-only `/eco` that slims request input without changing the model.

## v0.0.9

Session command allowlists, git inspect, and sturdier OpenAI sessions.

## v0.0.8

Real markdown rendering and tool calls that stay out of the answer.

## v0.0.7

Leaked tool-call residue stripped and safer `apply_patch` updates.

## v0.0.6

Leaked tool-call text stripped from the live transcript.

## v0.0.5

Live tool activity and patch line counts.

## v0.0.4

Prompts wrap to the next line without reprinting.

## v0.0.3

Plan mode, a single write tool, and a cleaner tree.

## v0.0.2

Usage, compacted history, and tighter writes.
