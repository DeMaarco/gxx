---
title: Instalación
description: Instala gxx en macOS, Linux y Windows.
---

macOS y Linux, amd64 y arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
```

Eso instala `gxx` en `~/.local/bin`. Si el shell no lo encuentra, o hay otro `gxx` primero en PATH:

```sh
export PATH="$HOME/.local/bin:$PATH"
gxx version
```

Fijar una release u otro directorio:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --version v0.0.24
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --dir /usr/local/bin
```

Windows, amd64 y arm64 (PowerShell):

```powershell
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

Eso instala `gxx.exe` en `%LOCALAPPDATA%\gxx` y pone esa carpeta primero en el PATH de usuario y en esta sesión. Si `gxx version` sigue mostrando una build antigua, hay otro `gxx.exe` primero en PATH (`Get-Command gxx`).

Fijar una release u otro directorio:

```powershell
$env:GXX_VERSION = "v0.0.24"
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

`run_command` usa PowerShell. Las herramientas git necesitan [Git for Windows](https://git-scm.com/download/win) en PATH.

`/config` escribe `%APPDATA%\gxx\config.json`. Un `%USERPROFILE%\.config\gxx\config.json` antiguo se sigue leyendo hasta el próximo guardado.

## Compilar desde fuente

Go 1.27+.

```sh
git clone https://github.com/DeMaarco/gxx.git
cd gxx
go install ./cmd/gxx
go test ./test/...
```
