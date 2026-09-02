---
title: CLI
description: Comandos one-shot, flags y piping con gxx ask.
---

## One-shot

Ejecuta un prompt único sin abrir el REPL:

```sh
gxx ask "explain this repository"
gxx ask --json "inspect the project"
echo "what does main.go do?" | gxx ask
gxx usage
```

Se admite entrada por pipe. `gxx ask` por pipe permanece en sesión ask a menos que pases `--permission`.

## Login

```sh
gxx login openai
gxx login claude
gxx login openai --device
```

Los scripts deben pasar el proveedor. `chatgpt` y `codex` son alias de `openai`.

## Flags comunes

| Flag | Descripción |
| --- | --- |
| `--model` | Nombre del modelo (p. ej. `sonnet`, `opus`) |
| `--json` | Salida legible por máquina |
| `--permission` | Anular el modo de permisos para ejecuciones one-shot |

## Versión

```sh
gxx version
```

Release actual: **v0.0.20**
