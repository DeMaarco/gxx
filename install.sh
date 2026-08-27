#!/bin/sh
# Install gxx from GitHub Releases. macOS and Linux, amd64 and arm64.
set -eu

repo="DeMaarco/gxx"
install_dir="${GXX_INSTALL_DIR:-$HOME/.local/bin}"
version="${GXX_VERSION:-latest}"

usage() {
	cat <<'EOF'
Install gxx from GitHub Releases.

Usage:
  curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --version v0.0.3
  curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh -s -- --dir /usr/local/bin

Environment:
  GXX_VERSION       Release tag or "latest" (default: latest)
  GXX_INSTALL_DIR   Install directory (default: ~/.local/bin)
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

if ! command -v curl >/dev/null 2>&1; then
	echo "curl is required" >&2
	exit 1
fi

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
base="https://github.com/${repo}/releases"
if [ "$version" = "latest" ] || [ -z "$version" ]; then
	download="${base}/latest/download"
else
	case "$version" in
	v*) ;;
	*) version="v${version}" ;;
	esac
	download="${base}/download/${version}"
fi

workdir=$(mktemp -d)
cleanup() {
	rm -rf "$workdir"
}
trap cleanup EXIT INT TERM HUP

echo "Downloading ${asset} from ${download}..."
curl -fsSL "${download}/${asset}" -o "${workdir}/${asset}"
curl -fsSL "${download}/checksums.txt" -o "${workdir}/checksums.txt"

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
