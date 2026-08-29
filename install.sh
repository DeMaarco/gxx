#!/bin/sh
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

# Install gxx from GitHub Releases. macOS and Linux, amd64 and arm64.
set -eu

repo="DeMaarco/gxx"
install_dir="${GXX_INSTALL_DIR:-$HOME/.local/bin}"
version="${GXX_VERSION:-latest}"
token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

usage() {
	cat <<'EOF'
Install gxx from GitHub Releases.

Usage:
  curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
  sh install.sh --version v0.0.11
  sh install.sh --dir /usr/local/bin

Environment:
  GXX_VERSION       Release tag or "latest" (default: latest)
  GXX_INSTALL_DIR   Install directory (default: ~/.local/bin)
  GITHUB_TOKEN      Optional token for authenticated downloads
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		version=$2
		shift 2
		;;
	--dir)
		install_dir=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*)
	echo "unsupported architecture: $arch" >&2
	exit 1
	;;
esac
case "$os" in
darwin | linux) ;;
*)
	echo "unsupported OS: $os (macOS and Linux only)" >&2
	exit 1
	;;
esac

asset="gxx-${os}-${arch}"
if [ "$version" != "latest" ] && [ -n "$version" ]; then
	case "$version" in
	v*) ;;
	*) version="v${version}" ;;
	esac
fi

workdir=$(mktemp -d)
cleanup() {
	rm -rf "$workdir"
}
trap cleanup EXIT INT TERM HUP

download_with_gh() {
	if [ "$version" = "latest" ]; then
		gh release download -R "$repo" -p "$asset" -p checksums.txt -D "$workdir"
	else
		gh release download "$version" -R "$repo" -p "$asset" -p checksums.txt -D "$workdir"
	fi
}

download_with_api() {
	if [ "$version" = "latest" ]; then
		api="https://api.github.com/repos/${repo}/releases/latest"
	else
		api="https://api.github.com/repos/${repo}/releases/tags/${version}"
	fi
	json="${workdir}/release.json"
	curl -fsSL \
		-H "Authorization: Bearer ${token}" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		"$api" >"$json"
	python3 - "$json" "$workdir" "$asset" "$token" <<'PY'
import json, os, sys, urllib.request

with open(sys.argv[1], encoding="utf-8") as handle:
    release = json.load(handle)
workdir, want, token = sys.argv[2], sys.argv[3], sys.argv[4]
need = {want, "checksums.txt"}
found = set()
for asset in release.get("assets", []):
    name = asset.get("name")
    if name not in need:
        continue
    request = urllib.request.Request(
        asset["url"],
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/octet-stream",
            "User-Agent": "gxx-install",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    dest = os.path.join(workdir, name)
    with urllib.request.urlopen(request) as response, open(dest, "wb") as out:
        out.write(response.read())
    found.add(name)
missing = need - found
if missing:
    raise SystemExit("release is missing: " + ", ".join(sorted(missing)))
PY
}

download_public() {
	if [ "$version" = "latest" ]; then
		base="https://github.com/${repo}/releases/latest/download"
	else
		base="https://github.com/${repo}/releases/download/${version}"
	fi
	curl -fsSL "${base}/${asset}" -o "${workdir}/${asset}"
	curl -fsSL "${base}/checksums.txt" -o "${workdir}/checksums.txt"
}

echo "Downloading ${asset} (${version})..."
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
	download_with_gh
elif [ -n "$token" ]; then
	if ! command -v curl >/dev/null 2>&1; then
		echo "curl is required" >&2
		exit 1
	fi
	if ! command -v python3 >/dev/null 2>&1; then
		echo "python3 is required for token downloads" >&2
		exit 1
	fi
	download_with_api
elif command -v curl >/dev/null 2>&1; then
	download_public
else
	echo "install gh (authenticated) or curl to download gxx" >&2
	exit 1
fi

expected=$(awk -v name="$asset" '$2 == name { print $1; exit }' "${workdir}/checksums.txt")
if [ -z "$expected" ]; then
	echo "checksums.txt has no entry for ${asset}" >&2
	exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "${workdir}/${asset}" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "${workdir}/${asset}" | awk '{ print $1 }')
else
	echo "sha256sum or shasum is required" >&2
	exit 1
fi
if [ "$actual" != "$expected" ]; then
	echo "checksum mismatch for ${asset}" >&2
	echo "  expected ${expected}" >&2
	echo "  actual   ${actual}" >&2
	exit 1
fi

mkdir -p "$install_dir"
dest="${install_dir}/gxx"
if [ ! -w "$install_dir" ]; then
	echo "cannot write to ${install_dir}" >&2
	exit 1
fi
mv "${workdir}/${asset}" "$dest"
chmod 755 "$dest"
if [ "$os" = "darwin" ]; then
	xattr -d com.apple.quarantine "$dest" 2>/dev/null || true
fi

echo "Installed gxx to ${dest}"
"$dest" version || true
if ! command -v gxx >/dev/null 2>&1; then
	echo "Add ${install_dir} to PATH to run gxx:"
	echo "  export PATH=\"${install_dir}:\$PATH\""
fi
