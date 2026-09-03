---
title: Install
description: Install gxx on macOS, Linux, and Windows.
---

macOS and Linux, amd64 and arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
```

That puts `gxx` in `~/.local/bin`. If the shell cannot find it, or another `gxx` is first on PATH:

```sh
export PATH="$HOME/.local/bin:$PATH"
gxx version
```

Pin a release or another directory:

```sh
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --version v0.0.24
curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --dir /usr/local/bin
```

Windows, amd64 and arm64 (PowerShell):

```powershell
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

That puts `gxx.exe` in `%LOCALAPPDATA%\gxx` and puts that folder first on your user PATH and this session. If `gxx version` still shows an older build, another `gxx.exe` is first on PATH (`Get-Command gxx`).

Pin a release or another directory:

```powershell
$env:GXX_VERSION = "v0.0.24"
irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
```

`run_command` uses PowerShell. The git tools need [Git for Windows](https://git-scm.com/download/win) on PATH.

`/config` writes `%APPDATA%\gxx\config.json`. An older `%USERPROFILE%\.config\gxx\config.json` is still read until the next save.

## Build from source

Go 1.27+.

```sh
git clone https://github.com/DeMaarco/gxx.git
cd gxx
go install ./cmd/gxx
go test ./test/...
```
