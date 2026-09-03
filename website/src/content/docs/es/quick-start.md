---
title: Inicio rápido
description: Conecta OpenAI o Claude y ejecuta tu primera sesión.
---

**OpenAI:** una [API key](https://platform.openai.com/api-keys), o una cuenta ChatGPT (Plus/Pro/Team). Exporta la key, ejecuta `/config`, o inicia sesión:

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

El login con cuenta habla con el backend ChatGPT Codex (no documentado; puede romperse). `/config` sigue siendo la API key de plataforma. `OPENAI_API_KEY` gana sobre una key guardada, y una key gana sobre OAuth. En SSH o una máquina sin pantalla, usa `gxx login openai --device`.

**Claude:** suscripción Pro/Max. Ejecuta `gxx login claude`, o inicia `gxx` y ejecuta `/login claude`. También puedes exportar un token de `claude setup-token`:

```sh
gxx login claude
cd your-project
gxx --model sonnet
```

`gxx login` / `/login` sin proveedor abre un selector en la terminal (`openai` o `claude`). Los scripts deben pasar el proveedor. `chatgpt` y `codex` son alias de `openai`.

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

One-shot, sin el REPL:

```sh
gxx ask "explain this repository"
gxx ask --json "inspect the project"
echo "what does main.go do?" | gxx ask
gxx usage
```

Coloca un `AGENTS.md` en la raíz del proyecto si quieres instrucciones extra cargadas en la sesión. Se re-lee al inicio de cada turno y en `/clear`. Esas notas no pueden anular las reglas de seguridad, permisos o plan-mode de gxx.

Para flujos reutilizables, añade Agent Skills (`SKILL.md`) bajo `.agents/skills`, `.gxx/skills`, o `~/.config/gxx/skills`. gxx envía un catálogo compacto cada turno y carga el cuerpo con `read_skill` cuando encaja. Ver [Skills](../skills/).
