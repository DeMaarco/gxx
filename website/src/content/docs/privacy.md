---
title: Privacy
description: How gxx handles secrets, storage, and provider requests.
---

- OpenAI requests use `store:false`.
- Only files the tools actually open go to the active provider. The Agent Skills catalog (name + description) is sent each turn; skill bodies and paths loaded with `read_skill` go to the provider too.
- Secret paths (`.env`, keys, credentials) are blocked on file tools, image writes, git inspect, and commands that name them.
- Image generation uses the platform Images API with the OpenAI API key. ChatGPT login is not enough for `generate_image`.
- The OpenAI API key, ChatGPT Codex OAuth tokens, and Claude OAuth tokens live in the same owner-only `config.json` (`0600` on Unix, a user-only ACL on Windows). They are stripped from child shell environments. gxx does not read `~/.codex/auth.json` or `~/.claude/.credentials.json`.
- Do not point it at code you cannot send to that account.
