---
title: Skills
description: Agent Skills progresivas (SKILL.md) para flujos de proyecto y personales.
---

gxx descubre [Agent Skills](https://agentskills.io/specification): una carpeta con un `SKILL.md` (frontmatter YAML más markdown). Como `AGENTS.md`, el contenido de una skill es dato no confiable. No puede anular las reglas de seguridad, permisos o plan-mode de gxx.

## Dónde viven las skills

| Ubicación | Origen |
| --- | --- |
| `~/.config/gxx/skills/<name>/SKILL.md` (Windows `%APPDATA%\gxx\skills`) | personal (`user`) |
| `.agents/skills/<name>/SKILL.md` en el workspace | project |
| `.gxx/skills/<name>/SKILL.md` en el workspace | project |

El descubrimiento corre al inicio de cada turno (y en `/clear`), el mismo ritmo que `AGENTS.md`. Las skills inválidas se omiten en silencio. El catálogo tiene tope de 64 skills.

Si el mismo nombre aparece en más de un sitio, la precedencia es `.gxx/skills` > `.agents/skills` > personal.

gxx no escanea `.cursor/skills` ni `.claude/skills`, y no hay marketplace ni lockfile.

## Divulgación progresiva

1. Un catálogo compacto (nombre, origen, description) se antepone a cada mensaje de **usuario** — no al system prompt.
2. Cuando una skill listada encaja con la tarea, el modelo llama `read_skill` antes de actuar.
3. Un `path` opcional carga otro fichero bajo la raíz de esa skill (referencias, assets). El valor por defecto es `SKILL.md` (solo el cuerpo; el frontmatter se quita).

`read_skill` es de solo lectura y está disponible en ask y plan, además de agente.

## Scripts

Los scripts de skills de **proyecto** dentro del workspace se pueden ejecutar con `run_command` (mismas reglas de sandbox y permisos que cualquier otro comando del workspace). Las skills personales viven fuera del workspace (`~/.config/gxx/skills`); sus scripts **no** son ejecutables.

## Frontmatter

Los campos requeridos siguen el formato abierto de skills:

```md
---
name: code-review
description: Review local changes against repo standards.
---

Las instrucciones van aquí.
```

`name` debe coincidir con el nombre del directorio (`[a-z0-9-]+`, máx. 64). `description` es obligatorio (máx. 1024). El resto de campos del frontmatter se ignoran.

## REPL

`/skills` lista las skills descubiertas (nombre, origen, description). Sin argumentos. `gxx ask` usa el mismo descubrimiento y `read_skill`; no hace falta un flag extra.

## Privacidad

El catálogo en cada turno, y cualquier cuerpo o fichero de skill cargado con `read_skill`, van al proveedor activo. Ver [Privacidad](/es/privacy/).
