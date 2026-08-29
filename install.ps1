# Copyright 2026 DeMarco
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Install gxx from GitHub Releases. Windows, amd64 and arm64.
$ErrorActionPreference = "Stop"

$repo = "DeMaarco/gxx"
$installDir = if ($env:GXX_INSTALL_DIR) { $env:GXX_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "gxx" }
$version = if ($env:GXX_VERSION) { $env:GXX_VERSION } else { "latest" }
$token = if ($env:GITHUB_TOKEN) { $env:GITHUB_TOKEN } elseif ($env:GH_TOKEN) { $env:GH_TOKEN } else { "" }

function ConvertTo-NormalizedPath([string]$path) {
    if (-not $path) {
        return ""
    }
    try {
        return [System.IO.Path]::GetFullPath($path.Trim()).TrimEnd("\", "/").ToLowerInvariant()
    } catch {
        return $path.Trim().TrimEnd("\", "/").ToLowerInvariant()
    }
}

function Get-PathEntries([string]$path) {
    if (-not $path) {
        return @()
    }
    return @($path -split ";" | Where-Object { $_ -and $_.Trim() -ne "" })
}

function Add-LeadingPathEntry([string]$path, [string]$dir) {
    $normalized = ConvertTo-NormalizedPath $dir
    $kept = @()
    foreach ($entry in (Get-PathEntries $path)) {
        if ((ConvertTo-NormalizedPath $entry) -ne $normalized) {
            $kept += $entry
        }
    }
    if ($kept.Count -eq 0) {
        return $dir
    }
    return $dir + ";" + ($kept -join ";")
}

function Find-GxxOnPath([string]$path) {
    foreach ($entry in (Get-PathEntries $path)) {
        foreach ($name in @("gxx.exe", "gxx")) {
            $candidate = Join-Path $entry $name
            if (Test-Path -LiteralPath $candidate) {
                return (Resolve-Path -LiteralPath $candidate).Path
            }
        }
    }
    return $null
}

function Show-Usage {
    @"
Install gxx from GitHub Releases.

Usage:
  irm https://raw.githubusercontent.com/DeMaarco/gxx/main/install.ps1 | iex
  powershell -File install.ps1 -Version v0.0.13
  powershell -File install.ps1 -Dir `$env:LOCALAPPDATA\gxx

Environment:
  GXX_VERSION       Release tag or "latest" (default: latest)
  GXX_INSTALL_DIR   Install directory (default: %LOCALAPPDATA%\gxx)
  GITHUB_TOKEN      Optional token for authenticated downloads
"@
}

for ($i = 0; $i -lt $args.Count; $i++) {
    switch ($args[$i]) {
        "-Version" {
            $version = $args[++$i]
        }
        "-Dir" {
            $installDir = $args[++$i]
        }
        { $_ -in "-h", "-Help", "--help" } {
            Show-Usage
            exit 0
        }
        default {
            Write-Error "unknown argument: $($args[$i])"
            Show-Usage
            exit 1
        }
    }
}

$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($arch) {
    "x64" { $arch = "amd64" }
    "amd64" { $arch = "amd64" }
    "arm64" { $arch = "arm64" }
    default {
        throw "unsupported architecture: $arch"
    }
}

$asset = "gxx-windows-$arch.exe"
if ($version -ne "latest" -and $version) {
    if ($version -notmatch "^v") {
        $version = "v$version"
    }
}

$workdir = Join-Path ([System.IO.Path]::GetTempPath()) ("gxx-install-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $workdir | Out-Null
try {
    Write-Host "Downloading $asset ($version)..."
    $headers = @{ "User-Agent" = "gxx-install" }
    if ($token) {
        $headers["Authorization"] = "Bearer $token"
        $headers["X-GitHub-Api-Version"] = "2022-11-28"
    }

    if ($version -eq "latest") {
        $base = "https://github.com/$repo/releases/latest/download"
    } else {
        $base = "https://github.com/$repo/releases/download/$version"
    }

    $assetPath = Join-Path $workdir $asset
    $checksumsPath = Join-Path $workdir "checksums.txt"
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $assetPath -Headers $headers -UseBasicParsing
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $checksumsPath -Headers $headers -UseBasicParsing

    $expected = $null
    foreach ($line in Get-Content -Path $checksumsPath) {
        $parts = $line.Trim() -split "\s+", 2
        if ($parts.Count -eq 2 -and $parts[1] -eq $asset) {
            $expected = $parts[0].ToLowerInvariant()
            break
        }
    }
    if (-not $expected) {
        throw "checksums.txt has no entry for $asset"
    }

    $actual = (Get-FileHash -Path $assetPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "checksum mismatch for $asset`n  expected $expected`n  actual   $actual"
    }

    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    $dest = Join-Path $installDir "gxx.exe"
    Copy-Item -Path $assetPath -Destination $dest -Force

    Write-Host "Installed gxx to $dest"
    try {
        & $dest version
    } catch {
        # version print is best-effort
    }

    $env:Path = Add-LeadingPathEntry $env:Path $installDir
    try {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $newUserPath = Add-LeadingPathEntry $userPath $installDir
        if ($newUserPath -ne $userPath) {
            [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
        }
    } catch {
        Write-Host "Could not persist PATH: $_"
        Write-Host "Add $installDir to PATH to run gxx:"
        Write-Host "  `$env:Path = `"$installDir;`$env:Path`""
    }

    $first = Find-GxxOnPath $env:Path
    $destFull = (Resolve-Path -LiteralPath $dest).Path
    if (-not $first) {
        Write-Host "Add $installDir to PATH to run gxx:"
        Write-Host "  `$env:Path = `"$installDir;`$env:Path`""
    } elseif ((ConvertTo-NormalizedPath $first) -ne (ConvertTo-NormalizedPath $destFull)) {
        Write-Host "warning: another gxx is first on PATH:"
        Write-Host "  $first"
        Write-Host "  this install: $destFull"
        Write-Host "This session prefers $installDir. Open a new terminal, or remove the older copy."
    }
} finally {
    Remove-Item -Recurse -Force -Path $workdir -ErrorAction SilentlyContinue
}
