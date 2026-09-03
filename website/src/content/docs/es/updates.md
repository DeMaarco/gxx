---
title: Actualizaciones
description: Qué cambió en cada release de gxx.
---

Lo más nuevo primero. Fija una versión con `--version` en el script de [instalación](../install/), o `gxx version` para ver la tuya.

## v0.0.25

Resultados de herramientas más fiables y preview web local funcional.

- Las llamadas distintas conservan sus resultados posicionales aunque el proveedor omita call IDs.
- Las lecturas canceladas emiten eventos de finalización para que el progreso de terminal no quede bloqueado.
- Astro sirve `/` durante desarrollo y mantiene la base `/gxx/` en los builds de GitHub Pages.
- Los controles de instalación en español y los enlaces internos quedan localizados y resuelven bajo la base publicada.

## v0.0.24

Hints de comando más limpios en Windows y borrados de archivos completos.

- Un script de PowerShell que falla ya no sugiere `npx --yes` con la primera palabra de un comando mixto; el aviso usa el término que falta y omite nombres solo de Unix como `which`.
- Borrar los últimos archivos de una carpeta también elimina los directorios padre vacíos.

## v0.0.23

Herramientas más precisas y manejo de cuota de Codex.

- La búsqueda reconoce selectores con punto como `Loop.Run` e identificadores de palabra completa; tokens en MAYÚSCULAS como `TODO` siguen siendo sensibles a mayúsculas.
- Las rutas sensibles (`.env`, claves, credenciales) se omiten en búsqueda, overview y git, con un aviso: el modelo no debe afirmar que el secreto no existe.
- `git_diff` incluye archivos untracked del mismo workspace.
- `apply_patch` ignora actualizaciones no-op en lugar de reescribir los mismos bytes.
- Los errores de límite de uso de Codex no se reintentan como si fueran rate limits.

## v0.0.22

Comandos slash de skills, HTML local y capturas en el proyecto.

- Invoca un skill con `/nombre` en el REPL.
- Los prompts pegados en varias líneas siguen siendo una sola petición.
- Abre HTML del workspace vía `file://`.
- Guarda las capturas pedidas dentro de la carpeta del proyecto.

## v0.0.21

Prompts de inventario más magros y paridad en Windows.

- Guía más estricta de `list_files` para que el agente no recorra carpeta a carpeta.
- Los tests de timeout en Windows usan `PATH` en lugar de rutas absolutas a System32.

## v0.0.20

Presupuestos de contexto nativos del modelo y búsqueda más precisa.

- Los selectores de contexto siguen la ventana de cada API.
- UI de modelo y opciones renovada.
- Alternancia sin distinguir mayúsculas y búsqueda de símbolos CamelCase.

## v0.0.19

Chrome de terminal más fino y sitio de docs bilingüe.

- Barras de contexto y uso por secciones.
- Línea de estado más compacta.
- Sitio en GitHub Pages en inglés y español.

## v0.0.18

Historial de conversaciones, `AGENTS.md` en el contexto de usuario y títulos legibles.

## v0.0.17

Sesiones agent-first, Luna Responses Lite y herramientas de workspace más baratas.

## v0.0.16

Sesiones ask/plan exclusivas, defaults más seguros e installs attestados.

## v0.0.15

Coste estimado por turn, ediciones in-place y generación con GPT Image 2.

## v0.0.14

Aprobación con flechas y menús de seguimiento de plan.

## v0.0.13

Prompts de ask visibles, install que respeta PATH y picker de modelo que permanece en pantalla.

## v0.0.12

Login con ChatGPT o Claude y cuota restante de la suscripción.

## v0.0.11

Soporte Windows (amd64 y arm64).

## v0.0.10

`/eco` solo de sesión: adelgaza el input sin cambiar el modelo.

## v0.0.9

Allowlists de comandos de sesión, inspección git y sesiones OpenAI más estables.

## v0.0.8

Markdown real y tool calls que no se cuelan en la respuesta.

## v0.0.7

Residuo de tool calls filtrado y `apply_patch` más seguro.

## v0.0.6

Texto de tool calls filtrado del transcript en vivo.

## v0.0.5

Actividad de tools en vivo y recuento de líneas en parches.

## v0.0.4

Los prompts saltan de línea sin reimprimirse.

## v0.0.3

Modo plan, una sola herramienta de escritura y un árbol más limpio.

## v0.0.2

Uso, historial compactado y escrituras más estrictas.
